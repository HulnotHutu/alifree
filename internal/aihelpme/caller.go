package aihelpme

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
	"unicode/utf8"
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
// cfg.SystemPrompt when it is set. model is the effective model (it may
// have been switched at runtime via PUT /model, so it comes in explicitly
// instead of from cfg). See chatCompletion for the shared call semantics.
func ChatCompletion(ctx context.Context, cfg *Config, model, question string) (string, error) {
	messages := []message{{Role: "user", Content: question}}
	if cfg.SystemPrompt != "" {
		messages = append([]message{{Role: "system", Content: cfg.SystemPrompt}}, messages...)
	}
	return chatCompletion(ctx, cfg, model, messages)
}

// chatCompletion is the daemon's only external call: one synchronous
// compatible-mode request whose outcome is logged (debug start, info done,
// error failed) no matter which path returns. The API key is read from
// DASHSCOPE_API_KEY per call; cfg.CallTimeout bounds a hung request; a
// non-200 response, an error payload, or an empty reply comes back as an
// error.
func chatCompletion(ctx context.Context, cfg *Config, model string, messages []message) (answer string, err error) {
	start := time.Now()
	slog.Debug("api call start",
		"model", model,
		"workspace", cfg.WorkspaceID,
		"turns", len(messages))
	defer func() {
		if err != nil {
			slog.Error("api call failed",
				"model", model,
				"duration_ms", time.Since(start).Milliseconds(),
				"err", err)
			return
		}
		slog.Info("api call done",
			"model", model,
			"duration_ms", time.Since(start).Milliseconds(),
			"answer_chars", utf8.RuneCountInString(answer))
		logConversation(model, messages, answer)
	}()

	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("缺少 DASHSCOPE_API_KEY 环境变量")
	}
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages})
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

// logConversation emits the full exchange (system prompt, every
// user/assistant turn, and the assistant reply) as one structured record.
// Results live only in daemon memory, so this log line is the durable trace
// of a prompt-test or job conversation, reviewable via journalctl.
func logConversation(model string, messages []message, answer string) {
	turns := make([]map[string]string, 0, len(messages)+1)
	for _, m := range messages {
		turns = append(turns, map[string]string{"role": m.Role, "content": m.Content})
	}
	turns = append(turns, map[string]string{"role": "assistant", "content": answer})
	slog.Info("conversation", "model", model, "turns", turns)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
