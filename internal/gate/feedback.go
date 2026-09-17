package gate

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Discussion is a bounded, immutable input snapshot, not trusted instructions.
type ReviewFeedback struct {
	ID        int64  `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
}
type Discussion struct {
	Feedback       []ReviewFeedback `json:"feedback"`
	PreviousReview string           `json:"previous_review"`
}

var reportMarker = regexp.MustCompile(`^<!-- hermes-review:[a-f0-9]{64} -->`)

func (a *API) Comments(ctx context.Context, n int) ([]Comment, error) {
	all := []Comment{}
	seen := map[int64]bool{}
	for page := 1; page <= 100; page++ {
		var batch []Comment
		if e := a.JSON(ctx, "GET", fmt.Sprintf("%s/issues/%d/comments?limit=50&page=%d", a.repo(), n, page), nil, &batch); e != nil {
			return nil, e
		}
		if batch == nil {
			return nil, errors.New("missing PR discussion")
		}
		for _, item := range batch {
			if item.ID <= 0 || seen[item.ID] {
				return nil, errors.New("invalid or repeated PR comment")
			}
			seen[item.ID] = true
			all = append(all, item)
		}
		if len(batch) < 50 {
			sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
			return all, nil
		}
	}
	return nil, errors.New("PR discussion pagination limit")
}
func (c *Controller) discussion(ctx context.Context, p PR) (Discussion, error) {
	comments, e := c.API.Comments(ctx, p.Number)
	if e != nil {
		return Discussion{}, e
	}
	d := Discussion{}
	var anchor int64
	total := 0
	for _, comment := range comments {
		if comment.User.Login == c.Config.BotUsername {
			if reportMarker.MatchString(comment.Body) {
				if anchor == 0 {
					anchor = comment.ID
				}
				d.PreviousReview = comment.Body
			}
			continue
		}
		if anchor == 0 || comment.User.IsBot || strings.EqualFold(comment.User.Type, "bot") || strings.HasSuffix(comment.User.Login, "[bot]") || strings.TrimSpace(comment.Body) == "" {
			continue
		}
		total += len(comment.Body)
		if len(d.Feedback) >= 100 || len(comment.Body) > 32000 || total > 64000 {
			return Discussion{}, errors.New("PR feedback exceeds review limit")
		}
		d.Feedback = append(d.Feedback, ReviewFeedback{ID: comment.ID, Author: comment.User.Login, Body: comment.Body, UpdatedAt: comment.UpdatedAt})
	}
	if len(d.Feedback) == 0 {
		return Discussion{}, nil
	}
	if len(d.PreviousReview) > 64000 {
		return Discussion{}, errors.New("previous report exceeds review limit")
	}
	return d, nil
}
func (d Discussion) key(base string) string {
	if len(d.Feedback) == 0 {
		return base
	}
	// Exclude bot output: appending our own report cannot trigger another review.
	return hash([]any{base, d.Feedback})
}
func (c *Controller) currentDiscussion(ctx context.Context, p PR, key string) error {
	if e := c.API.Current(ctx, p); e != nil {
		return e
	}
	d, e := c.discussion(ctx, p)
	if e != nil {
		return errors.Join(e, c.invalidateDiscussion(ctx, p, "error", key))
	}
	if d.key(c.Config.Key(p)) != key {
		return errors.Join(errors.New("PR discussion changed; review latest feedback next poll"), c.invalidateDiscussion(ctx, p, "pending", d.key(c.Config.Key(p))))
	}
	return nil
}

// Mark previous snapshots obsolete without deleting their reports/history.
func (c *Controller) activate(r *Run) error {
	changed := false
	for _, old := range c.Store.Runs {
		if old != r && old.Scope == c.scope() && old.PR.Number == r.PR.Number && !old.Superseded {
			old.Superseded = true
			changed = true
		}
	}
	if r.Superseded {
		r.Superseded = false
		r.PublicationInvalid = true
		changed = true
	}
	if changed {
		return c.Store.Save()
	}
	return nil
}

// Persist invalidation before publishing a temporary status so recovery restores
// the final check even when its previous value in the store was already success.
func (c *Controller) invalidateDiscussion(ctx context.Context, p PR, state, key string) error {
	for _, r := range c.Store.Runs {
		if r.Scope == c.scope() && r.PR.Number == p.Number {
			r.PublicationInvalid = true
		}
	}
	if e := c.Store.Save(); e != nil {
		return e
	}
	return c.API.Status(ctx, p, state, "", key)
}
