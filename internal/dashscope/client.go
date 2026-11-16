// Package dashscope 是阿里云百炼模型推理的轻量客户端（OpenAI 兼容协议）。
//
// 凭证使用模型 API Key（sk-…，DASHSCOPE_API_KEY），与控制台 AK/SK 体系无关；
// 推理调用消耗各模型的免费额度，用完即停开启时超额会收到
// 403 AllocationQuota.FreeTierOnly.。
package dashscope

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const baseURL = "https://dashscope.aliyuncs.com"

// Client 调用百炼模型推理 API。
type Client struct {
	HTTP *http.Client
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 180 * time.Second}}
}

// APIError 携带服务端返回的错误码与描述。
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s（%s）", e.Message, e.Code)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// IsFreeTierExhausted 识别「用完即停」触发的 403。
func IsFreeTierExhausted(err error) bool {
	var apiErr *APIError
	if ok := asAPIError(err, &apiErr); ok {
		return strings.Contains(apiErr.Code, "FreeTierOnly")
	}
	return false
}

func asAPIError(err error, target **APIError) bool {
	if e, ok := err.(*APIError); ok {
		*target = e
		return true
	}
	return false
}

// do 发送 JSON 请求并返回响应体（非 2xx 时解析为 APIError）。
func (c *Client) do(ctx context.Context, method, path, key string, payload any, extraHeaders ...map[string]string) (json.RawMessage, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	for _, hs := range extraHeaders {
		for k, v := range hs {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr APIError
		apiErr.Status = resp.StatusCode
		if json.Unmarshal(data, &apiErr) != nil || apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(string(data))
			if len(apiErr.Message) > 300 {
				apiErr.Message = apiErr.Message[:300]
			}
		}
		return nil, &apiErr
	}
	return json.RawMessage(data), nil
}

// ChatCompletion 调用 OpenAI 兼容的对话接口（文本 / 全模态 / 语音识别共用）。
// req 为完整请求体：{model, messages: [...], modalities?: ["text"]}。
func (c *Client) ChatCompletion(ctx context.Context, key string, req map[string]any) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPost, "/compatible-mode/v1/chat/completions", key, req)
}

// Embeddings 调用向量化接口。
func (c *Client) Embeddings(ctx context.Context, key, model string, inputs []string) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPost, "/compatible-mode/v1/embeddings", key, map[string]any{
		"model": model, "input": inputs,
	})
}

// --- 图像/视频生成：创建异步任务 + 轮询 -------------------------------------

// CreateImageTask 创建文生图任务（qwen-image 系列，异步）。
func (c *Client) CreateImageTask(ctx context.Context, key, model, prompt, size string) (string, error) {
	parameters := map[string]any{}
	if size != "" {
		parameters["size"] = size
	}
	raw, err := c.do(ctx, http.MethodPost,
		"/api/v1/services/aigc/text2image/image-synthesis", key, map[string]any{
			"model": model, "input": map[string]any{"prompt": prompt},
			"parameters": parameters,
		}, map[string]string{"X-DashScope-Async": "enable"})
	if err != nil {
		return "", err
	}
	return extractTaskID(raw)
}

// CreateVideoTask 创建视频生成任务（wan 系列，异步）。
func (c *Client) CreateVideoTask(ctx context.Context, key, model, prompt string) (string, error) {
	raw, err := c.do(ctx, http.MethodPost,
		"/api/v1/services/aigc/video-generation/video-synthesis", key, map[string]any{
			"model": model, "input": map[string]any{"prompt": prompt},
		}, map[string]string{"X-DashScope-Async": "enable"})
	if err != nil {
		return "", err
	}
	return extractTaskID(raw)
}

// PollTask 查询异步任务状态与结果。
type TaskResult struct {
	Status  string   `json:"status"`  // PENDING / RUNNING / SUCCEEDED / FAILED
	URLs    []string `json:"urls"`    // 产物 URL（图片或视频）
	Message string   `json:"message"` // 失败原因
}

func (c *Client) PollTask(ctx context.Context, key, taskID string) (*TaskResult, error) {
	raw, err := c.do(ctx, http.MethodGet, "/api/v1/tasks/"+taskID, key, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Output struct {
			TaskStatus string `json:"task_status"`
			Message    string `json:"message"`
			Results    []struct {
				URL string `json:"url"`
			} `json:"results"`
			VideoURL string `json:"video_url"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	res := &TaskResult{Status: out.Output.TaskStatus, Message: out.Output.Message}
	if out.Output.VideoURL != "" {
		res.URLs = append(res.URLs, out.Output.VideoURL)
	}
	for _, r := range out.Output.Results {
		if r.URL != "" {
			res.URLs = append(res.URLs, r.URL)
		}
	}
	return res, nil
}

func extractTaskID(raw json.RawMessage) (string, error) {
	var out struct {
		Output struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
			Results    []struct {
				URL string `json:"url"`
			} `json:"results"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Output.TaskID != "" {
		return out.Output.TaskID, nil
	}
	// 同步返回结果的情况（理论上带 Async 头不会发生）
	if len(out.Output.Results) > 0 {
		return "", nil
	}
	return "", fmt.Errorf("响应中缺少 task_id：%s", raw)
}
