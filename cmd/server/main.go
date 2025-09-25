package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"bucking.cn/code-review/internal/config"	
	"bucking.cn/code-review/internal/logger"
	"bucking.cn/code-review/internal/webhook"
)

func main() {
	cfg := config.Load()
	
	// Set Gin mode based on config
	if cfg.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	
	// Initialize logger
	logLevel := logger.INFO
	switch cfg.LogLevel {
	case "DEBUG":
		logLevel = logger.DEBUG
	case "WARN":
		logLevel = logger.WARN
	case "ERROR":
		logLevel = logger.ERROR
	}
	
	if err := logger.Init(logLevel, cfg.LogFilePath); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		return
	}
	defer logger.Close()
	
	logger.Info("Starting server in %s mode...", cfg.Mode)
	
	r := gin.Default()

	// Pull Request Webhook
	r.POST("/webhook/pr", webhook.PRHandler(cfg))
	r.POST("/webhook/push", webhook.PushHandler(cfg))


	addr := fmt.Sprintf(":%s", cfg.Port)
	logger.Info("🚀 Server running on %s", addr)
	r.Run(addr)
}
