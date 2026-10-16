package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"chat_service/internal/biz"
)

type QwenProvider struct {
	client  *http.Client
	apiKey  string
	baseURL string
	model   string
}

func NewQwenProvider() (*QwenProvider, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY is empty")
	}

	baseURL := strings.TrimRight(os.Getenv("QWEN_BASE_URL"), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("QWEN_BASE_URL is empty")
	}

	model := os.Getenv("QWEN_MODEL")
	if model == "" {
		model = "qwen-plus"
	}

	return &QwenProvider{
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
	}, nil
}

type qwenRequest struct {
	Model    string        `json:"model"`
	Messages []qwenMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type qwenMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type qwenResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int32 `json:"prompt_tokens"`
		CompletionTokens int32 `json:"completion_tokens"`
		TotalTokens      int32 `json:"total_tokens"`
	} `json:"usage"`
}

func (p *QwenProvider) Generate(ctx context.Context, model string, message []biz.Message) (*biz.ModelResult, error) {

	// 第一版只支持一个逻辑模型名。
	if model != "chat-default" {
		return nil, fmt.Errorf("unsupported model: %s", model)
	}

	reqBody := qwenRequest{
		Model:  p.model,
		Stream: false,
	}

	for _, msg := range message {
		reqBody.Messages = append(
			reqBody.Messages,
			qwenMessage{
				Role:    msg.Role,
				Content: msg.Content,
			},
		)
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal qwen request: %w", err)
	}

	url := p.baseURL + "/chat/completions"

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create qwen request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer"+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call qwen: %w", err)
	}
	defer resp.Body.Close()

	// 限制响应体大小，避免异常响应占用过多内存。
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read qwen response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qwen returned HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var result qwenResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode qwen response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("qwen returned empty choices")
	}

	return &biz.ModelResult{
		ID:               result.ID,
		Model:            result.Model,
		Content:          result.Choices[0].Message.Content,
		FinishReason:     result.Choices[0].FinishReason,
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		TotalTokens:      result.Usage.TotalTokens,
	}, nil
}
