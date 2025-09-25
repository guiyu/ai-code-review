# 钉钉通知模块

该模块提供了向钉钉群发送通知的功能，支持文本和Markdown格式的消息。

## 功能特性

- 发送文本格式通知
- 发送Markdown格式通知
- 支持@指定用户
- 支持钉钉机器人加签安全机制
- 自动处理空URL情况（不发送通知）

## 配置说明

在环境变量中配置钉钉Webhook URL：

```env
DINGTALK_WEBHOOK_URL=https://oapi.dingtalk.com/robot/send?access_token=your_access_token_here
```

## 使用示例

### 发送文本通知

```go
import "bucking.cn/code-review/internal/dingtalk"

// 发送带签名的文本通知
err := dingtalk.SendNotification(
    webhookURL,              // Webhook URL
    "your_sign_secret",       // 签名密钥，如果不需要签名则传空字符串
    "这是一条测试消息",        // 消息内容
    []string{"13800138000"}  // @的手机号列表
)
if err != nil {
    log.Fatalf("发送通知失败: %v", err)
}
```

### 发送Markdown通知

```go
import "bucking.cn/code-review/internal/dingtalk"

// 发送带签名的Markdown格式通知
webhookURL := "https://oapi.dingtalk.com/robot/send?access_token=your_token"
secret := "your_sign_secret"  // 签名密钥，如果不需要签名则传空字符串
title := "代码审查完成"
content := "## 代码审查结果  \n\n**项目**: test/project  \n**状态**: 审查完成  \n**时间**: 2024-01-01 12:00:00"

err := dingtalk.SendMarkdownNotification(webhookURL, secret, title, content)
if err != nil {
    log.Fatalf("发送Markdown通知失败: %v", err)
}
```

## 钉钉机器人配置

1. 打开钉钉客户端，进入目标群聊
2. 点击右上角菜单，选择"群设置"
3. 选择"智能群助手"
4. 点击"添加机器人"
5. 选择"自定义机器人"
6. 设置机器人名称和头像
7. 选择安全设置（建议使用加签方式）
8. 记录Webhook URL，格式如：`https://oapi.dingtalk.com/robot/send?access_token=your_token`

### 加签安全设置

如果启用了加签安全设置，需要获取签名密钥（secret）并将其配置到环境变量中：

1. 在钉钉机器人设置页面启用"加签"选项
2. 复制生成的签名密钥
3. 将签名密钥配置到环境变量`DINGTALK_WEBHOOK_SECRET`中

## 注意事项

- 如果环境变量`DINGTALK_WEBHOOK_URL`为空或未设置，将不会发送通知
- 消息内容会自动添加时间戳前缀
- 网络错误会记录到日志中但不会中断主流程