package biz

import (
	"context"
	"fmt"
)

// Message 是业务层的聊天消息。
type Message struct {
	Role    string
	Content string
}

// ModelResult 是模型生成结果。
type ModelResult struct {
	ID               string
	Model            string
	Content          string
	FinishReason     string
	PromptTokens     int32
	CompletionTokens int32
	TotalTokens      int32
}

// ModelStreamChunk 表示模型流式生成的一小段内容。
type ModelStreamChunk struct {
	ID           string
	Model        string
	Content      string
	FinishReason string
}

// ModelProvider 是业务层需要的模型能力。
type ModelProvider interface {
	Generate(
		ctx context.Context,
		model string,
		message []Message,
	) (*ModelResult, error)
}

// StreamingModelProvider 表示支持流式生成的模型能力。
type StreamingModelProvider interface {
	StreamGenerate(
		ctx context.Context,
		model string,
		messages []Message,
		emit func(*ModelStreamChunk) error,
	) error
}

// ChatUsecase 负责聊天业务。
type ChatUsecase struct {
	provider ModelProvider
}

func NewChatUsecase(provider ModelProvider) *ChatUsecase {
	return &ChatUsecase{provider: provider}
}

func (uc *ChatUsecase) Complete(ctx context.Context, model string, messages []Message) (*ModelResult, error) {
	return uc.provider.Generate(ctx, model, messages)
}

func (uc *ChatUsecase) Stream(ctx context.Context, model string, messages []Message, emit func(*ModelStreamChunk) error) error {
	provider, ok := uc.provider.(StreamingModelProvider)
	if !ok {
		return fmt.Errorf("model provider does not support streaming")
	}

	return provider.StreamGenerate(ctx, model, messages, emit)
}
