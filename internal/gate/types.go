package gate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const StatusContext = "hermes-review"

type Config struct {
	FeishuWebhookKeywordEnv string            `json:"feishu_webhook_keyword_env"`
	FeishuWebhookURLEnv     string            `json:"feishu_webhook_url_env"`
	NotificationMode        string            `json:"notification_mode"`
	GiteaURL                string            `json:"gitea_url"`
	Repository              string            `json:"repository"`
	BaseBranch              string            `json:"base_branch"`
	TokenEnv                string            `json:"token_env"`
	BotUsername             string            `json:"bot_username"`
	StateDir                string            `json:"state_dir"`
	PolicyVersion           string            `json:"policy_version"`
	BlockThreshold          string            `json:"block_threshold"`
	ReviewerCommand         []string          `json:"reviewer_command"`
	ReviewerEnv             map[string]string `json:"reviewer_env"`
	Identities              map[string]string `json:"identities"`
	FeishuAppIDEnv          string            `json:"feishu_app_id_env"`
	FeishuAppSecretEnv      string            `json:"feishu_app_secret_env"`
	PollSeconds             int               `json:"poll_seconds"`
	ReviewTimeoutSeconds    int               `json:"review_timeout_seconds"`
	MaxDiffBytes            int64             `json:"max_diff_bytes"`
	AllowInsecureHTTP       bool              `json:"allow_insecure_http"`
}

func DefaultConfig() Config {
	return Config{FeishuWebhookKeywordEnv: "FEISHU_WEBHOOK_KEYWORD", FeishuWebhookURLEnv: "FEISHU_WEBHOOK_URL", NotificationMode: "feishu_dm", GiteaURL: "http://120.26.178.131:3000", Repository: "qianshou/Gitea_code_review", BaseBranch: "main", TokenEnv: "GITEA_TOKEN", StateDir: ".review-gate-state", PolicyVersion: "oasis-v1", BlockThreshold: "high", FeishuAppIDEnv: "FEISHU_APP_ID", FeishuAppSecretEnv: "FEISHU_APP_SECRET", PollSeconds: 30, ReviewTimeoutSeconds: 300, MaxDiffBytes: 200000}
}
func LoadConfig(path string) (Config, error) {
	c := DefaultConfig()
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	u, e := url.Parse(c.GiteaURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(c.AllowInsecureHTTP && u.Scheme == "http")) {
		return c, errors.New("invalid Gitea URL or HTTP not explicitly enabled")
	}
	if len(strings.Split(c.Repository, "/")) != 2 || strings.Contains(c.Repository, "..") || c.BaseBranch == "" || c.BotUsername == "" || c.StateDir == "" || c.PolicyVersion == "" || rank[c.BlockThreshold] == 0 || c.PollSeconds < 1 || c.ReviewTimeoutSeconds < 1 || c.MaxDiffBytes < 1 {
		return c, errors.New("invalid controller configuration")
	}
	if c.NotificationMode != "feishu_dm" && c.NotificationMode != "feishu_group" {
		return c, errors.New("invalid notification_mode: use feishu_dm or feishu_group")
	}
	return c, nil
}

type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}
type Ref struct {
	SHA string `json:"sha"`
	Ref string `json:"ref"`
}
type PR struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Head      Ref    `json:"head"`
	Base      Ref    `json:"base"`
	User      User   `json:"user"`
	Draft     bool   `json:"draft"`
	Merged    bool   `json:"merged"`
	Mergeable bool   `json:"mergeable"`
	MergeBase string `json:"merge_base"`
}
type ReviewCommit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
}
type ReviewInput struct {
	Description string         `json:"description"`
	HeadRef     string         `json:"head_ref"`
	BaseRef     string         `json:"base_ref"`
	MergeBase   string         `json:"merge_base"`
	Commits     []ReviewCommit `json:"commits"`
	Repository  string         `json:"repository"`
	Number      int            `json:"number"`
	HeadSHA     string         `json:"head_sha"`
	BaseSHA     string         `json:"base_sha"`
	Diff        string         `json:"diff"`
	Title       string         `json:"title"`
}
type Finding struct {
	Severity   string `json:"severity"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Title      string `json:"title"`
	Evidence   string `json:"evidence"`
	Suggestion string `json:"suggestion"`
}
type Result struct {
	Verdict  string    `json:"verdict"`
	Complete bool      `json:"complete"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

var rank = map[string]int{"info": 1, "low": 2, "medium": 3, "high": 4, "critical": 5}

func DecodeResult(b []byte) (Result, error) {
	var r Result
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&r); e != nil {
		return r, errors.New("invalid reviewer JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return r, errors.New("extra reviewer output")
	}
	if !validVerdict(r.Verdict) || !r.Complete || strings.TrimSpace(r.Summary) == "" || r.Findings == nil {
		return r, errors.New("incomplete review")
	}
	for _, f := range r.Findings {
		if rank[f.Severity] == 0 || f.File == "" || f.Line < 1 || f.Title == "" || f.Evidence == "" || f.Suggestion == "" {
			return r, errors.New("invalid finding")
		}
	}
	return r, nil
}
func validVerdict(v string) bool {
	return v == "通过" || v == "有条件通过" || v == "不通过" || v == "证据不足"
}
func (r Result) Passes(threshold string) bool {
	if r.Verdict != "通过" || !r.Complete || rank[threshold] == 0 {
		return false
	}
	for _, f := range r.Findings {
		if rank[f.Severity] == 0 || rank[f.Severity] >= rank[threshold] {
			return false
		}
	}
	return true
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func (c Config) Key(p PR) string {
	return hash([]any{c.GiteaURL, c.Repository, c.BaseBranch, p.Number, p.Head.SHA, p.Base.SHA, c.PolicyVersion, c.BlockThreshold, c.ReviewerCommand, c.ReviewerEnv})
}

type Reviewer interface {
	Review(context.Context, ReviewInput) (Result, error)
}
type Notifier interface {
	Send(context.Context, string, string, string) (string, error)
}
type SubprocessReviewer struct{ Config Config }
type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("review output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func (s SubprocessReviewer) Review(ctx context.Context, in ReviewInput) (Result, error) {
	c := s.Config
	if len(c.ReviewerCommand) == 0 {
		return Result{}, errors.New("reviewer_command required")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.ReviewTimeoutSeconds)*time.Second)
	defer cancel()
	home, e := os.MkdirTemp("", "hermes-review-")
	if e != nil {
		return Result{}, e
	}
	defer os.RemoveAll(home)
	cmd := exec.CommandContext(ctx, c.ReviewerCommand[0], c.ReviewerCommand[1:]...)
	cmd.Dir = home
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "HERMES_HOME=" + filepath.Join(home, "hermes"), "LANG=C.UTF-8", "PYTHONNOUSERSITE=1"}
	allowed := map[string]bool{"REVIEW_MODEL": true, "REVIEW_BASE_URL": true, "REVIEW_API_KEY": true, "REVIEW_HERMES_PATH": true, "REVIEW_MAX_INPUT_BYTES": true, "REVIEW_MAX_TOKENS": true, "REVIEW_MAX_ITERATIONS": true, "REVIEW_TIMEOUT_SECONDS": true}
	for child, parent := range c.ReviewerEnv {
		if !allowed[child] {
			return Result{}, fmt.Errorf("reviewer environment name not allowed: %s", child)
		}
		v, ok := os.LookupEnv(parent)
		if !ok {
			return Result{}, fmt.Errorf("missing reviewer environment variable for %s", child)
		}
		cmd.Env = append(cmd.Env, child+"="+v)
	}
	b, _ := json.Marshal(in)
	cmd.Stdin = bytes.NewReader(b)
	out := &cappedBuffer{limit: 1024 * 1024}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if e = cmd.Run(); e != nil {
		return Result{}, errors.New("reviewer execution failed")
	}
	return DecodeResult(out.Bytes())
}
