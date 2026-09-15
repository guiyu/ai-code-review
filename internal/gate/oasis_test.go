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

func TestOasisVerdictControlsGate(t *testing.T) {
	for _, v := range []string{"通过", "有条件通过", "不通过", "证据不足", ""} {
		raw, _ := json.Marshal(map[string]any{"complete": true, "verdict": v, "summary": "中文评审报告", "findings": []any{}})
		r, e := DecodeResult(raw)
		if v == "" {
			if e == nil {
				t.Fatal("missing verdict accepted")
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		if r.Passes("high") != (v == "通过") {
			t.Fatalf("wrong gate verdict: %s", v)
		}
	}
}
func TestPullCommitHistoryPaginates(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("limit") != "50" {
			t.Error("missing pagination limit")
		}
		n := 50
		if r.URL.Query().Get("page") == "2" {
			n = 1
		}
		if r.URL.Query().Get("page") == "3" {
			n = 0
		}
		commits := []map[string]any{}
		for i := 0; i < n; i++ {
			commits = append(commits, map[string]any{"sha": fmt.Sprintf("%040x", (calls-1)*50+i+1), "commit": map[string]string{"message": "fix: 清理跨核资源"}})
		}
		json.NewEncoder(w).Encode(commits)
	}))
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.GiteaURL = srv.URL
	commits, e := NewAPI(cfg, "").PullCommits(context.Background(), 1)
	if e != nil || len(commits) != 51 || calls != 3 {
		t.Fatal(len(commits), calls, e)
	}
	if !strings.Contains(commits[50].Message, "跨核资源") {
		t.Fatal("commit message lost")
	}
}

func TestShortCommitPageIsNotLastPage(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			json.NewEncoder(w).Encode([]map[string]any{{"sha": fmt.Sprintf("%040x", calls), "commit": map[string]string{"message": "fix: 资源清理"}}})
		} else {
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.GiteaURL = srv.URL
	commits, e := NewAPI(cfg, "").PullCommits(context.Background(), 1)
	if e != nil || len(commits) != 2 || calls != 3 {
		t.Fatal(len(commits), calls, e)
	}
}

func TestSubprocessPreservesSafeTimeoutReason(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReviewerCommand = []string{"/bin/sh", "-c", `printf '{"complete":false,"error_code":"REVIEW_TIMEOUT","summary":"SECRET DO NOT LOG"}'; exit 1`}
	_, err := (SubprocessReviewer{cfg}).Review(context.Background(), ReviewInput{})
	if err == nil || !strings.Contains(err.Error(), "REVIEW_TIMEOUT") || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("safe timeout reason lost", err)
	}
}
func TestSubprocessDoesNotTrustArbitraryErrorText(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReviewerCommand = []string{"/bin/sh", "-c", `printf '{"complete":false,"error_code":"SECRET DO NOT LOG"}'; exit 1`}
	_, err := (SubprocessReviewer{cfg}).Review(context.Background(), ReviewInput{})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("untrusted error exposed", err)
	}
}
