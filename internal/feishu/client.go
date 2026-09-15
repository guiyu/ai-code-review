// Package feishu sends direct messages through a Feishu enterprise application bot.
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const requestTimeout = 20 * time.Second

// Client is safe for concurrent Send calls. Configure public fields before use.
// BaseURL is the API origin (without /open-apis); HTTP is supported for local tests.
// Credentials and access tokens stay in memory and never appear in returned errors.
type Client struct {
	BaseURL          string
	HTTPClient       *http.Client
	appID, appSecret string
	tokenGate        chan struct{}
	token            string
	expires          time.Time
}

func NewClient(appID, appSecret string) *Client {
	return &Client{BaseURL: "https://open.feishu.cn", HTTPClient: &http.Client{Timeout: requestTimeout}, appID: appID, appSecret: appSecret, tokenGate: make(chan struct{}, 1)}
}

type response struct {
	Code   *int   `json:"code"`
	Token  string `json:"tenant_access_token"`
	Expire int64  `json:"expire"`
	Data   struct {
		MessageID string `json:"message_id"`
	} `json:"data"`
}

type apiError struct {
	operation string
	code      int
}

func (e *apiError) Error() string { return fmt.Sprintf("feishu %s: API code %d", e.operation, e.code) }

// Send returns only an API-acknowledged message ID; this does not mean read receipt.
// uuid must be a stable, nonempty <=50-byte idempotency key supplied by the outbox.
// A rejected/expired tenant token is refreshed once, preserving the same uuid.
func (c *Client) Send(ctx context.Context, openID, text, uuid string) (string, error) {
	if strings.TrimSpace(openID) == "" || strings.TrimSpace(text) == "" || strings.TrimSpace(uuid) == "" || len(uuid) > 50 {
		return "", errors.New("feishu send: recipient, text and UUID (1-50 bytes) required")
	}
	if c.appID == "" || c.appSecret == "" || c.tokenGate == nil {
		return "", errors.New("feishu: application credentials required")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	content, _ := json.Marshal(map[string]string{"text": text})
	payload := map[string]string{"receive_id": openID, "msg_type": "text", "content": string(content), "uuid": uuid}
	rejected := ""
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.accessToken(ctx, rejected)
		if err != nil {
			return "", err
		}
		resp, err := c.post(ctx, "send", "/open-apis/im/v1/messages?receive_id_type=open_id", token, payload)
		if err != nil {
			var api *apiError
			if attempt == 0 && errors.As(err, &api) && (api.code == 99991663 || api.code == 99991664 || api.code == 99991671) {
				rejected = token
				continue
			}
			return "", err
		}
		if strings.TrimSpace(resp.Data.MessageID) == "" {
			return "", errors.New("feishu send: missing message ID")
		}
		return resp.Data.MessageID, nil
	}
	return "", errors.New("feishu send: authentication retry exhausted")
}

func (c *Client) accessToken(ctx context.Context, rejected string) (string, error) {
	// A channel lock allows a caller waiting for another refresh to cancel promptly.
	select {
	case c.tokenGate <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.tokenGate }()
	if c.token != "" && c.token != rejected && time.Now().Before(c.expires) {
		return c.token, nil
	}
	// Drop a rejected token even if fetching its replacement fails.
	c.token = ""
	resp, err := c.post(ctx, "token", "/open-apis/auth/v3/tenant_access_token/internal", "", map[string]string{"app_id": c.appID, "app_secret": c.appSecret})
	if err != nil {
		return "", err
	}
	if resp.Token == "" || resp.Expire <= 0 || resp.Expire > 86400 {
		return "", errors.New("feishu token: missing token or invalid expiry")
	}
	lifetime := time.Duration(resp.Expire) * time.Second
	margin := 30 * time.Second
	if lifetime < 2*margin {
		margin = lifetime / 2
	}
	c.token = resp.Token
	c.expires = time.Now().Add(lifetime - margin)
	return c.token, nil
}

func (c *Client) post(ctx context.Context, operation, path, token string, payload any) (response, error) {
	var result response
	body, err := json.Marshal(payload)
	if err != nil {
		return result, fmt.Errorf("feishu %s: invalid request", operation)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("feishu %s: invalid endpoint", operation)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := http.Client{Timeout: requestTimeout}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	// Never forward app secrets or bearer tokens through HTTP redirects.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("feishu %s: %w", operation, ctx.Err())
		}
		return result, fmt.Errorf("feishu %s: HTTP request failed", operation)
	}
	defer resp.Body.Close()
	if (resp.StatusCode < 200 || resp.StatusCode >= 300) && resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnauthorized {
		return result, fmt.Errorf("feishu %s: HTTP status %d", operation, resp.StatusCode)
	}
	const maxResponse = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("feishu %s: %w", operation, ctx.Err())
		}
		return result, fmt.Errorf("feishu %s: response read failed", operation)
	}
	if len(data) > maxResponse || json.Unmarshal(data, &result) != nil || result.Code == nil {
		return response{}, fmt.Errorf("feishu %s: invalid response", operation)
	}
	if *result.Code != 0 {
		return response{}, &apiError{operation: operation, code: *result.Code}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return response{}, fmt.Errorf("feishu %s: HTTP status %d", operation, resp.StatusCode)
	}
	return result, nil
}
