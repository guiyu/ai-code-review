package ai

import (
	"context"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

func ReviewCode(aiBaseURL, aiModel, aiKey, diff string) (string, error) {
	// client := openai.NewClient(aiKey)
	config := openai.DefaultConfig(aiKey)
	config.BaseURL = aiBaseURL
	client := openai.NewClientWithConfig(config)

	req := openai.ChatCompletionRequest{
		Model: aiModel,
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: "你是一个专业的代码审查员。请审查以下Git diff，找出潜在问题并提供改进建议。"},
			{Role: "user", Content: diff},
		},
	}

	resp, err := client.CreateChatCompletion(context.Background(), req)
	if err != nil {
		return "", err
	}
	println(resp.Choices[0].Message.Content)

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
