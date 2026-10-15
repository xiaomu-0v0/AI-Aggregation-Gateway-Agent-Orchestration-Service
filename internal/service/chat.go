package service

import (
	"context"
	"fmt"
	"time"

	chatv1 "chat_service/api/chat/v1"
)

// ChatService 实现内部的 Chat gRPC 服务。
type ChatService struct {
	chatv1.UnimplementedChatServer
}

// NewChatService 是 Wire 使用的构造函数。
func NewChatService() *ChatService {
	return &ChatService{}
}

// CreateChatCompletion 暂时返回模拟的 AI 回复。
func (s *ChatService) CreateChatCompletion(ctx context.Context, in *chatv1.CreateChatCompletionRequest) (*chatv1.CreateChatCompletionReply, error) {
	// 即使是模拟业务，也尊重请求的取消信号。
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 找到最后一条用户消息。
	var userMessage string
	for i := len(in.Messages) - 1; i >= 0; i-- {
		if in.Messages[i].Role == "user" {
			userMessage = in.Messages[i].Content
			break
		}
	}

	answer := fmt.Sprintf("模拟 AI 回复：我收到了你的问题：%s", userMessage)

	now := time.Now()

	return &chatv1.CreateChatCompletionReply{
		Id:      fmt.Sprintf("chatcmpl-%d", now.UnixNano()),
		Object:  "chat.completion",
		Created: now.Unix(),
		Model:   in.Model,
		Choices: []*chatv1.ChatCompletionChoice{
			{
				Index: 0,
				Message: &chatv1.ChatCompletionMessage{
					Role:    "assistant",
					Content: answer,
				},
				FinishReason: "stop",
			},
		},
		Usage: &chatv1.ChatCompletionUsage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}, nil
}
