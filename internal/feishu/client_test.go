package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c := NewClient("app-id", "secret-value")
	c.BaseURL = s.URL
	c.HTTPClient = s.Client()
	return c
}

// Catches wrong recipient type, malformed nested text JSON, lost UUID and missing cache.
func TestSendAndCache(t *testing.T) {
	var tokens, sends atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			tokens.Add(1)
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["app_id"] != "app-id" || b["app_secret"] != "secret-value" {
				t.Error("bad credentials payload")
			}
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"token-value","expire":7200}`)
			return
		}
		sends.Add(1)
		if r.URL.Path != "/open-apis/im/v1/messages" || r.URL.Query().Get("receive_id_type") != "open_id" {
			t.Error("bad endpoint")
		}
		if r.Header.Get("Authorization") != "Bearer token-value" {
			t.Error("bad token")
		}
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		var content map[string]string
		if err := json.Unmarshal([]byte(b["content"]), &content); err != nil {
			t.Error(err)
		}
		if b["receive_id"] != "ou_verified" || b["msg_type"] != "text" || b["uuid"] != "stable-uuid" || content["text"] != "Review \"quoted\"\n报告" {
			t.Errorf("wrong message: %#v", b)
		}
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_123"}}`)
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := c.Send(context.Background(), "ou_verified", "Review \"quoted\"\n报告", "stable-uuid")
			if err != nil || id != "om_123" {
				t.Errorf("send: %q %v", id, err)
			}
		}()
	}
	wg.Wait()
	if tokens.Load() != 1 || sends.Load() != 8 {
		t.Fatalf("tokens=%d sends=%d", tokens.Load(), sends.Load())
	}
}

func TestRefreshExpiredTokenOnce(t *testing.T) {
	for _, forever := range []bool{false, true} {
		t.Run(fmt.Sprint(forever), func(t *testing.T) {
			tokens, sends := 0, 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "tenant_access_token") {
					tokens++
					fmt.Fprintf(w, `{"code":0,"tenant_access_token":"token-%d","expire":7200}`, tokens)
					return
				}
				sends++
				var b map[string]string
				_ = json.NewDecoder(r.Body).Decode(&b)
				if b["uuid"] != "retry-stable" {
					t.Error("UUID changed")
				}
				if sends == 1 || forever {
					fmt.Fprint(w, `{"code":99991663,"msg":"secret-value token-1"}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer token-2" {
					t.Error("stale token")
				}
				fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_123"}}`)
			})
			_, err := c.Send(context.Background(), "ou_verified", "text", "retry-stable")
			if (err != nil) != forever {
				t.Fatalf("err=%v", err)
			}
			if tokens != 2 || sends != 2 {
				t.Fatalf("unbounded/wrong retry %d/%d", tokens, sends)
			}
		})
	}
}

func TestErrorsAreSafeAndNeverMarkDelivered(t *testing.T) {
	for _, stage := range []string{"token", "send"} {
		for _, tc := range []struct {
			name, body string
			status     int
		}{
			{"business", `{"code":999,"msg":"secret-value token-value app-id"}`, 200},
			{"http", `secret-value token-value app-id`, 500},
			{"malformed", `secret-value`, 200},
			{"missing-code", `{"data":{"message_id":"om_bad"},"tenant_access_token":"token-value","expire":7200}`, 200},
			{"empty", `{"code":0}`, 200},
			{"trailing", `{"code":0,"data":{"message_id":"om_bad"},"tenant_access_token":"token-value","expire":7200}junk`, 200},
		} {
			t.Run(stage+"/"+tc.name, func(t *testing.T) {
				c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
					if stage == "send" && strings.Contains(r.URL.Path, "tenant_access_token") {
						fmt.Fprint(w, `{"code":0,"tenant_access_token":"token-value","expire":7200}`)
						return
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
				})
				id, err := c.Send(context.Background(), "ou_verified", "text", "stable")
				if err == nil || id != "" {
					t.Fatalf("id=%q err=%v", id, err)
				}
				for _, secret := range []string{"secret-value", "token-value", "app-id"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatal("secret leaked")
					}
				}
			})
		}
	}
}

func TestCancellation(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(150 * time.Millisecond):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Send(ctx, "ou_verified", "text", "uuid")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("cancellation failed: %v", err)
	}
}

func TestValidation(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, args := range [][3]string{{"", "text", "id"}, {"ou_id", "", "id"}, {"ou_id", "text", ""}, {"ou_id", "text", strings.Repeat("x", 51)}} {
		if _, err := c.Send(context.Background(), args[0], args[1], args[2]); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request sent")
	}
}

func TestRedirectDoesNotLeakCredentials(t *testing.T) {
	var received atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
	defer target.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	if _, err := c.Send(context.Background(), "ou_id", "text", "id"); err == nil {
		t.Fatal("redirect accepted")
	}
	if received.Load() != 0 {
		t.Fatal("credential request followed redirect")
	}
}

func TestTokenExpiryRenewsCache(t *testing.T) {
	var tokens atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			tokens.Add(1)
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"short-token","expire":1}`)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_ok"}}`)
	})
	if _, err := c.Send(context.Background(), "ou_id", "text", "id"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := c.Send(context.Background(), "ou_id", "text", "id"); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 2 {
		t.Fatal("expired token was reused")
	}
}

func TestWaitingForTokenRefreshCanCancel(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			close(started)
			<-release
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"token","expire":7200}`)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_ok"}}`)
	})
	done := make(chan error, 1)
	go func() { _, err := c.Send(context.Background(), "ou_id", "text", "id"); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Send(ctx, "ou_id", "text", "id-2")
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter didn't cancel: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOversizedResponseRejected(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"code":0,"tenant_access_token":%q,"expire":7200}`, strings.Repeat("a", 2<<20))
	})
	if _, err := c.Send(context.Background(), "ou_id", "text", "id"); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestHTTPTokenErrorsRefreshOnce(t *testing.T) {
	for _, status := range []int{400, 401} {
		for _, code := range []int{99991663, 99991664, 99991671} {
			for _, always := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%d/%t", status, code, always), func(t *testing.T) {
					tokens, sends := 0, 0
					c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
						if strings.Contains(r.URL.Path, "tenant_access_token") {
							tokens++
							fmt.Fprintf(w, `{"code":0,"tenant_access_token":"token-%d","expire":7200}`, tokens)
							return
						}
						sends++
						var body map[string]string
						_ = json.NewDecoder(r.Body).Decode(&body)
						if body["uuid"] != "same-key" {
							t.Error("UUID changed")
						}
						if sends == 1 || always {
							w.WriteHeader(status)
							fmt.Fprintf(w, `{"code":%d,"msg":"secret-value token-1"}`, code)
							return
						}
						fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_ok"}}`)
					})
					id, err := c.Send(context.Background(), "ou_id", "text", "same-key")
					if (err != nil) != always || (!always && id != "om_ok") {
						t.Fatalf("id=%q err=%v", id, err)
					}
					if tokens != 2 || sends != 2 {
						t.Fatalf("tokens=%d sends=%d", tokens, sends)
					}
					if err != nil && (strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "token-1")) {
						t.Fatal("secret leaked")
					}
				})
			}
		}
	}
}

func TestNonAuthHTTPFailuresDoNotRetry(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{500, `{"code":99991663}`}, {401, `not-json secret-value`}, {401, `{"code":0,"data":{"message_id":"bad"}}`}, {401, `{"code":99991668}`},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			sends := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "tenant_access_token") {
					fmt.Fprint(w, `{"code":0,"tenant_access_token":"token","expire":7200}`)
					return
				}
				sends++
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			id, err := c.Send(context.Background(), "ou_id", "text", "id")
			if err == nil || id != "" || sends != 1 {
				t.Fatalf("id=%q err=%v sends=%d", id, err, sends)
			}
		})
	}
}
