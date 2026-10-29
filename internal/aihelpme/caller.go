package aihelpme

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest mirrors the compatible-mode chat completion request body.
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ChatCompletion calls the OpenAI-compatible chat completions endpoint
// synchronously and returns the assistant reply for question, prepending
// cfg.SystemPrompt when it is set.
//
// The API key is read from the DASHSCOPE_API_KEY environment variable. The
// HTTP client enforces cfg.CallTimeout, so a hung request surfaces as an
// error instead of tying up the worker forever. A non-200 response, an
// error payload, or an empty reply is returned as an error.
func ChatCompletion(ctx context.Context, cfg *Config, question string) (string, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("缺少 DASHSCOPE_API_KEY 环境变量")
	}
	messages := []message{{Role: "user", Content: question}}
	if cfg.SystemPrompt != "" {
		messages = append([]message{{Role: "system", Content: cfg.SystemPrompt}}, messages...)
	}
	body, err := json.Marshal(chatRequest{Model: cfg.Model, Messages: messages})
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://%s.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/chat/completions", cfg.WorkspaceID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: cfg.CallTimeout}).Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("响应解析失败（HTTP %d）：%s", resp.StatusCode, truncate(string(raw), 200))
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("%s：%s", parsed.Error.Code, parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncate(string(raw), 200))
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("空响应（HTTP %d）", resp.StatusCode)
	}
	return parsed.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
