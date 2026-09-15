package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

func (a *API) Protection(ctx context.Context) (map[string]any, error) {
	var p map[string]any
	e := a.JSON(ctx, "GET", a.repo()+"/branch_protections/"+url.PathEscape(a.Config.BaseBranch), nil, &p)
	return p, e
}
func stringsOf(v any) []string {
	var out []string
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		for _, v := range s {
			if x, ok := v.(string); ok {
				out = append(out, x)
			}
		}
	}
	return out
}
func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func (a *API) ProtectionPlan(existing map[string]any) map[string]any {
	p := map[string]any{}
	for k, v := range existing {
		p[k] = v
	}
	delete(p, "created_at")
	delete(p, "updated_at")
	p["rule_name"] = a.Config.BaseBranch
	p["branch_name"] = a.Config.BaseBranch
	p["enable_push"] = false
	p["enable_push_whitelist"] = false
	p["push_whitelist_deploy_keys"] = false
	p["push_whitelist_usernames"] = []string{}
	p["push_whitelist_teams"] = []string{}
	p["dismiss_stale_approvals"] = true
	p["ignore_stale_approvals"] = false
	p["enable_force_push"] = false
	p["enable_force_push_allowlist"] = false
	p["enable_merge_whitelist"] = true
	p["merge_whitelist_usernames"] = []string{a.Config.BotUsername}
	p["merge_whitelist_teams"] = []string{}
	p["enable_status_check"] = true
	p["block_on_outdated_branch"] = true
	p["block_admin_merge_override"] = true
	p["unprotected_file_patterns"] = ""
	checks := stringsOf(p["status_check_contexts"])
	if !contains(checks, StatusContext) {
		checks = append(checks, StatusContext)
	}
	p["status_check_contexts"] = checks
	return p
}
func (a *API) AuditProtection(ctx context.Context) error {
	var branch struct {
		Protected bool   `json:"protected"`
		Effective string `json:"effective_branch_protection_name"`
	}
	if e := a.JSON(ctx, "GET", a.repo()+"/branches/"+url.PathEscape(a.Config.BaseBranch), nil, &branch); e != nil {
		return e
	}
	if !branch.Protected || branch.Effective != a.Config.BaseBranch {
		return errors.New("configured protection is not the effective branch rule")
	}
	p, e := a.Protection(ctx)
	if e != nil {
		return e
	}
	for _, k := range []string{"enable_merge_whitelist", "enable_status_check", "block_on_outdated_branch", "block_admin_merge_override", "dismiss_stale_approvals"} {
		if p[k] != true {
			return fmt.Errorf("protection missing %s", k)
		}
	}
	if p["enable_push"] != false || p["enable_force_push"] != false || p["enable_force_push_allowlist"] != false {
		return errors.New("direct or force push remains enabled")
	}
	if p["ignore_stale_approvals"] != false {
		return errors.New("stale approvals must not be ignored")
	}
	if v, _ := p["unprotected_file_patterns"].(string); v != "" {
		return errors.New("unprotected file patterns bypass gate")
	}
	if !reflect.DeepEqual(stringsOf(p["merge_whitelist_usernames"]), []string{a.Config.BotUsername}) || len(stringsOf(p["merge_whitelist_teams"])) != 0 {
		return errors.New("merge whitelist must contain only controller bot")
	}
	if !contains(stringsOf(p["status_check_contexts"]), StatusContext) {
		return errors.New("required Hermes check missing")
	}
	return nil
}
func (a *API) Identity(ctx context.Context) error {
	var u User
	if e := a.JSON(ctx, "GET", "/user", nil, &u); e != nil {
		return e
	}
	if u.Login != a.Config.BotUsername || u.ID <= 0 {
		return errors.New("authenticated account differs from controller bot")
	}
	var repo struct {
		Permissions map[string]bool `json:"permissions"`
	}
	if e := a.JSON(ctx, "GET", a.repo(), nil, &repo); e != nil {
		return e
	}
	if !repo.Permissions["admin"] {
		return errors.New("controller needs repository administrator permission for protection management")
	}
	return nil
}
func (a *API) Preflight(ctx context.Context) error {
	if e := a.Identity(ctx); e != nil {
		return e
	}
	return a.AuditProtection(ctx)
}
func (a *API) Protect(ctx context.Context, apply bool) (map[string]any, error) {
	if e := a.Identity(ctx); e != nil {
		return nil, e
	}
	old, e := a.Protection(ctx)
	create := false
	if e != nil {
		var ae *APIError
		if errors.As(e, &ae) && ae.Code == 404 {
			create = true
		} else {
			return nil, e
		}
	}
	p := a.ProtectionPlan(old)
	if !apply {
		return p, nil
	}
	method := "PATCH"
	path := a.repo() + "/branch_protections/" + url.PathEscape(a.Config.BaseBranch)
	if create {
		method = "POST"
		path = a.repo() + "/branch_protections"
	}
	if e = a.JSON(ctx, method, path, p, nil); e != nil {
		return p, e
	}
	return p, a.AuditProtection(ctx)
}
func (c *Controller) Merge(ctx context.Context, n int, expectedHead string) error {
	if n < 1 || !shaPattern.MatchString(expectedHead) {
		return errors.New("PR number and expected head SHA required")
	}
	if e := c.API.Preflight(ctx); e != nil {
		return e
	}
	p, e := c.API.Pull(ctx, n)
	if e != nil {
		return e
	}
	if p.Head.SHA != expectedHead {
		return errors.New("expected head does not match current PR")
	}
	if !p.Mergeable || p.MergeBase != p.Base.SHA {
		return errors.New("PR must be mergeable and contain current target branch")
	}
	prs, e := c.API.Pulls(ctx)
	if e != nil {
		return e
	}
	for _, other := range prs {
		if other.Number != n && other.Head.SHA == p.Head.SHA {
			return errors.New("head shared by another open PR")
		}
	}
	r := c.Store.Runs[c.Config.Key(p)]
	if r == nil || r.Result == nil || !r.Result.Passes(c.Config.BlockThreshold) || r.Status != "success" || r.ReportID <= 0 {
		return errors.New("no trusted complete passing review for current PR/head/base/policy")
	}
	data, _ := json.Marshal(r.Result)
	if _, err := DecodeResult(data); err != nil {
		return errors.New("persisted review is invalid")
	}
	if r.ReportBody != c.report(r) {
		return errors.New("persisted report mismatch")
	}
	var comment Comment
	if e = c.API.JSON(ctx, "GET", fmt.Sprintf("%s/issues/comments/%d", c.API.repo(), r.ReportID), nil, &comment); e != nil {
		return e
	}
	if comment.Body != r.ReportBody || comment.User.Login != c.Config.BotUsername {
		return errors.New("published review report missing or modified")
	}
	var statuses struct {
		Statuses []struct {
			Context     string `json:"context"`
			Status      string `json:"status"`
			Description string `json:"description"`
			TargetURL   string `json:"target_url"`
			Creator     User   `json:"creator"`
		} `json:"statuses"`
	}
	if e = c.API.JSON(ctx, "GET", c.API.repo()+"/commits/"+url.PathEscape(p.Head.SHA)+"/status", nil, &statuses); e != nil {
		return e
	}
	found := false
	for _, s := range statuses.Statuses {
		if s.Context == StatusContext {
			found = true
			if s.Status != "success" || s.Creator.Login != c.Config.BotUsername || s.Description != "Hermes review "+r.Key[:12] || s.TargetURL != r.ReportURL {
				return errors.New("current Hermes status is not the trusted passing run")
			}
			break
		}
	}
	if !found {
		return errors.New("required success status absent")
	}
	if e = c.API.Current(ctx, p); e != nil {
		return e
	}
	if !strings.EqualFold(expectedHead, p.Head.SHA) {
		return errors.New("stale head")
	}
	return c.API.JSON(ctx, "POST", fmt.Sprintf("%s/pulls/%d/merge", c.API.repo(), n), map[string]any{"Do": "merge", "head_commit_id": expectedHead, "force_merge": false, "merge_when_checks_succeed": false}, nil)
}
