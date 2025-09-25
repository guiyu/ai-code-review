# Code Review Bot

An AI-powered code review bot that automatically reviews Pull Requests and Push events in Gitea.

[中文文档](README-zh.md)

## 功能特性

- 自动监听Gitea的Pull Request事件
- 自动监听Gitea的Push事件
- 使用AI对代码变更进行智能审查
- 在PR和Commit中自动添加审查评论
- 支持多种AI模型（DeepSeek、OpenAI等）
- 可配置的日志系统
- Webhook签名验证确保安全性
- 钉钉通知功能（PR审查完成后发送通知）

## 项目架构

```
code-review/
├── cmd/
│   └── server/          # 服务入口
│       └── main.go
├── internal/
│   ├── ai/              # AI代码审查逻辑
│   ├── config/          # 配置管理
│   ├── dingtalk/        # 钉钉通知模块
│   ├── gitea/           # Gitea API客户端
│   ├── logger/          # 日志模块
│   └── webhook/         # Webhook处理
└── log/                 # 日志文件目录
```

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
- `DINGTALK_WEBHOOK_URL`: 钉钉机器人Webhook URL (可选，用于发送通知)

### 运行

```bash
go run cmd/server/main.go
```

或者编译后运行：

```bash
go build -o code-review cmd/server/main.go
./code-review
```

## Webhook配置

在Gitea中配置Webhook：

1. 进入仓库设置 -> Webhooks
2. 添加Webhook
3. 设置URL为: `http://your-domain:port/webhook/pr` (PR事件)
4. 设置URL为: `http://your-domain:port/webhook/push` (Push事件)
5. 内容类型选择: `application/json`
6. 密钥填写与`.env`中`WEBHOOK_SECRET`相同的值
7. 选择触发事件:
   - 对于PR Webhook: 选择"Pull Request"
   - 对于Push Webhook: 选择"Push Events"

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

## 支持的AI模型

本项目支持任何兼容OpenAI API的模型，包括：

- DeepSeek (默认配置)
- OpenAI GPT系列
- 阿里通义千问
- 百度文心一言
- 腾讯混元

只需在`.env`文件中配置相应的`AI_BASE_URL`和`AI_MODEL`即可。

## 钉钉通知功能

### 钉钉通知功能

本项目支持在PR代码审查完成后发送钉钉通知。要启用此功能，请执行以下步骤：

1. 在钉钉群中创建自定义机器人并获取Webhook URL
2. 在`.env`文件中配置`DINGTALK_WEBHOOK_URL`环境变量

当PR审查完成后，系统会自动向配置的钉钉群发送通知，包含以下信息：
- 项目名称
- PR编号和标题
- 到PR页面的链接

如果钉钉机器人启用了加签安全设置，系统会自动使用配置的签名密钥对通知进行签名，确保通知的安全性。

## 许可证

MIT