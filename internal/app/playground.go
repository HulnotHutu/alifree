package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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

func (s *server) handleKeyStatus(c *gin.Context) {
	writeJSON(c, http.StatusOK, map[string]any{
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
func (s *server) handleInferChat(c *gin.Context) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(c, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY（sk- 开头的模型调用 Key）。请在 " + s.envFile +
				" 写入一行 DASHSCOPE_API_KEY=sk-…，保存后重试，无需重启。"})
		return
	}
	var req map[string]any
	if err := bindJSON(c, &req); err != nil {
		return
	}
	start := time.Now()
	raw, err := dashscope.New().ChatCompletion(c.Request.Context(), key, req)
	if err != nil {
		writeJSON(c, http.StatusOK, inferErr(err))
		return
	}
	writeJSON(c, http.StatusOK, inferEnvelope{OK: true, Data: raw,
		LatencyMs: time.Since(start).Milliseconds()})
}

// handleInferEmbeddings 代理向量化接口。
func (s *server) handleInferEmbeddings(c *gin.Context) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(c, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY。请在 " + s.envFile + " 写入 DASHSCOPE_API_KEY=sk-…"})
		return
	}
	var req struct {
		Model      string   `json:"model" binding:"required"`
		Inputs     []string `json:"inputs" binding:"required,min=1"`
		Dimensions int      `json:"dimensions"` // 可选：1024/768/512/256（flash 系列）
	}
	if err := bindJSON(c, &req); err != nil {
		return
	}
	start := time.Now()
	raw, err := dashscope.New().Embeddings(c.Request.Context(), key, req.Model, req.Inputs, req.Dimensions)
	if err != nil {
		writeJSON(c, http.StatusOK, inferErr(err))
		return
	}
	writeJSON(c, http.StatusOK, inferEnvelope{OK: true, Data: raw,
		LatencyMs: time.Since(start).Milliseconds()})
}

// handleInferImageGen 创建并轮询图像/视频生成任务，完成后返回产物 URL。
func (s *server) handleInferImageGen(c *gin.Context) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(c, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY。请在 " + s.envFile + " 写入 DASHSCOPE_API_KEY=sk-…"})
		return
	}
	var req struct {
		Model  string         `json:"model" binding:"required"`
		Prompt string         `json:"prompt" binding:"required"`
		Kind   string         `json:"kind"`   // image | video
		Params map[string]any `json:"params"` // 官方 parameters 对象（size/resolution/ratio/duration/audio/…）
	}
	if err := bindJSON(c, &req); err != nil {
		return
	}

	client := dashscope.New()
	start := time.Now()
	if req.Kind == "video" {
		// 视频生成：异步任务，提交立即返回 taskId；轮询状态经 SSE 推送
		// （GET /api/infer/tasks/{id}/events），前端展示进度条
		taskID, err := client.CreateVideoTask(c.Request.Context(), key, req.Model, req.Prompt, req.Params)
		if err != nil {
			writeJSON(c, http.StatusOK, inferErr(err))
			return
		}
		writeJSON(c, http.StatusOK, inferEnvelope{OK: true,
			Data:      mustJSON(map[string]any{"taskId": taskID}),
			LatencyMs: time.Since(start).Milliseconds()})
		return
	}

	// 图像生成（qwen-image 2.x）：同步接口，直接返回图片 URL
	imageURL, err := client.GenerateImage(c.Request.Context(), key, req.Model, req.Prompt, req.Params)
	if err != nil {
		writeJSON(c, http.StatusOK, inferErr(err))
		return
	}
	writeJSON(c, http.StatusOK, inferEnvelope{OK: true,
		Data:      mustJSON(map[string]any{"urls": []string{imageURL}}),
		LatencyMs: time.Since(start).Milliseconds()})
}

// handleTaskEvents 用 SSE 推送异步任务（视频生成等）的轮询状态：
// 2s 起指数退避到 15s，终态（SUCCEEDED/FAILED/CANCELED/UNKNOWN/TIMEOUT）
// 或客户端断开时结束流。查询仍走官方 GET /tasks/{id}（限流 20QPS，
// 单任务远低于上限）；事件让前端即时看到排队/生成中进度。
func (s *server) handleTaskEvents(c *gin.Context) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(c, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY。请在 " + s.envFile + " 写入 DASHSCOPE_API_KEY=sk-…"})
		return
	}
	taskID := c.Param("id")
	if taskID == "" {
		writeError(c, http.StatusBadRequest, "缺少任务 id")
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, "当前连接不支持流式响应")
		return
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	emit := func(v map[string]any) {
		c.SSEvent("", v)
		flusher.Flush()
	}

	client := dashscope.New()
	interval := 2 * time.Second
	const maxInterval = 15 * time.Second
	const maxWait = 10 * time.Minute
	deadline := time.Now().Add(maxWait)
	sseStart := time.Now()

	for {
		select {
		case <-c.Request.Context().Done():
			return // 客户端断开：停止轮询
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			emit(map[string]any{"status": "TIMEOUT",
				"message": fmt.Sprintf("生成超时（>%v），任务 %s 可能仍在执行，可稍后在控制台查看", maxWait, taskID)})
			return
		}
		res, err := client.PollTask(c.Request.Context(), key, taskID)
		if err != nil {
			env := inferErr(err)
			emit(map[string]any{"error": map[string]string{"error": env.Error, "code": env.Code}})
			return
		}
		emit(map[string]any{"status": res.Status, "urls": res.URLs, "message": res.Message,
			"elapsed": time.Since(sseStart).Milliseconds()})
		switch res.Status {
		case "SUCCEEDED", "FAILED", "CANCELED", "UNKNOWN":
			return
		}
		interval = min(interval*2, maxInterval) // 指数退避：PENDING/RUNNING 期间逐渐放缓查询
	}
}

// handleInferTTS 代理非实时语音合成接口（qwen-audio-3.x-tts / cosyvoice / qwen3-tts-flash）。
func (s *server) handleInferTTS(c *gin.Context) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(c, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY。请在 " + s.envFile + " 写入 DASHSCOPE_API_KEY=sk-…"})
		return
	}
	var req struct {
		Model string         `json:"model" binding:"required"`
		Input map[string]any `json:"input"` // text/voice/format/sample_rate/instruction
	}
	if err := bindJSON(c, &req); err != nil {
		return
	}
	if text, _ := req.Input["text"].(string); strings.TrimSpace(text) == "" {
		writeJSON(c, http.StatusOK, inferEnvelope{OK: false, Error: "缺少 input.text（待合成文本）"})
		return
	}
	start := time.Now()
	audioURL, err := dashscope.New().SynthesizeSpeech(c.Request.Context(), key, req.Model, req.Input)
	if err != nil {
		writeJSON(c, http.StatusOK, inferErr(err))
		return
	}
	writeJSON(c, http.StatusOK, inferEnvelope{OK: true,
		Data:      mustJSON(map[string]any{"urls": []string{audioURL}}),
		LatencyMs: time.Since(start).Milliseconds()})
}

// handleInferRerank 代理排序模型接口（qwen3-rerank / gte-rerank 等）。
func (s *server) handleInferRerank(c *gin.Context) {
	key := dashScopeKey(s.envFile)
	if key == "" {
		writeJSON(c, http.StatusOK, inferEnvelope{OK: false,
			Error: "未配置 DASHSCOPE_API_KEY。请在 " + s.envFile + " 写入 DASHSCOPE_API_KEY=sk-…"})
		return
	}
	var req struct {
		Model     string         `json:"model" binding:"required"`
		Query     string         `json:"query" binding:"required"`
		Documents []string       `json:"documents" binding:"required,min=1"`
		Params    map[string]any `json:"params"` // top_n/return_documents/instruct
	}
	if err := bindJSON(c, &req); err != nil {
		return
	}
	start := time.Now()
	raw, err := dashscope.New().Rerank(c.Request.Context(), key, req.Model, req.Query, req.Documents, req.Params)
	if err != nil {
		writeJSON(c, http.StatusOK, inferErr(err))
		return
	}
	writeJSON(c, http.StatusOK, inferEnvelope{OK: true, Data: raw,
		LatencyMs: time.Since(start).Milliseconds()})
}

// bindJSON 解析请求体（上限 16MB），失败时写回统一错误信封。
func bindJSON(c *gin.Context, v any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)
	if err := c.ShouldBindJSON(v); err != nil {
		writeJSON(c, http.StatusOK, inferEnvelope{OK: false, Error: "请求体解析失败：" + err.Error()})
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
