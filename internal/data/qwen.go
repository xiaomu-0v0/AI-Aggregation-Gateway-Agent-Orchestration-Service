package data

import (
	"bufio"
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
		client:  &http.Client{},
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
	}, nil
}

type qwenRequest struct {
	Model         string             `json:"model"`
	Messages      []qwenMessage      `json:"messages"`
	Stream        bool               `json:"stream"`
	StreamOptions *qwenStreamOptions `json:"stream_options,omitempty"`
}

type qwenStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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

type qwenStreamResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`

	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`

		FinishReason string `json:"finish_reason"`
	} `json:"choices"`

	Usage *struct {
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

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

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

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
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

func (p *QwenProvider) StreamGenerate(ctx context.Context, model string, messages []biz.Message, emit func(*biz.ModelStreamChunk) error) error {
	if model != "chat-default" {
		return fmt.Errorf("unsupported model: %s", model)
	}

	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute+30*time.Second)
	defer cancel()

	// 1.构造流式请求
	reqBody := qwenRequest{
		Model:  p.model,
		Stream: true,
		StreamOptions: &qwenStreamOptions{
			IncludeUsage: true,
		},
	}

	for _, msg := range messages {
		reqBody.Messages = append(reqBody.Messages, qwenMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal qwen stream request: %w", err)
	}

	// 2. 创建一个绑定调用方 Context 的 HTTP 请求。
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create qwen stream request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// 3. 发起请求。成功后，不要立即读取整个 Body。
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("call qwen stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return fmt.Errorf(
				"qwen stream returned HTTP %d; read error: %w",
				resp.StatusCode,
				readErr,
			)
		}

		return fmt.Errorf(
			"qwen stream returned HTTP %d: %s",
			resp.StatusCode,
			string(raw),
		)
	}

	// 4. 逐行读取 SSE，而不是等待完整回答。
	scanner := bufio.NewScanner(resp.Body)

	// 默认 Scanner 的单行大小限制比较小。
	// 将上限提高到 1 MiB，避免较大的事件导致扫描失败。
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	receiveDone := false

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}

		line := strings.TrimSpace(scanner.Text())

		// 忽略空行和非 data 字段（注释行、id:/event: 等）。
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(
			strings.TrimPrefix(line, "data:"),
		)

		if data == "" {
			continue
		}

		// 供应商用这个标志表示流结束。
		if data == "[DONE]" {
			receiveDone = true
			break
		}

		var chunk qwenStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("decode qwen stream chunk: %w", err)
		}

		// 一个数据块可能没有 choices，例如只携带 usage。
		for _, choice := range chunk.Choices {
			content := choice.Delta.Content

			// 如果既没有内容，也没有结束原因，就不必向上发送。
			if content == "" && choice.FinishReason == "" {
				continue
			}

			err := emit(&biz.ModelStreamChunk{
				ID:           chunk.ID,
				Model:        chunk.Model,
				Content:      content,
				FinishReason: choice.FinishReason,
			})
			if err != nil {
				return err
			}
		}

		// 用量可能出现在 choices 为空的数据块中。
		if chunk.Usage != nil {
			err := emit(&biz.ModelStreamChunk{
				ID:    chunk.ID,
				Model: chunk.Model,
				Usage: &biz.ModelUsage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				},
			})
			if err != nil {
				return err
			}
		}
	}

	// 5. 区分正常结束、读取错误和异常中断。

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read qwen stream: %w", err)
	}

	if !receiveDone {
		return fmt.Errorf("qwen stream ended before [DONE]")
	}

	return nil
}
