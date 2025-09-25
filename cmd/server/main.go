package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	
	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	
	// Create HTTP server
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}
	
	// Start server in a goroutine
	go func() {
		logger.Info("🚀 Server running on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start server: %v", err)
		}
	}()
	
	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")
	
	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown: %v", err)
	}
	
	logger.Info("Server exited")
}
