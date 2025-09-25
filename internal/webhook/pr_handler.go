package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"bucking.cn/code-review/internal/ai"
	"bucking.cn/code-review/internal/config"
	"bucking.cn/code-review/internal/dingtalk"
	"bucking.cn/code-review/internal/gitea"
	"bucking.cn/code-review/internal/logger"
)

type PullRequestPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  struct {
			Repo struct {
				Name  string `json:"name"`
				Owner struct {
					Login string `json:"login"`
				} `json:"owner"`
			} `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

func PRHandler(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.Info("Received PR webhook request")
		body, _ := io.ReadAll(c.Request.Body)

		signature := c.GetHeader("X-Gitea-Signature")
		if !validateSignature(body, cfg.WebhookSecret, signature) {
			logger.Warn("Invalid signature received")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}

		var payload PullRequestPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			logger.Error("Failed to unmarshal payload: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}

		if payload.Action != "opened" && payload.Action != "synchronize" {
			logger.Info("Ignoring action: %s", payload.Action)
			c.JSON(http.StatusOK, gin.H{"message": "ignored action"})
			return
		}

		owner := payload.PullRequest.Head.Repo.Owner.Login
		repo := payload.PullRequest.Head.Repo.Name
		prNum := payload.Number

		logger.Info("Processing PR #%d for %s/%s", prNum, owner, repo)

		// 在真实生产中，这里应该调用 Gitea Diff API 获取diff
		// 这里用PR Body代替示例
		// diff := fmt.Sprintf("PR Title: %s\nPR Body: %s", payload.PullRequest.Title, payload.PullRequest.Body)
		client := gitea.NewClient(cfg.GiteaBaseURL, cfg.GiteaToken)

		diff, err := client.GetPRDiff(owner, repo, prNum)
		if err != nil {
			logger.Error("Failed to get PR diff: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "get pr diff failed"})
			return
		}

		// 调用AI审查
		logger.Info("Calling AI for code review")
		review, err := ai.ReviewCode(cfg.AIBaseURL, cfg.AIModel, cfg.AIKey, diff)
		if err != nil {
			logger.Error("AI review failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 调用Gitea API发表评论
		comment := "🤖 **AI代码审查结果**\n\n" + review
		logger.Debug("Comment content: %s", comment)
		if err := client.PostPRComment(owner, repo, prNum, comment); err != nil {
			logger.Error("Failed to post comment to PR: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "post comment failed"})
			return
		}

		logger.Info("Successfully posted AI review to PR #%d", prNum)

		// 发送钉钉通知
		if cfg.DingtalkWebhookURL != "" {
			title := fmt.Sprintf("AI代码审查完成 - PR #%d", prNum)
			content := fmt.Sprintf("## AI代码审查完成  \n\n**项目**: %s/%s  \n**PR编号**: #%d  \n**PR标题**: %s  \n\n[查看PR](%s/repos/%s/%s/pulls/%d)  ", 
				owner, repo, prNum, payload.PullRequest.Title, cfg.GiteaBaseURL, owner, repo, prNum)
			
			err := dingtalk.SendMarkdownNotification(cfg.DingtalkWebhookURL, title, content)
			if err != nil {
				logger.Error("Failed to send dingtalk notification: %v", err)
				// 如果Markdown格式发送失败，尝试发送普通文本通知
				notificationContent := fmt.Sprintf("AI代码审查完成\n项目: %s/%s\nPR #%d: %s", owner, repo, prNum, payload.PullRequest.Title)
				err = dingtalk.SendNotification(cfg.DingtalkWebhookURL, notificationContent, []string{})
				if err != nil {
					logger.Error("Failed to send dingtalk text notification: %v", err)
				} else {
					logger.Info("Dingtalk text notification sent for PR #%d", prNum)
				}
			} else {
				logger.Info("Dingtalk markdown notification sent for PR #%d", prNum)
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "AI review posted to PR"})
	}
}

func validateSignature(payload []byte, secret, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
