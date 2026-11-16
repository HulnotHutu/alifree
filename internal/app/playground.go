package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/HulnotHutu/dashscope-call/internal/dashscope"
)

// dashScopeKey 读取模型 API Key：环境变量优先，其次凭证文件里的同名键。
// 每次请求都重读，凭证文件更新后无需重启服务。
func dashScopeKey(envFile string) string {
	if v := os.Getenv("DASHSCOPE_API_KEY"); v != "" {
		return v
	}
	if fileVars, err := godotenv.Read(envFile); err == nil {
		return fileVars["DASHSCOPE_API_KEY"]
	}
	return ""
}

func (s *server) handleKeyStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": dashScopeKey(s.envFile) != "",
	})
}

// inferEnvelope 是测试台推理接口的统一响应。
type inferEnvelope struct {
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Code      string          `json:"code,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	LatencyMs int64           `json:"latencyMs,omitempty"`
}

// inferErr 把 dashscope 错误转成面向用户的提示。
func inferErr(err error) inferEnvelope {
	if de, ok := err.(*dashscope.APIError); ok {
		msg := de.Error()
		if dashscope.IsFreeTierExhausted(err) {
			msg = "该模型的免费额度已用尽（用完即停已开启，返回 403）。" +
				"可换其他有额度的模型，或在本页对应模型上关闭用完即停（将转按量计费）。"
		}
		if strings.Contains(de.Code, "InvalidApiKey") || strings.Contains(de.Code, "NotFoundApiKey") {
			msg = "API Key 无效。请确认 DASHSCOPE_API_KEY 是百炼控制台创建的 sk- 开头模型调用 Key。"
		}
		return inferEnvelope{OK: false, Error: msg, Code: de.Code}
	}
	return inferEnvelope{OK: false, Error: err.Error()}
}

// handleInferChat 代理 OpenAI 兼容对话接口（文本 / 全模态 / 语音识别共用）。
func (s *server) handleInferChat(w http.ResponseWriter, r *http.Request) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(w, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY（sk- 开头的模型调用 Key）。请在 " + s.envFile +
				" 写入一行 DASHSCOPE_API_KEY=sk-…，保存后重试，无需重启。"})
		return
	}
	var req map[string]any
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	start := time.Now()
	raw, err := dashscope.New().ChatCompletion(r.Context(), key, req)
	if err != nil {
		writeJSON(w, http.StatusOK, inferErr(err))
		return
	}
	writeJSON(w, http.StatusOK, inferEnvelope{OK: true, Data: raw,
		LatencyMs: time.Since(start).Milliseconds()})
}

// handleInferEmbeddings 代理向量化接口。
func (s *server) handleInferEmbeddings(w http.ResponseWriter, r *http.Request) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(w, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY。请在 " + s.envFile + " 写入 DASHSCOPE_API_KEY=sk-…"})
		return
	}
	var req struct {
		Model  string   `json:"model"`
		Inputs []string `json:"inputs"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.Model == "" || len(req.Inputs) == 0 {
		writeJSON(w, http.StatusOK, inferEnvelope{OK: false, Error: "缺少 model 或 inputs"})
		return
	}
	start := time.Now()
	raw, err := dashscope.New().Embeddings(r.Context(), key, req.Model, req.Inputs)
	if err != nil {
		writeJSON(w, http.StatusOK, inferErr(err))
		return
	}
	writeJSON(w, http.StatusOK, inferEnvelope{OK: true, Data: raw,
		LatencyMs: time.Since(start).Milliseconds()})
}

// handleInferImageGen 创建并轮询图像/视频生成任务，完成后返回产物 URL。
func (s *server) handleInferImageGen(w http.ResponseWriter, r *http.Request) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(w, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY。请在 " + s.envFile + " 写入 DASHSCOPE_API_KEY=sk-…"})
		return
	}
	var req struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		Size   string `json:"size"`
		Kind   string `json:"kind"` // image | video
	}
	if err := decodeBody(w, r, &req); err != nil {
		return
	}
	if req.Model == "" || strings.TrimSpace(req.Prompt) == "" {
		writeJSON(w, http.StatusOK, inferEnvelope{OK: false, Error: "缺少 model 或 prompt"})
		return
	}

	client := dashscope.New()
	if req.Kind == "video" {
		// 视频生成：异步任务，提交后轮询（最长 5 分钟）
		taskID, err := client.CreateVideoTask(r.Context(), key, req.Model, req.Prompt)
		if err != nil {
			writeJSON(w, http.StatusOK, inferErr(err))
			return
		}
		maxWait := 300 * time.Second
		start := time.Now()
		deadline := time.Now().Add(maxWait)
		for time.Now().Before(deadline) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(3 * time.Second):
			}
			res, err := client.PollTask(r.Context(), key, taskID)
			if err != nil {
				writeJSON(w, http.StatusOK, inferErr(err))
				return
			}
			switch res.Status {
			case "SUCCEEDED":
				writeJSON(w, http.StatusOK, inferEnvelope{OK: true,
					Data:      mustJSON(map[string]any{"urls": res.URLs, "taskId": taskID}),
					LatencyMs: time.Since(start).Milliseconds()})
				return
			case "FAILED", "CANCELED", "UNKNOWN":
				msg := res.Message
				if msg == "" {
					msg = "任务失败：" + res.Status
				}
				writeJSON(w, http.StatusOK, inferEnvelope{OK: false, Error: msg, Code: res.Status})
				return
			}
		}
		writeJSON(w, http.StatusOK, inferEnvelope{OK: false,
			Error: fmt.Sprintf("生成超时（>%v），任务 %s 可能仍在执行，可稍后在控制台查看", maxWait, taskID)})
		return
	}

	// 图像生成（qwen-image 2.x）：同步接口，直接返回图片 URL
	start := time.Now()
	imageURL, err := client.GenerateImage(r.Context(), key, req.Model, req.Prompt, req.Size)
	if err != nil {
		writeJSON(w, http.StatusOK, inferErr(err))
		return
	}
	writeJSON(w, http.StatusOK, inferEnvelope{OK: true,
		Data:      mustJSON(map[string]any{"urls": []string{imageURL}}),
		LatencyMs: time.Since(start).Milliseconds()})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusOK, inferEnvelope{OK: false, Error: "请求体解析失败：" + err.Error()})
		return err
	}
	return nil
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return data
}
