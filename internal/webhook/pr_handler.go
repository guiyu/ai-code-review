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
	"bucking.cn/code-review/internal/gitea"
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
		body, _ := io.ReadAll(c.Request.Body)

		signature := c.GetHeader("X-Gitea-Signature")
		if !validateSignature(body, cfg.WebhookSecret, signature) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}

		var payload PullRequestPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}

		if payload.Action != "opened" && payload.Action != "synchronize" {
			c.JSON(http.StatusOK, gin.H{"message": "ignored action"})
			return
		}

		owner := payload.PullRequest.Head.Repo.Owner.Login
		repo := payload.PullRequest.Head.Repo.Name
		prNum := payload.Number

		// 在真实生产中，这里应该调用 Gitea Diff API 获取diff
		// 这里用PR Body代替示例
		diff := fmt.Sprintf("PR Title: %s\nPR Body: %s", payload.PullRequest.Title, payload.PullRequest.Body)

		// 调用AI审查
		review, err := ai.ReviewCode(cfg.AIBaseURL, cfg.AIModel, cfg.AIKey, diff)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 调用Gitea API发表评论
		client := gitea.NewClient(cfg.GiteaBaseURL, cfg.GiteaToken)
		comment := "🤖 **AI代码审查结果**\n\n" + review
		println(comment)
		if err := client.PostPRComment(owner, repo, prNum, comment); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "post comment failed"})
			return
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
