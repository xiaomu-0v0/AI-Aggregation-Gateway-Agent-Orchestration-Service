package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	chatv1 "chat_service/api/chat/v1"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ChatCompletionService 负责对外的 Chat Completions HTTP API。
type ChatCompletionService struct {
	client chatv1.ChatClient
}

func NewChatCompletionService(client chatv1.ChatClient) *ChatCompletionService {
	return &ChatCompletionService{
		client: client,
	}
}

// HTTP 请求结构：这是外部 API 的契约，不是内部 Proto 类型。
type ChatCompletionRequest struct {
	Model    string       `json:"model"`
	Messages []APIMessage `json:"messages"`
	Stream   bool         `json:"stream"`
}

type APIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []APIChoice `json:"choices"`
	Usage   APIUsage    `json:"usage"`
}

type APIChoice struct {
	Index        int32      `json:"index"`
	Message      APIMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type APIUsage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
	TotalTokens      int32 `json:"total_tokens"`
}

type APIErrorResponse struct {
	Error APIErrorDetail `json:"error"`
}

type APIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

// Handle 是 POST /v1/chat/completions 的处理函数。
func (s *ChatCompletionService) Handler(ctx khttp.Context) error {
	var req ChatCompletionRequest

	if err := ctx.Bind(&req); err != nil {
		return writeAPIError(
			ctx,
			http.StatusBadRequest,
			"invalid JSON request body",
			"invalid_request_error",
			"",
			"invalid_request",
		)
	}

	if strings.TrimSpace(req.Model) == "" {
		return writeAPIError(
			ctx,
			http.StatusBadRequest,
			"model is required",
			"invalid_request_error",
			"model",
			"invalid_request",
		)
	}

	if len(req.Messages) == 0 {
		return writeAPIError(
			ctx,
			http.StatusBadRequest,
			"messages cannot be empty",
			"invalid_request_error",
			"messages",
			"invalid_request",
		)
	}

	if req.Stream {
		return writeAPIError(
			ctx,
			http.StatusBadRequest,
			"stream=true is not supported yet",
			"invalid_request_error",
			"stream",
			"unsupported_value",
		)
	}

	// 校验消息角色，并转换为内部 Proto 请求。
	grpcReq := &chatv1.CreateChatCompletionRequest{
		Model: req.Model,
	}

	for i, message := range req.Messages {
		switch message.Role {
		case "system", "user", "assistant":
		default:
			return writeAPIError(
				ctx,
				http.StatusBadRequest,
				"unsupported message role",
				"invalid_request_error",
				"messages",
				"invalid_role",
			)
		}

		if strings.TrimSpace(message.Content) == "" {
			return writeAPIError(
				ctx,
				http.StatusBadRequest,
				"message content cannot be empty",
				"invalid_request_error",
				"messages",
				"invalid_request",
			)
		}

		_ = i

		grpcReq.Messages = append(
			grpcReq.Messages,
			&chatv1.ChatMessage{
				Role:    message.Role,
				Content: message.Content,
			},
		)
	}

	// 本次先设置 15 秒总调用时限。
	rpcCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	reply, err := s.client.CreateChatCompletion(rpcCtx, grpcReq)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded), status.Code(err) == codes.DeadlineExceeded:
			return writeAPIError(
				ctx,
				http.StatusGatewayTimeout,
				"chat service request timed out",
				"server_error",
				"",
				"timeout",
			)

		case status.Code(err) == codes.Unavailable, kerrors.Reason(err) == "CIRCUITBREAKER":
			return writeAPIError(
				ctx,
				http.StatusServiceUnavailable,
				"chat service temporarily unavailable",
				"server_error",
				"",
				"service_unavailable",
			)

		default:
			return writeAPIError(
				ctx,
				http.StatusBadGateway,
				"chat service request failed",
				"server_error",
				"",
				"upstream_error",
			)
		}
	}

	// 把内部 Proto 回复转换为外部 HTTP JSON 契约。
	resp := ChatCompletionResponse{
		ID:      reply.GetId(),
		Object:  reply.GetObject(),
		Created: reply.GetCreated(),
		Model:   reply.GetModel(),
	}

	for _, choice := range reply.GetChoices() {
		var message APIMessage

		if choice.GetMessage() != nil {
			message = APIMessage{
				Role:    choice.GetMessage().GetRole(),
				Content: choice.GetMessage().GetContent(),
			}
		}

		resp.Choices = append(resp.Choices, APIChoice{
			Index:        choice.GetIndex(),
			Message:      message,
			FinishReason: choice.GetFinishReason(),
		})
	}

	if reply.GetUsage() != nil {
		resp.Usage = APIUsage{
			PromptTokens:     reply.GetUsage().GetPromptTokens(),
			CompletionTokens: reply.GetUsage().GetCompletionTokens(),
			TotalTokens:      reply.GetUsage().GetTotalTokens(),
		}
	}

	return ctx.JSON(http.StatusOK, resp)
}

func writeAPIError(ctx khttp.Context, statusCode int, message string, errType string, param string, code string) error {
	return ctx.JSON(statusCode, APIErrorResponse{
		Error: APIErrorDetail{
			Message: message,
			Type:    errType,
			Param:   param,
			Code:    code,
		},
	})
}
