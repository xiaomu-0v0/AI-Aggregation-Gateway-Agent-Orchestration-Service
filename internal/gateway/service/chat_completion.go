package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	chatv1 "chat_service/api/chat/v1"
	gatewaymiddleware "chat_service/internal/gateway/middleware"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	kmetadata "github.com/go-kratos/kratos/v2/metadata"
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

type SSEChatCompletionChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []SSEChoice `json:"choices"`
}

type SSEChoice struct {
	Index        int32    `json:"index"`
	Delta        SSEDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type SSEDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
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

	if req.Stream {
		return s.handleStream(ctx, grpcReq)
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

// requestID 从 metadata 中取出网关入口生成的 request ID，用于把日志串起来。
func requestID(ctx context.Context) string {
	md, ok := kmetadata.FromServerContext(ctx)
	if !ok {
		return ""
	}

	ids := md.Values(gatewaymiddleware.RequestIDKey)
	if len(ids) == 0 {
		return ""
	}

	return ids[0]
}

func (s *ChatCompletionService) handleStream(ctx khttp.Context, grpcReq *chatv1.CreateChatCompletionRequest) error {
	// 总调用时限。HTTP 请求取消时，这个 Context 也会被取消。
	rpcCtx, cancel := context.WithTimeout(ctx.Request().Context(), 60*time.Second)
	defer cancel()

	// 1. 创建 gRPC 流。
	stream, err := s.client.StreamChatCompletion(rpcCtx, grpcReq)
	if err != nil {
		return writeAPIError(
			ctx,
			http.StatusBadGateway,
			"failed to start chat stream",
			"server_error",
			"",
			"upstream_error",
		)
	}

	// 2. 检查 ResponseWriter 是否支持 Flush。
	w := ctx.Response()
	flusher, ok := w.(http.Flusher)
	if !ok {
		return writeAPIError(
			ctx,
			http.StatusInternalServerError,
			"HTTP streaming is not supported",
			"server_error",
			"",
			"stream_unsupported",
		)
	}

	// 3. 设置 SSE 响应头。
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	// 4. 从这里开始，HTTP 状态和响应头就不能随意更改了。
	w.WriteHeader(http.StatusOK)

	// 尽可能让客户端立即收到响应头。
	flusher.Flush()

	// 整个流共用一个 ID 和创建时间。
	completionID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	// 5. 先发送 assistant 角色事件。
	if err := writeSSE(w, flusher, SSEChatCompletionChunk{
		ID:      completionID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   grpcReq.GetModel(),
		Choices: []SSEChoice{
			{
				Index: 0,
				Delta: SSEDelta{
					Role: "assistant",
				},
				FinishReason: nil,
			},
		},
	}); err != nil {
		// 客户端可能已经断开，不再继续发送。
		return nil
	}

	// 6. 持续读取 gRPC 流。
	for {
		chunk, err := stream.Recv()

		if err == io.EOF {
			// 正常结束
			break
		}

		if err != nil {
			// HTTP 200 已经发出，不能再改成 502/504。
			// 使用 SSE 事件表达流中途发生的错误。
			log.Errorf(
				"chat stream interrupted: request_id=%s model=%s err=%v",
				requestID(ctx.Request().Context()),
				grpcReq.GetModel(),
				err,
			)

			_ = writeSSE(w, flusher, map[string]interface{}{
				"error": APIErrorDetail{
					Message: "chat stream interrupted",
					Type:    "server_error",
					Param:   "",
					Code:    "stream_interrupted",
				},
			})
			return nil
		}

		var finishReason *string
		if chunk.GetFinishReason() != "" {
			reason := chunk.GetFinishReason()
			finishReason = &reason
		}

		// 7. 将内部 gRPC chunk 转成外部 SSE JSON。
		event := SSEChatCompletionChunk{
			ID:      completionID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   grpcReq.GetModel(),
			Choices: []SSEChoice{
				{
					Index: 0,
					Delta: SSEDelta{
						Content: chunk.GetContent(),
					},
					FinishReason: finishReason,
				},
			},
		}

		if err := writeSSE(w, flusher, event); err != nil {
			// 不再继续消耗下游模型流。
			return nil
		}
	}

	// 8. 按 OpenAI 风格发送流结束标记。
	_, err = fmt.Fprint(w, "data: [DONE]\n\n")
	if err != nil {
		return nil
	}

	flusher.Flush()
	return nil
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event interface{}) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}

	flusher.Flush()
	return nil
}
