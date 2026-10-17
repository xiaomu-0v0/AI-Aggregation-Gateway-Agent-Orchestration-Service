package service

import (
	"context"
	"time"

	chatv1 "chat_service/api/chat/v1"
	"chat_service/internal/biz"

	"google.golang.org/grpc"
)

// ChatService 实现内部的 Chat gRPC 服务。
type ChatService struct {
	chatv1.UnimplementedChatServer
	uc *biz.ChatUsecase
}

// NewChatService 是 Wire 使用的构造函数。
func NewChatService(uc *biz.ChatUsecase) *ChatService {
	return &ChatService{uc: uc}
}

// CreateChatCompletion 调用业务层完成对话生成，并转换为内部 Proto 回复。
func (s *ChatService) CreateChatCompletion(ctx context.Context, in *chatv1.CreateChatCompletionRequest) (*chatv1.CreateChatCompletionReply, error) {
	messages := make([]biz.Message, 0, len(in.Messages))

	for _, msg := range in.Messages {
		messages = append(messages, biz.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	result, err := s.uc.Complete(ctx, in.Model, messages)
	if err != nil {
		return nil, err
	}

	return &chatv1.CreateChatCompletionReply{
		Id:      result.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   in.Model,
		Choices: []*chatv1.ChatCompletionChoice{
			{
				Index: 0,
				Message: &chatv1.ChatCompletionMessage{
					Role:    "assistant",
					Content: result.Content,
				},
				FinishReason: result.FinishReason,
			},
		},
		Usage: &chatv1.ChatCompletionUsage{
			PromptTokens:     result.PromptTokens,
			CompletionTokens: result.CompletionTokens,
			TotalTokens:      result.TotalTokens,
		},
	}, nil
}

func (s *ChatService) StreamChatCompletion(in *chatv1.CreateChatCompletionRequest, stream grpc.ServerStreamingServer[chatv1.ChatCompletionChunk]) error {
	ctx := stream.Context()

	messages := make([]biz.Message, 0, len(in.Messages))

	for _, msg := range in.Messages {
		messages = append(messages, biz.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return s.uc.Stream(
		ctx,
		in.GetModel(),
		messages,
		func(chunk *biz.ModelStreamChunk) error {
			return stream.Send(&chatv1.ChatCompletionChunk{
				Id:           chunk.ID,
				Model:        chunk.Model,
				Content:      chunk.Content,
				FinishReason: chunk.FinishReason,
			})
		},
	)
}
