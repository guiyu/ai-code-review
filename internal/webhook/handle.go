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
)

type PushPayload struct {
	Repository struct {
		Name string `json:"name"`
	} `json:"repository"`
	Commits []struct {
		Message string `json:"message"`
		URL     string `json:"url"`
	} `json:"commits"`
	Ref string `json:"ref"`
}

func Handler(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}

		// 校验签名
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

		// 模拟Git diff (实际项目应通过Gitea API获取commit diff)
		diff := fmt.Sprintf("Repository: %s\nRef: %s\nCommits: %v",
			payload.Repository.Name, payload.Ref, payload.Commits)

		review, err := ai.ReviewCode(cfg.AIKey, diff)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "AI review completed",
			"review":  review,
		})
	}
}

func validateSignature(payload []byte, secret, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
