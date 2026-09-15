package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GroupClient sends only controller-generated review notifications to a custom bot.
// Configure HTTPClient before use. The webhook is a secret and is never logged.
type GroupClient struct {
	webhook    string
	HTTPClient *http.Client
}

func NewGroupClient(webhook string) *GroupClient {
	return &GroupClient{webhook: webhook, HTTPClient: &http.Client{Timeout: requestTimeout}}
}

// Send returns a local acceptance marker, not a Feishu message ID. Custom bots do
// not support the DM API's UUID deduplication; uncertain retries can duplicate.
func (c *GroupClient) Send(ctx context.Context, _ string, text, uuid string) (string, error) {
	u, err := url.Parse(c.webhook)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", errors.New("feishu group: valid HTTPS webhook required")
	}
	if strings.TrimSpace(text) == "" || strings.TrimSpace(uuid) == "" {
		return "", errors.New("feishu group: text and run key required")
	}
	payload, _ := json.Marshal(map[string]any{"msg_type": "text", "content": map[string]string{"text": "codereview\n" + text}})
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhook, bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("feishu group: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: requestTimeout}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("feishu group: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("feishu group: unsuccessful HTTP status")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return "", errors.New("feishu group: invalid response")
	}
	var ack struct {
		Code       *int `json:"code"`
		StatusCode *int `json:"StatusCode"`
	}
	if json.Unmarshal(body, &ack) != nil || (ack.Code == nil && ack.StatusCode == nil) {
		return "", errors.New("feishu group: missing acknowledgement")
	}
	if ack.Code != nil && *ack.Code != 0 {
		return "", &apiError{operation: "group", code: *ack.Code}
	}
	if ack.StatusCode != nil && *ack.StatusCode != 0 {
		return "", &apiError{operation: "group", code: *ack.StatusCode}
	}
	return "group-accepted:" + uuid, nil
}
