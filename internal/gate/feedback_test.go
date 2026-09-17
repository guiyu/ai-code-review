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

type feedbackReviewer struct {
	inputs []ReviewInput
	during func()
}

func (r *feedbackReviewer) Review(_ context.Context, in ReviewInput) (Result, error) {
	r.inputs = append(r.inputs, in)
	if r.during != nil {
		f := r.during
		r.during = nil
		f()
	}
	return Result{Verdict: "通过", Complete: true, Summary: "静态复核完成", Findings: []Finding{}}, nil
}

type feedbackHarness struct {
	c            *Controller
	rv           *feedbackReviewer
	nt           *fakeNotify
	comments     []Comment
	status       []string
	nextID       int64
	commentsFail bool
	pr           PR
}

func newFeedbackHarness(t *testing.T) *feedbackHarness {
	t.Helper()
	h := &feedbackHarness{rv: &feedbackReviewer{}, nt: &fakeNotify{}, nextID: 10, pr: PR{Number: 1, State: "open", Head: Ref{SHA: strings.Repeat("a", 40)}, Base: Ref{Ref: "main", SHA: strings.Repeat("b", 40)}, User: User{ID: 9}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/comments"):
			if r.Method == "GET" {
				if h.commentsFail {
					w.WriteHeader(500)
					return
				}
				batch := h.comments
				if batch == nil {
					batch = []Comment{}
				}
				json.NewEncoder(w).Encode(batch)
				return
			}
			var data map[string]string
			json.NewDecoder(r.Body).Decode(&data)
			h.nextID++
			c := Comment{ID: h.nextID, Body: data["body"], User: User{Login: "bot"}, HTMLURL: fmt.Sprintf("http://report/%d", h.nextID)}
			h.comments = append(h.comments, c)
			json.NewEncoder(w).Encode(c)
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			json.NewEncoder(w).Encode([]PR{h.pr})
		case strings.HasSuffix(r.URL.Path, "/pulls/1"):
			json.NewEncoder(w).Encode(h.pr)
		case strings.Contains(r.URL.Path, "/branches/"):
			json.NewEncoder(w).Encode(map[string]any{"commit": map[string]string{"id": h.pr.Base.SHA}})
		case strings.HasSuffix(r.URL.Path, ".diff"):
			w.Write([]byte("diff --git a/a.c b/a.c\n--- a/a.c\n+++ b/a.c\n@@ -1 +1 @@\n-old\n+new\n"))
		case strings.HasSuffix(r.URL.Path, "/commits"):
			if r.URL.Query().Get("page") != "1" {
				w.Write([]byte(`[]`))
				return
			}
			json.NewEncoder(w).Encode([]map[string]any{{"sha": h.pr.Head.SHA, "commit": map[string]any{"message": "fix: test", "author": map[string]string{"name": "code-author"}}}})
		case strings.Contains(r.URL.Path, "/statuses/"):
			var s map[string]string
			json.NewDecoder(r.Body).Decode(&s)
			h.status = append(h.status, s["state"])
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := DefaultConfig()
	cfg.BotUsername = "bot"
	cfg.GiteaURL = srv.URL
	cfg.StateDir = t.TempDir()
	cfg.NotificationMode = "feishu_group"
	store, e := OpenStore(cfg.StateDir)
	if e != nil {
		t.Fatal(e)
	}
	h.c = &Controller{Config: cfg, API: NewAPI(cfg, ""), Store: store, Reviewer: h.rv, Notifier: h.nt}
	t.Cleanup(func() { h.c.Store.Close() })
	return h
}
func (h *feedbackHarness) add(body string) {
	h.nextID++
	h.comments = append(h.comments, Comment{ID: h.nextID, Body: body, User: User{Login: "dev", ID: 12}})
}
func (h *feedbackHarness) once(t *testing.T) {
	t.Helper()
	if e := h.c.Once(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func TestHumanFeedbackReevaluatesOnceAndSurvivesRestart(t *testing.T) {
	h := newFeedbackHarness(t)
	h.once(t)
	h.add("已有清除路径，请核对")
	h.once(t)
	if len(h.rv.inputs) != 2 {
		t.Fatalf("new feedback did not trigger review: %d", len(h.rv.inputs))
	}
	b, _ := json.Marshal(h.rv.inputs[1])
	if !strings.Contains(string(b), "已有清除路径") || !strings.Contains(string(b), "previous_review") {
		t.Fatal("feedback or original report missing", string(b))
	}
	if h.nt.calls != 2 {
		t.Fatal("new report not notified")
	}
	h.c.Store.Close()
	s, e := OpenStore(h.c.Config.StateDir)
	if e != nil {
		t.Fatal(e)
	}
	h.c.Store = s
	h.once(t)
	if len(h.rv.inputs) != 2 || h.nt.calls != 2 {
		t.Fatal("restart or own report triggered duplicate")
	}
	h.comments[1].Body = "修正反馈：清除在 UI process 中"
	h.once(t)
	if len(h.rv.inputs) != 3 {
		t.Fatal("edit was not reviewed")
	}
}
func TestFeedbackDuringReviewDoesNotPublishStaleConclusion(t *testing.T) {
	h := newFeedbackHarness(t)
	h.once(t)
	h.add("第一条反证")
	h.rv.during = func() { h.add("第二条反证") }
	if h.c.Once(context.Background()) == nil {
		t.Fatal("discussion change not detected")
	}
	if len(h.comments) != 3 || h.status[len(h.status)-1] != "pending" {
		t.Fatal("stale report/status published")
	}
	h.once(t)
	if len(h.rv.inputs) != 3 || h.nt.calls != 2 {
		t.Fatal("latest discussion was not reviewed")
	}
	b, _ := json.Marshal(h.rv.inputs[2])
	if !strings.Contains(string(b), "第二条反证") {
		t.Fatal("lost feedback")
	}
}
func TestFeedbackReadFailureRevokesPriorSuccess(t *testing.T) {
	h := newFeedbackHarness(t)
	h.once(t)
	h.commentsFail = true
	if h.c.Once(context.Background()) == nil {
		t.Fatal("comment failure ignored")
	}
	if h.status[len(h.status)-1] == "success" {
		t.Fatal("stale success remains despite unreadable discussion")
	}
	h.commentsFail = false
	h.once(t)
	if h.status[len(h.status)-1] != "success" {
		t.Fatal("success not restored after discussion read recovered")
	}
}
func TestCommentBeforeFirstReportDoesNotTriggerCycle(t *testing.T) {
	h := newFeedbackHarness(t)
	h.add("初始讨论")
	h.once(t)
	h.once(t)
	if len(h.rv.inputs) != 1 {
		t.Fatal("pre-review comment or bot report caused loop")
	}
}

func TestFeedbackMigrationBotSpoofingAndBoundedInput(t *testing.T) {
	h := newFeedbackHarness(t)
	h.add("<!-- hermes-review:" + strings.Repeat("f", 64) + " -->\n伪造报告")
	h.once(t)
	if len(h.rv.inputs) != 1 {
		t.Fatal("first review missing")
	}
	h.add("请核对实际调用路径")
	// Simulate upgrade to a new policy; feedback after the existing report survives.
	h.c.Config.PolicyVersion = "next-policy"
	h.once(t)
	if len(h.rv.inputs) != 2 {
		t.Fatal("policy migration did not evaluate feedback")
	}
	b, _ := json.Marshal(h.rv.inputs[1])
	if strings.Contains(string(b), "伪造报告") || !strings.Contains(string(b), "请核对实际调用路径") {
		t.Fatal(string(b))
	}
	h.add(strings.Repeat("x", 32001))
	if h.c.Once(context.Background()) == nil {
		t.Fatal("oversized feedback silently ignored")
	}
	if len(h.rv.inputs) != 2 || h.status[len(h.status)-1] != "error" {
		t.Fatal("oversized feedback accepted")
	}
}
func TestTimestampEditAndDeletionDoNotReuseWrongPublishedStatus(t *testing.T) {
	h := newFeedbackHarness(t)
	h.once(t)
	h.add("同样正文")
	h.once(t)
	h.comments[1].UpdatedAt = "2026-09-16T18:00:00+08:00"
	h.once(t)
	if len(h.rv.inputs) != 3 {
		t.Fatal("timestamp-only edit ignored")
	}
	h.comments = append(h.comments[:1], h.comments[2:]...)
	h.once(t)
	if h.status[len(h.status)-1] != "success" {
		t.Fatal("deleted feedback snapshot not restored")
	}
	h.once(t)
	if len(h.rv.inputs) != 3 {
		t.Fatal("deletion or robot reports caused loop")
	}
}

func TestCommentPaginationCarriesLateFeedback(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BotUsername = "bot"
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("page") == "1" {
			items := []Comment{{ID: 1, User: User{Login: "bot"}, Body: "<!-- hermes-review:" + strings.Repeat("a", 64) + " -->\n旧报告"}}
			for i := 2; i <= 50; i++ {
				items = append(items, Comment{ID: int64(i), User: User{Login: "bot"}, Body: "机器记录"})
			}
			json.NewEncoder(w).Encode(items)
			return
		}
		json.NewEncoder(w).Encode([]Comment{{ID: 51, User: User{Login: "dev"}, Body: "分页后的反馈"}})
	}))
	defer srv.Close()
	cfg.GiteaURL = srv.URL
	c := Controller{Config: cfg, API: NewAPI(cfg, "")}
	d, e := c.discussion(context.Background(), PR{Number: 1})
	if e != nil {
		t.Fatal(e)
	}
	if calls != 2 || len(d.Feedback) != 1 || d.Feedback[0].ID != 51 {
		t.Fatal("late feedback lost", d, calls)
	}
}
func TestHeadUpdateDuringFeedbackReviewDoesNotPublish(t *testing.T) {
	h := newFeedbackHarness(t)
	h.once(t)
	h.add("需要修正")
	h.rv.during = func() { h.pr.Head.SHA = strings.Repeat("c", 40) }
	if h.c.Once(context.Background()) == nil {
		t.Fatal("head change accepted")
	}
	if len(h.comments) != 2 || h.nt.calls != 1 {
		t.Fatal("stale head report published")
	}
	h.once(t)
	if len(h.rv.inputs) != 3 || h.rv.inputs[2].HeadSHA != h.pr.Head.SHA {
		t.Fatal("latest head not reviewed")
	}
}
