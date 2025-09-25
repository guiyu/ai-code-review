package dingtalk

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// generateSign 生成钉钉机器人加签签名
func generateSign(secret string) (string, string, error) {
	// 获取当前时间戳（毫秒）
	timestamp := strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10)
	
	// 构造签名字符串：timestamp+"\n"+secret
	stringToSign := timestamp + "\n" + secret
	
	// 使用HMAC-SHA256算法计算签名
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	
	// 对签名进行URL编码
	encodedSign := url.QueryEscape(signature)
	
	return encodedSign, timestamp, nil
}

// buildWebhookURL 构造带签名参数的Webhook URL
func buildWebhookURL(webhookURL, secret string) (string, error) {
	// 如果没有配置secret，则直接返回原始URL
	if secret == "" {
		return webhookURL, nil
	}
	
	// 生成签名和时间戳
	sign, timestamp, err := generateSign(secret)
	if err != nil {
		return "", err
	}
	
	// 构造带签名参数的URL
	// 检查原始URL是否已经包含查询参数
	separator := "?"
	if strings.Contains(webhookURL, "?") {
		separator = "&"
	}
	
	finalURL := fmt.Sprintf("%s%stimestamp=%s&sign=%s", webhookURL, separator, timestamp, sign)
	return finalURL, nil
}

// SendNotification 发送钉钉通知
func SendNotification(webhookURL, secret, content string, atMobiles []string) error {
	// 如果webhookURL为空，则不发送通知
	if webhookURL == "" {
		logger.Debug("Dingtalk webhook URL is empty, skip sending notification")
		return nil
	}

	// 构造带签名的Webhook URL
	finalWebhookURL, err := buildWebhookURL(webhookURL, secret)
	if err != nil {
		logger.Error("Failed to build webhook URL: %v", err)
		return err
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
	resp, err := http.Post(finalWebhookURL, "application/json", bytes.NewBuffer(jsonData))
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

// SendMarkdownNotification 发送钉钉Markdown通知
func SendMarkdownNotification(webhookURL, secret, title, content string) error {
	// 如果webhookURL为空，则不发送通知
	if webhookURL == "" {
		logger.Debug("Dingtalk webhook URL is empty, skip sending notification")
		return nil
	}

	// 构造带签名的Webhook URL
	finalWebhookURL, err := buildWebhookURL(webhookURL, secret)
	if err != nil {
		logger.Error("Failed to build webhook URL: %v", err)
		return err
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
	resp, err := http.Post(finalWebhookURL, "application/json", bytes.NewBuffer(jsonData))
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