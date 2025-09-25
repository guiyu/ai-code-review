package webhook

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"bucking.cn/code-review/internal/ai"
	"bucking.cn/code-review/internal/config"
	"bucking.cn/code-review/internal/gitea"
	"bucking.cn/code-review/internal/logger"
)

// Gitea Push Webhook Payload
type PushPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		Name string `json:"name"`
		Owner struct {
			Name string `json:"name"`
		} `json:"owner"`
	} `json:"repository"`
	Commits []struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		URL     string `json:"url"`
	} `json:"commits"`
}

func PushHandler(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.Info("Received Push webhook request")
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			logger.Error("Failed to read request body: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}

		// 签名校验
		signature := c.GetHeader("X-Gitea-Signature")
		if !validateSignature(body, cfg.WebhookSecret, signature) {
			logger.Warn("Invalid signature received")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}

		var payload PushPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			logger.Error("Failed to unmarshal payload: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}

		if len(payload.Commits) == 0 {
			logger.Info("No commits in push event")
			c.JSON(http.StatusOK, gin.H{"message": "no commits"})
			return
		}

		owner := payload.Repository.Owner.Name
		repo := payload.Repository.Name
		logger.Info("Processing push event for %s/%s with %d commits", owner, repo, len(payload.Commits))
		
		client := gitea.NewClient(cfg.GiteaBaseURL, cfg.GiteaToken)

		for _, commit := range payload.Commits {
			logger.Info("Processing commit: %s", commit.ID)
			// 获取commit diff
			logger.Debug("Fetching diff for commit: %s, %s, %s ", owner, repo, commit.ID)
			diff, err := client.GetCommitDiff(owner, repo, commit.ID)
			if err != nil {
				logger.Error("Failed to get commit diff for %s: %v", commit.ID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "get commit diff failed"})
				return
			}
			
			if diff == "" {
				logger.Warn("Empty diff for commit: %s", commit.ID)
				continue
			}

			// 调用AI审查
			logger.Info("Calling AI for code review of commit: %s", commit.ID)
			review, err := ai.ReviewCode(cfg.AIBaseURL, cfg.AIModel, cfg.AIKey, diff)
			if err != nil {
				logger.Error("AI review failed for commit %s: %v", commit.ID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// 发表评论
			comment := "🤖 **AI代码审查结果**\n\n" + review
			logger.Debug("Comment content for commit %s: %s", commit.ID, comment)
			if err := client.PostCommitComment(owner, repo, commit.ID, comment); err != nil {
				logger.Error("Failed to post comment to commit %s: %v", commit.ID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "post commit comment failed"})
				return
			}
			logger.Info("Successfully posted AI review to commit: %s", commit.ID)
		}

		logger.Info("Successfully processed push event for %s/%s", owner, repo)
		c.JSON(http.StatusOK, gin.H{"message": "AI review comments posted to commits"})
	}
}