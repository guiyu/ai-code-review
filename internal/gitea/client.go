package gitea

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"bucking.cn/code-review/internal/logger"
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

// PostIssueComment 在指定 issue 下发表评论
func (c *Client) PostIssueComment(owner, repo string, issueNumber int, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		c.BaseURL, owner, repo, issueNumber)

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
		return fmt.Errorf("failed to post issue comment, status: %s", resp.Status)
	}
	return nil
}

// GetPRDiff 获取指定 PR 的真实 diff 内容
func (c *Client) GetPRDiff(owner, repo string, prNumber int) (string, error) {
	// 构建 API URL
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d.diff", c.BaseURL, owner, repo, prNumber)
	logger.Debug("GetPRDiff URL: %s", url)

	// 创建请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置认证 header
	req.Header.Set("Authorization", "token "+c.Token)
	req.Header.Set("Accept", "application/vnd.gitea.v1.diff") // 可选，确保返回 diff

	// 发起请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("请求失败，状态码: %d, 返回: %s", resp.StatusCode, string(body))
	}

	// 读取 diff
	diffBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	return string(diffBytes), nil
}

// GetCommitDiff 获取指定 commit 的 diff 内容
func (c *Client) GetCommitDiff(owner, repo, commitID string) (string, error) {
	// 构建 API URL
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s.diff", c.BaseURL, owner, repo, commitID)
	logger.Debug("GetCommitDiff URL: %s", url)

	// 创建请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置认证 header
	req.Header.Set("Authorization", "token "+c.Token)
	req.Header.Set("Accept", "application/vnd.gitea.v1.diff") // 可选，确保返回 diff

	// 发起请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("请求失败，状态码: %d, 返回: %s", resp.StatusCode, string(body))
	}

	// 读取 diff
	diffBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	logger.Debug("Diff: %s", string(diffBytes))

	return string(diffBytes), nil
}
