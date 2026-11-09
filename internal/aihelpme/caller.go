package aihelpme

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
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
	Stream   bool      `json:"stream,omitempty"`
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

// chatCompletionStream is the streaming variant of chatCompletion: the API
// is called with stream=true and every delta is forwarded to onDelta as it
// arrives. It returns the accumulated answer and logs the full conversation
// at the end, exactly like the blocking path. A mid-stream error yields the
// partial answer plus the error.
func chatCompletionStream(ctx context.Context, cfg *Config, model string, messages []message, onDelta func(string)) (answer string, err error) {
	start := time.Now()
	slog.Debug("api call start",
		"model", model,
		"workspace", cfg.WorkspaceID,
		"stream", true,
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
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages, Stream: true})
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
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var parsed chatResponse
		if json.Unmarshal(raw, &parsed) == nil && parsed.Error != nil {
			return "", fmt.Errorf("%s：%s", parsed.Error.Code, parsed.Error.Message)
		}
		return "", fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncate(string(raw), 200))
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue // 无法解析的行（心跳等）直接跳过
		}
		if chunk.Error != nil {
			return answer, fmt.Errorf("%s：%s", chunk.Error.Code, chunk.Error.Message)
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				answer += c.Delta.Content
				if onDelta != nil {
					onDelta(c.Delta.Content)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return answer, fmt.Errorf("读取流式响应失败：%w", err)
	}
	if answer == "" {
		return "", fmt.Errorf("空响应（stream）")
	}
	return answer, nil
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
