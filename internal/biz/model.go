package biz

import "context"

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

// ModelProvider 是业务层需要的模型能力。
type ModelProvider interface {
	Generate(
		ctx context.Context,
		model string,
		message []Message,
	) (*ModelResult, error)
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
