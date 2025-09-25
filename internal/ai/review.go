package ai

import (
	"context"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

func ReviewCode(aiKey, diff string) (string, error) {
	client := openai.NewClient(aiKey)

	req := openai.ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: "你是一个专业的代码审查员。请审查以下Git diff，找出潜在问题并提供改进建议。"},
			{Role: "user", Content: diff},
		},
	}

	resp, err := client.CreateChatCompletion(context.Background(), req)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
