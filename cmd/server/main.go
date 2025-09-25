package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"bucking.cn/code-review/internal/config"	
	"bucking.cn/code-review/internal/webhook"
)

func main() {
	cfg := config.Load()
	r := gin.Default()

	// Pull Request Webhook
	r.POST("/webhook/pr", webhook.PRHandler(cfg))

	addr := fmt.Sprintf(":%s", cfg.Port)
	fmt.Printf("🚀 Server running on %s\n", addr)
	r.Run(addr)
}
