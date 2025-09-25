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

	prompt :=
	"请审查以下代码diff差异并提供详细的代码审查意见\n" +
	"针对新增的代码，提供改进意见，包括但不限于代码质量、可读性、性能、安全性等方面，给出你觉得需要改进的代码并显示改进前后的代码差异\n" +
	"针对删除的代码，提出可能会造成的影响，并提出建议\n" +
	"最后，综合所有更改做个言简意赅的总结\n"

	req := openai.ChatCompletionRequest{
		Model: aiModel,
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: prompt},
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
