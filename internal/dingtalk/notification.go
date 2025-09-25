package dingtalk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"bucking.cn/code-review/internal/logger"
)

// Message 钉钉消息结构体
type Message struct {
	MsgType string `json:"msgtype"`
	Text    Text   `json:"text"`
	At      At     `json:"at"`
}

// Text 文本消息内容
type Text struct {
	Content string `json:"content"`
}

// At @相关人员
type At struct {
	AtMobiles []string `json:"atMobiles"`
	IsAtAll   bool     `json:"isAtAll"`
}

// SendNotification 发送钉钉通知
func SendNotification(webhookURL, content string, atMobiles []string) error {
	// 如果webhookURL为空，则不发送通知
	if webhookURL == "" {
		logger.Debug("Dingtalk webhook URL is empty, skip sending notification")
		return nil
	}

	// 构造消息
	message := Message{
		MsgType: "text",
		Text: Text{
			Content: fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04:05"), content),
		},
		At: At{
			AtMobiles: atMobiles,
			IsAtAll:   false,
		},
	}

	// 序列化消息
	jsonData, err := json.Marshal(message)
	if err != nil {
		logger.Error("Failed to marshal dingtalk message: %v", err)
		return err
	}

	// 发送POST请求
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("Failed to send dingtalk notification: %v", err)
		return err
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		logger.Error("Dingtalk notification response status: %s", resp.Status)
		return fmt.Errorf("dingtalk notification failed with status: %s", resp.Status)
	}

	logger.Info("Dingtalk notification sent successfully")
	return nil
}

// SendMarkdownNotification 发送Markdown格式的通知
func SendMarkdownNotification(webhookURL, title, content string) error {
	// 如果webhookURL为空，则不发送通知
	if webhookURL == "" {
		logger.Debug("Dingtalk webhook URL is empty, skip sending notification")
		return nil
	}

	// 构造Markdown消息
	markdownMessage := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  content,
		},
	}

	// 序列化消息
	jsonData, err := json.Marshal(markdownMessage)
	if err != nil {
		logger.Error("Failed to marshal dingtalk markdown message: %v", err)
		return err
	}

	// 发送POST请求
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("Failed to send dingtalk markdown notification: %v", err)
		return err
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		logger.Error("Dingtalk markdown notification response status: %s", resp.Status)
		return fmt.Errorf("dingtalk markdown notification failed with status: %s", resp.Status)
	}

	logger.Info("Dingtalk markdown notification sent successfully")
	return nil
}