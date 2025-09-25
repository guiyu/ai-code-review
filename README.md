# Code Review Bot

一个基于AI的代码审查机器人，可以自动审查Gitea中的Pull Request。

## 功能特性

- 自动监听Gitea的Pull Request事件
- 使用AI对代码变更进行智能审查
- 在PR中自动添加审查评论

## 快速开始

### 环境要求

- Go 1.19+
- Gitea实例

### 安装

```bash
go mod tidy
```

### 配置

复制 `.env.example` 文件为 `.env` 并填写相应配置：

```bash
cp .env.example .env
```

配置项说明：

- `PORT`: 服务监听端口
- `MODE`: 运行模式 (debug 或 release)
- `AI_BASE_URL`: AI API基础URL
- `AI_MODEL`: AI模型名称
- `AI_API_KEY`: AI API密钥
- `WEBHOOK_SECRET`: Gitea Webhook密钥
- `GITEA_TOKEN`: Gitea访问令牌
- `GITEA_BASE_URL`: Gitea API基础URL
- `LOG_LEVEL`: 日志级别 (DEBUG, INFO, WARN, ERROR)
- `LOG_FILE_PATH`: 日志文件路径 (留空则输出到控制台)

### 运行

```bash
go run cmd/server/main.go
```

## 日志模块

本项目包含一个内置的日志模块，支持以下特性：

- 多级别日志记录 (DEBUG, INFO, WARN, ERROR)
- 可配置的日志输出位置 (控制台或文件)
- 时间戳和日志级别前缀

使用方法：

```go
import "bucking.cn/code-review/internal/logger"

// 初始化日志模块
logger.Init(logger.INFO, "/path/to/logfile.log")
defer logger.Close()

// 记录不同级别的日志
logger.Debug("调试信息: %s", debugInfo)
logger.Info("一般信息: %s", info)
logger.Warn("警告信息: %s", warning)
logger.Error("错误信息: %v", err)
```

## 许可证

MIT