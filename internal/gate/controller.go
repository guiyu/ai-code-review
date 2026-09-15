package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Controller struct {
	Config   Config
	API      *API
	Store    *Store
	Reviewer Reviewer
	Notifier Notifier
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

func (c *Controller) Once(ctx context.Context) error {
	prs, e := c.API.Pulls(ctx)
	if e != nil {
		return errors.Join(e, c.drainOutbox(ctx))
	}
	counts := map[string]int{}
	for _, p := range prs {
		counts[p.Head.SHA]++
	}
	var errs []error
	for _, p := range prs {
		if p.Base.Ref != c.Config.BaseBranch || p.Draft {
			continue
		}
		if counts[p.Head.SHA] > 1 {
			// Persist invalidation before changing the commit-scoped remote status.
			for _, r := range c.Store.Runs {
				if r.Scope == c.scope() && r.PR.Head.SHA == p.Head.SHA {
					r.PublicationInvalid = true
				}
			}
			if e = c.Store.Save(); e != nil {
				errs = append(errs, e)
				continue
			}
			e = c.API.Status(ctx, p, "error", "", c.Config.Key(p))
			errs = append(errs, errors.Join(errors.New("ambiguous head shared by open PRs"), e))
			continue
		}
		if e = c.process(ctx, p); e != nil {
			errs = append(errs, fmt.Errorf("PR %d: %w", p.Number, e))
		}
	}

	errs = append(errs, c.drainOutbox(ctx))
	return errors.Join(errs...)
}
func (c *Controller) drainOutbox(ctx context.Context) error {
	var errs []error
	for _, r := range c.Store.Runs {
		if r.Scope == c.scope() && r.ReportID != 0 && r.Status != "" && !r.Notified {
			if e := c.notify(ctx, r); e != nil {
				errs = append(errs, e)
			}
		}
	}
	return errors.Join(errs...)
}
func (c *Controller) process(ctx context.Context, p PR) error {
	if !shaPattern.MatchString(p.Head.SHA) || !shaPattern.MatchString(p.Base.SHA) || p.Number < 1 {
		return errors.New("invalid PR commits")
	}
	if e := c.API.Current(ctx, p); e != nil {
		return e
	}
	key := c.Config.Key(p)
	r := c.Store.Runs[key]
	if r == nil {
		r = &Run{Key: key, PR: p, Scope: c.scope()}
		c.Store.Runs[key] = r
		if e := c.Store.Save(); e != nil {
			return e
		}
	}
	if r.Result == nil && r.ReviewError != "" && r.Attempts < 3 {
		if time.Now().Unix() < r.NextAttemptUnix {
			return errors.New("review retry scheduled")
		}
		r.ReviewError = ""
	}
	if r.Result == nil && r.ReviewError == "" {
		if e := c.API.Status(ctx, p, "pending", "", key); e != nil {
			return e
		}
		var diff string
		if e := c.API.request(ctx, "GET", fmt.Sprintf("%s/pulls/%d.diff", c.API.repo(), p.Number), nil, &diff, c.Config.MaxDiffBytes); e != nil {
			return e
		}
		if e := c.API.Current(ctx, p); e != nil {
			return e
		}
		commits, e := c.API.PullCommits(ctx, p.Number)
		if e != nil {
			return e
		}
		foundHead := false
		for _, commit := range commits {
			if commit.SHA == p.Head.SHA {
				foundHead = true
			}
		}
		if !foundHead {
			return errors.New("PR commit history missing reviewed head")
		}
		if e := c.API.Current(ctx, p); e != nil {
			return e
		}
		r.Attempts++
		if e := c.Store.Save(); e != nil {
			return e
		}
		res, e := c.Reviewer.Review(ctx, ReviewInput{Repository: c.Config.Repository, Number: p.Number, HeadSHA: p.Head.SHA, BaseSHA: p.Base.SHA, Diff: diff, Title: p.Title, Description: p.Body, HeadRef: p.Head.Ref, BaseRef: p.Base.Ref, MergeBase: p.MergeBase, Commits: commits})
		if e == nil {
			data, _ := json.Marshal(res)
			res, e = DecodeResult(data)
		}
		if e != nil {
			r.ReviewError = reviewFailureMessage(e)
			r.NextAttemptUnix = time.Now().Add(time.Duration(30*(1<<r.Attempts)) * time.Second).Unix()
		} else {
			r.Result = &res
		}
		if e = c.Store.Save(); e != nil {
			return e
		}
	}
	if r.Result == nil && r.ReviewError != "" && r.Attempts < 3 {
		return fmt.Errorf("%s；已安排重试", r.ReviewError)
	}
	if e := c.API.Current(ctx, p); e != nil {
		return e
	}
	if r.ReportBody == "" {
		r.ReportBody = c.report(r)
		if e := c.Store.Save(); e != nil {
			return e
		}
	}
	if r.ReportID == 0 {
		comment, e := c.findReport(ctx, r)
		if e != nil {
			return e
		}
		if comment.ID == 0 {
			if e := c.API.JSON(ctx, "POST", fmt.Sprintf("%s/issues/%d/comments", c.API.repo(), p.Number), map[string]string{"body": r.ReportBody}, &comment); e != nil {
				return e
			}
		}
		if comment.ID <= 0 {
			return errors.New("report missing ID")
		}
		r.ReportID = comment.ID
		r.ReportURL = comment.HTMLURL
		if r.ReportURL == "" {
			r.ReportURL = fmt.Sprintf("%s/%s/pulls/%d#issuecomment-%d", strings.TrimRight(c.Config.GiteaURL, "/"), c.Config.Repository, p.Number, comment.ID)
		}
		if e := c.Store.Save(); e != nil {
			return e
		}
	}
	state := "error"
	if r.Result != nil {
		state = "failure"
		if r.Result.Passes(c.Config.BlockThreshold) {
			state = "success"
		}
	}
	if e := c.API.Current(ctx, p); e != nil {
		return e
	}
	if r.Status != state || r.PublicationInvalid {
		if e := c.API.Status(ctx, p, state, r.ReportURL, key); e != nil {
			return e
		}
		r.Status = state
		r.PublicationInvalid = false
		if e := c.Store.Save(); e != nil {
			return e
		}
	}
	return nil
}
func (c *Controller) report(r *Run) string {
	body := fmt.Sprintf("<!-- hermes-review:%s -->\n## 自动代码评审\nPR #%d · 提交 `%s` · 目标分支提交 `%s` · 策略 `%s`\n\n", r.Key, r.PR.Number, r.PR.Head.SHA, r.PR.Base.SHA, c.Config.PolicyVersion)
	if r.Result == nil {
		return body + r.ReviewError
	}
	body += r.Result.Summary + "\n\n"
	for _, f := range r.Result.Findings {
		body += fmt.Sprintf("### %s: %s\n`%s:%d`\n\n证据：%s\n\n修复建议及验证方法：%s\n\n", f.Severity, f.Title, f.File, f.Line, f.Evidence, f.Suggestion)
	}
	if r.Result.Passes(c.Config.BlockThreshold) {
		body += "**合并门禁：通过**"
	} else {
		body += "**合并门禁：禁止合入**"
	}
	return body
}
func (c *Controller) notify(ctx context.Context, r *Run) error {
	recipient := r.Recipient
	if c.Config.NotificationMode == "feishu_group" {
		recipient = "feishu_group"
	}
	if recipient == "" {
		recipient = c.Config.Identities[strconv.FormatInt(r.PR.User.ID, 10)]
		r.Recipient = recipient
	}
	if recipient == "" {
		r.NotifyError = "missing author Feishu mapping"
		if e := c.Store.Save(); e != nil {
			return e
		}
		return errors.New(r.NotifyError)
	}
	if c.Notifier == nil {
		r.NotifyError = "Feishu credentials unavailable"
	} else {
		uuid := hash([]string{r.Key, recipient})[:32]
		msg, e := c.Notifier.Send(ctx, recipient, c.notification(r), uuid)
		if e == nil {
			r.Notified = true
			r.MessageID = msg
			r.NotifyError = ""
		} else {
			r.NotifyError = "Feishu delivery failed"
		}
	}
	if e := c.Store.Save(); e != nil {
		return e
	}
	if r.NotifyError != "" {
		return errors.New(r.NotifyError)
	}
	return nil
}

// Retry deliberately resets only an unsuccessful execution, never a completed review.
func (c *Controller) Retry(ctx context.Context, n int) error {
	if n < 1 {
		return errors.New("PR number required")
	}
	p, e := c.API.Pull(ctx, n)
	if e != nil {
		return e
	}
	key := c.Config.Key(p)
	r := c.Store.Runs[key]
	if r == nil || r.ReviewError == "" || r.Result != nil {
		return errors.New("no failed execution to retry")
	}
	delete(c.Store.Runs, key)
	return c.Store.Save()
}
func (c *Controller) findReport(ctx context.Context, r *Run) (Comment, error) {
	for page := 1; page <= 1000; page++ {
		var comments []Comment
		if e := c.API.JSON(ctx, "GET", fmt.Sprintf("%s/issues/%d/comments?limit=50&page=%d", c.API.repo(), r.PR.Number, page), nil, &comments); e != nil {
			return Comment{}, e
		}
		for _, comment := range comments {
			if comment.Body == r.ReportBody && comment.User.Login == c.Config.BotUsername {
				return comment, nil
			}
		}
		if len(comments) < 50 {
			return Comment{}, nil
		}
	}
	return Comment{}, errors.New("comment pagination limit")
}
func (c *Controller) notification(r *Run) string {
	author := r.PR.User.Login
	if author == "" {
		author = "未提供用户名"
	}
	body := fmt.Sprintf("Oasis 代码评审 %s #%d：%s\n评审提交 %s，目标分支提交 %s；本报告仅适用于此版本组合。\n", c.Config.Repository, r.PR.Number, r.Status, r.PR.Head.SHA, r.PR.Base.SHA)
	body += fmt.Sprintf("PR 提交人：%s（Gitea ID：%d）\n", author, r.PR.User.ID)
	if r.Result != nil {
		body += r.Result.Summary + "\n"
		counts := map[string]int{}
		for _, f := range r.Result.Findings {
			counts[f.Severity]++
		}
		body += fmt.Sprintf("P0=%d P1=%d P2=%d P3=%d 提示=%d\n", counts["critical"], counts["high"], counts["medium"], counts["low"], counts["info"])
		for i, f := range r.Result.Findings {
			if i == 3 {
				break
			}
			body += fmt.Sprintf("%s %s:%d %s\n", f.Severity, f.File, f.Line, f.Title)
		}
	} else {
		body += r.ReviewError + "\n"
	}
	if len(body) > 12000 {
		body = string([]rune(body)[:2000]) + "\n[完整内容请查看评审报告]\n"
	}
	return body + r.ReportURL
}

func (c *Controller) scope() string {
	return hash([]string{c.Config.GiteaURL, c.Config.Repository, c.Config.BaseBranch})
}
