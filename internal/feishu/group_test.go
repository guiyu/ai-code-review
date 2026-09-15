package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupWebhookKeywordAndBusinessAcknowledgement(t *testing.T) {
	for _, body := range []string{`{"code":0,"msg":"success"}`, `{"StatusCode":0,"StatusMessage":"success"}`, `{"code":19024,"msg":"keyword missing"}`, `{}`} {
		t.Run(body, func(t *testing.T) {
			var got string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p struct {
					MsgType string `json:"msg_type"`
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				json.NewDecoder(r.Body).Decode(&p)
				got = p.Content.Text
				if p.MsgType != "text" {
					t.Error("wrong message type")
				}
				w.Write([]byte(body))
			}))
			defer server.Close()
			c := NewGroupClient(server.URL)
			c.HTTPClient = server.Client()
			ack, err := c.Send(context.Background(), "group", "Review complete\nhttps://gitea.example/pr/1", "run1")
			success := strings.Contains(body, `:0`)
			if success && (err != nil || ack == "") {
				t.Fatal(ack, err)
			}
			if !success && err == nil {
				t.Fatal("accepted business error")
			}
			if !strings.Contains(got, "codereview") {
				t.Fatal("missing required keyword")
			}
		})
	}
}

func TestGroupWebhookRejectsRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://example.com", 302) }))
	defer server.Close()
	c := NewGroupClient(server.URL)
	c.HTTPClient = server.Client()
	if _, err := c.Send(context.Background(), "group", "result", "run"); err == nil {
		t.Fatal("accepted redirect")
	}
}

func TestGroupWebhookUsesExactConfiguredKeyword(t *testing.T) {
	var got string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		got = p.Content.Text
		if !strings.HasPrefix(got, "1. codereview\n") {
			w.Write([]byte(`{"code":19024}`))
			return
		}
		w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()
	c := NewGroupClient(server.URL)
	c.HTTPClient = server.Client()
	c.Keyword = "1. codereview"
	if _, e := c.Send(context.Background(), "group", "中文评审结果", "run"); e != nil {
		t.Fatal(e)
	}
	if got != "1. codereview\n中文评审结果" {
		t.Fatal("configured keyword changed")
	}
}
