package webhook

import (
	// "crypto/hmac"
	// "crypto/sha256"
	// "encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"bucking.cn/code-review/internal/ai"
	"bucking.cn/code-review/internal/config"
	"bucking.cn/code-review/internal/gitea"
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
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}

		// 签名校验
		signature := c.GetHeader("X-Gitea-Signature")
		if !validateSignature(body, cfg.WebhookSecret, signature) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}

		var payload PushPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}

		if len(payload.Commits) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "no commits"})
			return
		}

		owner := payload.Repository.Owner.Name
		repo := payload.Repository.Name
		client := gitea.NewClient(cfg.GiteaBaseURL, cfg.GiteaToken)

		for _, commit := range payload.Commits {
			// 这里用 commit message 模拟 diff，可改为调用 Gitea Diff API 获取真实 diff
			// diff := fmt.Sprintf("Commit: %s\nMessage: %s", commit.ID, commit.Message)
			diff, err := client.GetCommitDiff(owner, repo, commit.ID)
			if err != nil || diff == "" {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "get commit diff failed"})
				return
			}

			review, err := ai.ReviewCode(cfg.AIBaseURL, cfg.AIModel, cfg.AIKey, diff)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			comment := "🤖 **AI代码审查结果**\n\n" + review
			if err := client.PostCommitComment(owner, repo, commit.ID, comment); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "post commit comment failed"})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "AI review comments posted to commits"})
	}
}