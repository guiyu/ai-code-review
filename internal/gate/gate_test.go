package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyFailClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"complete":true,"summary":"ok"}`, `{"complete":true,"summary":"ok","findings":[{"severity":"unknown"}]}`, `{"complete":false,"summary":"partial","findings":[]}`} {
		if _, err := DecodeResult([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	r, err := DecodeResult([]byte(`{"complete":true,"verdict":"通过","summary":"ok","findings":[]}`))
	if err != nil || !r.Passes("high") {
		t.Fatal(r, err)
	}
	r.Findings = []Finding{{Severity: "high"}}
	if r.Passes("high") {
		t.Fatal("high passed")
	}
}
func TestStoreExclusiveAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = OpenStore(dir); e == nil {
		t.Fatal("double lock")
	}
	s.Runs["x"] = &Run{Key: "x", ReportID: 7, Notified: true}
	if e = s.Save(); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if s.Runs["x"].ReportID != 7 || !s.Runs["x"].Notified {
		t.Fatal("lost state")
	}
}

type fakeReviewer struct {
	input ReviewInput
	calls int
	fail  int
}

func (r *fakeReviewer) Review(_ context.Context, in ReviewInput) (Result, error) {
	r.input = in
	r.calls++
	if r.calls <= r.fail {
		return Result{}, errTest
	}
	return Result{Verdict: "通过", Complete: true, Summary: "Reviewed", Findings: []Finding{}}, nil
}

type fakeNotify struct {
	calls int
	fail  bool
}

func (n *fakeNotify) Send(context.Context, string, string, string) (string, error) {
	n.calls++
	if n.fail {
		return "", errTest
	}
	return "sent", nil
}

var errTest = &APIError{Code: 500}

func TestReportBeforeSuccessAndRetry(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	events := []string{}
	reportFail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/pulls/1/commits"):
			if r.URL.Query().Get("page") != "1" {
				w.Write([]byte(`[]`))
				return
			}
			json.NewEncoder(w).Encode([]map[string]any{{"sha": head, "commit": map[string]string{"message": "fix: 本次需求"}}})
		case strings.HasSuffix(p, "/pulls"):
			json.NewEncoder(w).Encode([]PR{{Number: 1, State: "open", Head: Ref{SHA: head}, Base: Ref{Ref: "main", SHA: base}, User: User{ID: 9}}})
		case strings.HasSuffix(p, "/pulls/1.diff"):
			w.Write([]byte("diff --git a/a b/a\n+ok\n"))
		case strings.HasSuffix(p, "/pulls/1"):
			json.NewEncoder(w).Encode(PR{Number: 1, State: "open", Head: Ref{SHA: head}, Base: Ref{Ref: "main", SHA: base}, User: User{ID: 9}})
		case strings.Contains(p, "/branches/"):
			json.NewEncoder(w).Encode(map[string]any{"commit": map[string]string{"id": base}})
		case strings.Contains(p, "/statuses/"):
			var v map[string]string
			json.NewDecoder(r.Body).Decode(&v)
			events = append(events, v["state"])
			json.NewEncoder(w).Encode(map[string]any{"id": 1})
		case strings.HasSuffix(p, "/comments"):
			if r.Method == "GET" {
				w.Write([]byte(`[]`))
				return
			}
			events = append(events, "report")
			if reportFail {
				w.WriteHeader(500)
			} else {
				json.NewEncoder(w).Encode(Comment{ID: 7, HTMLURL: "http://report"})
			}
		default:
			t.Errorf("unexpected %s", p)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.GiteaURL = server.URL
	cfg.StateDir = t.TempDir()
	cfg.Identities = map[string]string{"9": "ou_test"}
	s, _ := OpenStore(cfg.StateDir)
	defer s.Close()
	rv := &fakeReviewer{}
	nt := &fakeNotify{fail: true}
	c := &Controller{Config: cfg, API: NewAPI(cfg, "token"), Store: s, Reviewer: rv, Notifier: nt}
	if e := c.Once(context.Background()); e == nil {
		t.Fatal("report error ignored")
	}
	for _, v := range events {
		if v == "success" {
			t.Fatal("success before report")
		}
	}
	s.Close()
	restarted, err := OpenStore(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	c.Store = restarted
	reportFail = false
	if e := c.Once(context.Background()); e == nil {
		t.Fatal("notification error ignored")
	}
	if len(rv.input.Commits) != 1 || rv.input.Commits[0].SHA != head || rv.input.Commits[0].Message != "fix: 本次需求" {
		t.Fatal("commit evidence not delivered to reviewer")
	}
	if rv.calls != 1 {
		t.Fatal("re-reviewed", rv.calls)
	}
	nt.fail = false
	if e := c.Once(context.Background()); e != nil {
		t.Fatal(e)
	}
	if rv.calls != 1 || nt.calls != 2 {
		t.Fatal(rv.calls, nt.calls)
	}
}
func TestVersionIdentity(t *testing.T) {
	c := DefaultConfig()
	p := PR{Number: 1, Head: Ref{SHA: "a"}, Base: Ref{SHA: "b"}}
	key := c.Key(p)
	p.Number = 2
	if c.Key(p) == key {
		t.Fatal("PR reused")
	}
	p.Number = 1
	p.Base.SHA = "c"
	if c.Key(p) == key {
		t.Fatal("base reused")
	}
	p.Base.SHA = "b"
	c.PolicyVersion = "2"
	if c.Key(p) == key {
		t.Fatal("policy reused")
	}
}
func TestStaleDuringDiffNeverReviews(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	changed := false
	reviews := &fakeReviewer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PR{Number: 1, State: "open", Head: Ref{SHA: head}, Base: Ref{Ref: "main", SHA: base}}
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			json.NewEncoder(w).Encode([]PR{p})
		case strings.HasSuffix(r.URL.Path, "/pulls/1/commits"):
			if r.URL.Query().Get("page") != "1" {
				w.Write([]byte(`[]`))
				return
			}
			json.NewEncoder(w).Encode([]map[string]any{{"sha": head, "commit": map[string]string{"message": "fix: 本次需求"}}})
		case strings.HasSuffix(r.URL.Path, ".diff"):
			changed = true
			w.Write([]byte("diff"))
		case strings.HasSuffix(r.URL.Path, "/pulls/1"):
			if changed {
				p.Head.SHA = strings.Repeat("c", 40)
			}
			json.NewEncoder(w).Encode(p)
		case strings.Contains(r.URL.Path, "/branches/"):
			json.NewEncoder(w).Encode(map[string]any{"commit": map[string]string{"id": base}})
		case strings.Contains(r.URL.Path, "/statuses/"):
			var v map[string]string
			json.NewDecoder(r.Body).Decode(&v)
			if v["state"] != "pending" {
				t.Error("nonpending stale status")
			}
			w.Write([]byte(`{}`))
		default:
			t.Error("unexpected call", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.GiteaURL = srv.URL
	s, _ := OpenStore(t.TempDir())
	defer s.Close()
	c := &Controller{Config: cfg, API: NewAPI(cfg, ""), Store: s, Reviewer: reviews}
	if c.Once(context.Background()) == nil {
		t.Fatal("stale change ignored")
	}
	if reviews.calls != 0 {
		t.Fatal("reviewed stale diff")
	}
}
func TestProtectionPreservesChecksAndRejectsBypass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BotUsername = "bot"
	cfg.MergeWhitelistUsernames = []string{"halliday", "qingye", "bot", "halliday"}
	api := NewAPI(cfg, "")
	old := map[string]any{"status_check_contexts": []any{"ci"}, "required_approvals": float64(2), "enable_push": true, "merge_whitelist_teams": []any{"maintainers"}}
	p := api.ProtectionPlan(old)
	if !contains(stringsOf(p["status_check_contexts"]), "ci") || p["required_approvals"] != float64(2) || p["enable_push"] != false {
		t.Fatal(p)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/branches/") {
			json.NewEncoder(w).Encode(map[string]any{"protected": true, "effective_branch_protection_name": "main"})
			return
		}
		json.NewEncoder(w).Encode(p)
	}))
	defer srv.Close()
	api.Config.GiteaURL = srv.URL
	if e := api.AuditProtection(context.Background()); e != nil {
		t.Fatal(e)
	}
	p["merge_whitelist_usernames"] = []string{"qingye", "bot", "halliday"}
	if e := api.AuditProtection(context.Background()); e != nil {
		t.Fatal(e)
	}
	p["merge_whitelist_usernames"] = []string{"bot", "halliday", "qingye", "stranger"}
	if api.AuditProtection(context.Background()) == nil {
		t.Fatal("unconfigured merger accepted")
	}
	p["merge_whitelist_usernames"] = []string{"bot", "halliday"}
	if api.AuditProtection(context.Background()) == nil {
		t.Fatal("missing configured merger accepted")
	}
	p["merge_whitelist_usernames"] = cfg.MergeUsers()
	p["merge_whitelist_teams"] = []string{"admins"}
	if api.AuditProtection(context.Background()) == nil {
		t.Fatal("team bypass")
	}
}
func TestMergeRefusesStaleHead(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BotUsername = "bot"
	api := NewAPI(cfg, "")
	protection := api.ProtectionPlan(nil)
	mergeCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/user":
			json.NewEncoder(w).Encode(User{ID: 1, Login: "bot"})
		case strings.Contains(r.URL.Path, "/branches/"):
			json.NewEncoder(w).Encode(map[string]any{"protected": true, "effective_branch_protection_name": "main"})
		case strings.Contains(r.URL.Path, "branch_protections"):
			json.NewEncoder(w).Encode(protection)
		case strings.HasSuffix(r.URL.Path, "/pulls/1"):
			json.NewEncoder(w).Encode(PR{Head: Ref{SHA: strings.Repeat("b", 40)}})
		case strings.HasSuffix(r.URL.Path, "/merge"):
			mergeCalled = true
		default:
			json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]bool{"admin": true}})
		}
	}))
	defer srv.Close()
	cfg.GiteaURL = srv.URL
	api.Config = cfg
	s, _ := OpenStore(t.TempDir())
	defer s.Close()
	c := &Controller{Config: cfg, API: api, Store: s}
	if c.Merge(context.Background(), 1, strings.Repeat("a", 40)) == nil {
		t.Fatal("stale accepted")
	}
	if mergeCalled {
		t.Fatal("merge requested")
	}
}
func TestSubprocessIsolationAndTimeout(t *testing.T) {
	t.Setenv("GITEA_TOKEN", "never-child")
	t.Setenv("FEISHU_APP_SECRET", "never-child")
	cfg := DefaultConfig()
	cfg.ReviewerCommand = []string{"/bin/sh", "-c", `test -z "$GITEA_TOKEN$FEISHU_APP_SECRET" || exit 1; printf '{"complete":true,"verdict":"通过","summary":"isolated","findings":[]}'`}
	r, e := (SubprocessReviewer{cfg}).Review(context.Background(), ReviewInput{})
	if e != nil || r.Summary != "isolated" {
		t.Fatal(r, e)
	}
	cfg.ReviewTimeoutSeconds = 1
	cfg.ReviewerCommand = []string{"/bin/sh", "-c", "exec sleep 10"}
	if _, e = (SubprocessReviewer{cfg}).Review(context.Background(), ReviewInput{}); e == nil {
		t.Fatal("timeout ignored")
	}
	cfg.ReviewerCommand = []string{"/bin/sh", "-c", `printf '{"complete":true}'`}
	if _, e = (SubprocessReviewer{cfg}).Review(context.Background(), ReviewInput{}); e == nil {
		t.Fatal("incomplete accepted")
	}
}
func TestMergeTrustedCurrentReview(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BotUsername = "bot"
	p := PR{Number: 1, State: "open", Mergeable: true, MergeBase: strings.Repeat("b", 40), Head: Ref{SHA: strings.Repeat("a", 40)}, Base: Ref{Ref: "main", SHA: strings.Repeat("b", 40)}}
	api := NewAPI(cfg, "")
	protection := api.ProtectionPlan(nil)
	s, _ := OpenStore(t.TempDir())
	defer s.Close()
	c := &Controller{Config: cfg, API: api, Store: s}
	var run *Run
	merged := false
	tamper := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/user":
			json.NewEncoder(w).Encode(User{ID: 1, Login: "bot"})
		case strings.Contains(r.URL.Path, "/branch_protections/"):
			json.NewEncoder(w).Encode(protection)
		case strings.Contains(r.URL.Path, "/branches/"):
			json.NewEncoder(w).Encode(map[string]any{"protected": true, "effective_branch_protection_name": "main", "commit": map[string]string{"id": p.Base.SHA}})
		case strings.HasSuffix(r.URL.Path, "/pulls/1"):
			json.NewEncoder(w).Encode(p)
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			json.NewEncoder(w).Encode([]PR{p})
		case strings.HasSuffix(r.URL.Path, "/comments/7"):
			body := run.ReportBody
			if tamper {
				body = "modified"
			}
			json.NewEncoder(w).Encode(Comment{ID: 7, Body: body, User: User{Login: "bot"}})
		case strings.HasSuffix(r.URL.Path, "/status"):
			json.NewEncoder(w).Encode(map[string]any{"statuses": []any{map[string]any{"context": StatusContext, "status": "success", "description": "Hermes review " + run.Key[:12], "target_url": run.ReportURL, "creator": User{Login: "bot"}}}})
		case strings.HasSuffix(r.URL.Path, "/merge"):
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["head_commit_id"] != p.Head.SHA || b["force_merge"] != false || b["Do"] != "merge" {
				t.Error(b)
			}
			merged = true
			w.WriteHeader(200)
		default:
			json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]bool{"admin": true}})
		}
	}))
	defer srv.Close()
	cfg.GiteaURL = srv.URL
	c.Config = cfg
	api.Config = cfg
	run = &Run{Key: cfg.Key(p), PR: p, Result: &Result{Verdict: "通过", Complete: true, Summary: "ok", Findings: []Finding{}}, ReportID: 7, ReportURL: "http://report", Status: "success"}
	run.ReportBody = c.report(run)
	s.Runs[run.Key] = run
	if e := c.Merge(context.Background(), 1, p.Head.SHA); e != nil {
		t.Fatal(e)
	}
	if !merged {
		t.Fatal("merge absent")
	}
	merged = false
	tamper = true
	if c.Merge(context.Background(), 1, p.Head.SHA) == nil || merged {
		t.Fatal("tampered report accepted")
	}
}
func TestAutomaticReviewerRetryAndCommentRecovery(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BotUsername = "bot"
	p := PR{Number: 1, State: "open", Head: Ref{SHA: strings.Repeat("a", 40)}, Base: Ref{Ref: "main", SHA: strings.Repeat("b", 40)}}
	var c *Controller
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/1"):
			json.NewEncoder(w).Encode(p)
		case strings.Contains(r.URL.Path, "/branches/"):
			json.NewEncoder(w).Encode(map[string]any{"commit": map[string]string{"id": p.Base.SHA}})
		case strings.HasSuffix(r.URL.Path, "/pulls/1/commits"):
			if r.URL.Query().Get("page") != "1" {
				w.Write([]byte(`[]`))
				return
			}
			json.NewEncoder(w).Encode([]map[string]any{{"sha": p.Head.SHA, "commit": map[string]string{"message": "fix: 本次需求"}}})
		case strings.HasSuffix(r.URL.Path, ".diff"):
			w.Write([]byte("diff"))
		case strings.Contains(r.URL.Path, "/statuses/"):
			w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/comments"):
			if r.Method == "POST" {
				posts++
				t.Error("duplicated recovered report")
				w.WriteHeader(500)
				return
			}
			run := c.Store.Runs[c.Config.Key(p)]
			json.NewEncoder(w).Encode([]Comment{{ID: 7, Body: run.ReportBody, HTMLURL: "http://report", User: User{Login: "bot"}}})
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cfg.GiteaURL = srv.URL
	s, _ := OpenStore(t.TempDir())
	defer s.Close()
	rv := &fakeReviewer{fail: 1}
	c = &Controller{Config: cfg, API: NewAPI(cfg, ""), Store: s, Reviewer: rv}
	if c.process(context.Background(), p) == nil {
		t.Fatal("failure ignored")
	}
	run := s.Runs[cfg.Key(p)]
	if run.Attempts != 1 || run.ReviewError == "" || run.ReportID != 0 {
		t.Fatal(run)
	}
	if c.process(context.Background(), p) == nil || rv.calls != 1 {
		t.Fatal("backoff ignored")
	}
	run.NextAttemptUnix = 0
	if e := c.process(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	if rv.calls != 2 || run.ReportID != 7 || run.Status != "success" || posts != 0 {
		t.Fatal(run, rv.calls, posts)
	}
}

func TestSharedHeadConflictClearsAndRepublishesWithoutReview(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Identities = map[string]string{"9": "ou_test"}
	p := PR{Number: 1, State: "open", Head: Ref{SHA: strings.Repeat("a", 40)}, Base: Ref{Ref: "main", SHA: strings.Repeat("b", 40)}, User: User{ID: 9}}
	ambiguous := false
	states := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			prs := []PR{p}
			if ambiguous {
				other := p
				other.Number = 2
				prs = append(prs, other)
			}
			json.NewEncoder(w).Encode(prs)
		case strings.HasSuffix(r.URL.Path, "/pulls/1"):
			json.NewEncoder(w).Encode(p)
		case strings.Contains(r.URL.Path, "/branches/"):
			json.NewEncoder(w).Encode(map[string]any{"commit": map[string]string{"id": p.Base.SHA}})
		case strings.HasSuffix(r.URL.Path, "/pulls/1/commits"):
			if r.URL.Query().Get("page") != "1" {
				w.Write([]byte(`[]`))
				return
			}
			json.NewEncoder(w).Encode([]map[string]any{{"sha": p.Head.SHA, "commit": map[string]string{"message": "fix: 本次需求"}}})
		case strings.HasSuffix(r.URL.Path, ".diff"):
			w.Write([]byte("diff"))
		case strings.Contains(r.URL.Path, "/statuses/"):
			var v map[string]string
			json.NewDecoder(r.Body).Decode(&v)
			states = append(states, v["state"])
			w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/comments"):
			if r.Method == "GET" {
				w.Write([]byte(`[]`))
			} else {
				json.NewEncoder(w).Encode(Comment{ID: 7, HTMLURL: "http://report"})
			}
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	cfg.GiteaURL = srv.URL
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rv := &fakeReviewer{}
	c := &Controller{Config: cfg, API: NewAPI(cfg, ""), Store: s, Reviewer: rv, Notifier: &fakeNotify{}}
	if e = c.Once(context.Background()); e != nil {
		t.Fatal(e)
	}
	if states[len(states)-1] != "success" {
		t.Fatal(states)
	}
	ambiguous = true
	if c.Once(context.Background()) == nil {
		t.Fatal("ambiguity accepted")
	}
	if states[len(states)-1] != "error" {
		t.Fatal(states)
	}
	ambiguous = false
	if e = c.Once(context.Background()); e != nil {
		t.Fatal(e)
	}
	if states[len(states)-1] != "success" {
		t.Fatal("passing status was not restored", states)
	}
	if rv.calls != 1 {
		t.Fatal("unnecessary re-review", rv.calls)
	}
}

func TestOutboxRetriesDuringPullListOutage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.GiteaURL = srv.URL
	cfg.Identities = map[string]string{"9": "ou_test"}
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	nt := &fakeNotify{}
	c := &Controller{Config: cfg, API: NewAPI(cfg, ""), Store: s, Notifier: nt}
	r := &Run{Key: strings.Repeat("a", 64), Scope: c.scope(), PR: PR{Number: 1, User: User{ID: 9}}, ReportID: 7, ReportURL: "http://report", Status: "success", Result: &Result{Verdict: "通过", Complete: true, Summary: "ok", Findings: []Finding{}}}
	s.Runs[r.Key] = r
	if e = s.Save(); e != nil {
		t.Fatal(e)
	}
	if c.Once(context.Background()) == nil {
		t.Fatal("Gitea outage not reported")
	}
	if nt.calls != 1 || !r.Notified {
		t.Fatal("outbox was not drained", nt.calls, r.Notified)
	}
}

func TestReviewOnlyCannotMerge(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BotUsername = "bot"
	cfg.ReviewOnly = true
	cfg.MergeWhitelistUsernames = []string{"bot", "halliday", "qingye"}
	users := cfg.MergeUsers()
	if contains(users, "bot") || len(users) != 2 {
		t.Fatal(users)
	}
	c := &Controller{Config: cfg}
	// No API is configured: the guard must reject before any network operation.
	if err := c.Merge(context.Background(), 588, strings.Repeat("a", 40)); err == nil {
		t.Fatal("review-only controller can merge")
	}
	cfg.MergeWhitelistUsernames = nil
	if len(cfg.MergeUsers()) != 0 {
		t.Fatal("bot was reintroduced")
	}
}

func TestAdministratorMergeOverridePolicy(t *testing.T) {
	for _, allow := range []bool{false, true} {
		cfg := DefaultConfig()
		// Decode configuration to also exercise the public JSON setting.
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"allow_admin_merge_override":%t}`, allow)), &cfg); err != nil {
			t.Fatal(err)
		}
		api := NewAPI(cfg, "")
		p := api.ProtectionPlan(nil)
		if p["block_admin_merge_override"] != !allow {
			t.Fatalf("allow=%v: %v", allow, p)
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/branches/") {
				json.NewEncoder(w).Encode(map[string]any{"protected": true, "effective_branch_protection_name": "main"})
				return
			}
			json.NewEncoder(w).Encode(p)
		}))
		api.Config.GiteaURL = srv.URL
		if err := api.AuditProtection(context.Background()); err != nil {
			t.Fatal(err)
		}
		p["block_admin_merge_override"] = allow
		if api.AuditProtection(context.Background()) == nil {
			t.Fatal("unexpected admin override policy accepted")
		}
		srv.Close()
	}
}
