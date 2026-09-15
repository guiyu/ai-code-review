package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type API struct {
	Config     Config
	Token      string
	HTTPClient *http.Client
}
type APIError struct{ Code int }

func (e *APIError) Error() string { return fmt.Sprintf("Gitea request failed (HTTP %d)", e.Code) }
func NewAPI(c Config, token string) *API {
	return &API{c, token, &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}}
}
func (a *API) repo() string {
	p := strings.Split(a.Config.Repository, "/")
	return "/repos/" + url.PathEscape(p[0]) + "/" + url.PathEscape(p[1])
}
func (a *API) request(ctx context.Context, method, path string, body, out any, limit int64) error {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.Config.GiteaURL, "/")+"/api/v1"+path, bytes.NewReader(b))
	if e != nil {
		return errors.New("invalid API request")
	}
	req.Header.Set("Authorization", "token "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	res, e := a.HTTPClient.Do(req)
	if e != nil {
		return errors.New("Gitea transport failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &APIError{res.StatusCode}
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil {
		return errors.New("Gitea response read failed")
	}
	if int64(len(data)) > limit {
		return errors.New("Gitea response exceeds limit")
	}
	if out == nil {
		return nil
	}
	if v, ok := out.(*string); ok {
		*v = string(data)
		return nil
	}
	if e = json.Unmarshal(data, out); e != nil {
		return errors.New("invalid Gitea response")
	}
	return nil
}
func (a *API) JSON(ctx context.Context, m, p string, b, o any) error {
	return a.request(ctx, m, p, b, o, 4*1024*1024)
}
func (a *API) Pull(ctx context.Context, n int) (PR, error) {
	var p PR
	e := a.JSON(ctx, "GET", fmt.Sprintf("%s/pulls/%d", a.repo(), n), nil, &p)
	return p, e
}
func (a *API) Pulls(ctx context.Context) ([]PR, error) {
	all := []PR{}
	for page := 1; ; page++ {
		var p []PR
		if e := a.JSON(ctx, "GET", fmt.Sprintf("%s/pulls?state=open&limit=50&page=%d", a.repo(), page), nil, &p); e != nil {
			return nil, e
		}
		all = append(all, p...)
		if len(p) < 50 {
			return all, nil
		}
		if page >= 1000 {
			return nil, errors.New("pull pagination limit")
		}
	}
}
func (a *API) Current(ctx context.Context, p PR) error {
	q, e := a.Pull(ctx, p.Number)
	if e != nil {
		return e
	}
	if q.State != "open" || q.Merged || q.Draft || q.Head.SHA != p.Head.SHA || q.Base.SHA != p.Base.SHA || q.Base.Ref != a.Config.BaseBranch {
		return errors.New("PR changed or ineligible")
	}
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if e = a.JSON(ctx, "GET", a.repo()+"/branches/"+url.PathEscape(a.Config.BaseBranch), nil, &branch); e != nil {
		return e
	}
	if branch.Commit.ID != p.Base.SHA {
		return errors.New("target branch changed")
	}
	return nil
}

type Comment struct {
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	User    User   `json:"user"`
}

func (a *API) Status(ctx context.Context, p PR, state, target, key string) error {
	return a.JSON(ctx, "POST", a.repo()+"/statuses/"+url.PathEscape(p.Head.SHA), map[string]string{"state": state, "context": StatusContext, "description": "Hermes review " + key[:12], "target_url": target}, nil)
}

// PullCommits collects the entire PR commit log, never silently truncating it.
func (a *API) PullCommits(ctx context.Context, n int) ([]ReviewCommit, error) {
	all := []ReviewCommit{}
	seen := map[string]bool{}
	totalBytes := 0
	for page := 1; page <= 1001; page++ {
		var batch []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		if e := a.JSON(ctx, "GET", fmt.Sprintf("%s/pulls/%d/commits?limit=50&page=%d", a.repo(), n, page), nil, &batch); e != nil {
			return nil, e
		}
		if batch == nil {
			return nil, errors.New("missing PR commit history")
		}
		for _, item := range batch {
			if !shaPattern.MatchString(item.SHA) || strings.TrimSpace(item.Commit.Message) == "" || seen[item.SHA] {
				return nil, errors.New("invalid or repeated PR commit history")
			}
			seen[item.SHA] = true
			totalBytes += len(item.Commit.Message)
			if totalBytes > 200000 || len(all) >= 1000 {
				return nil, errors.New("PR commit history exceeds review limit")
			}
			all = append(all, ReviewCommit{SHA: item.SHA, Message: item.Commit.Message})
		}
		if len(batch) == 0 {
			if len(all) == 0 {
				return nil, errors.New("empty PR commit history")
			}
			return all, nil
		}
	}
	return nil, errors.New("PR commit pagination limit")
}
