package gitea

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	BaseURL string
	Token   string
}

func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token}
}

// PostPRComment 在PR中发表评论
func (c *Client) PostPRComment(owner, repo string, prNumber int, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/comments", c.BaseURL, owner, repo, prNumber)

	data := map[string]string{"body": body}
	b, _ := json.Marshal(data)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token "+c.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("failed to post comment, status: %s", resp.Status)
	}
	return nil
}


// PostCommitComment 在指定 commit 下发表评论
func (c *Client) PostCommitComment(owner, repo, sha, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s/comments",
		c.BaseURL, owner, repo, sha)

	data := map[string]string{"body": body}
	b, _ := json.Marshal(data)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token "+c.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("failed to post commit comment, status: %s", resp.Status)
	}
	return nil
}
