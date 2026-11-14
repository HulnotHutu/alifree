package bailian

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 用完即停（FreeTierOnly）：按模型开启后，免费额度用尽时推理网关直接
// 返回 403（AllocationQuota.FreeTierOnly.），不转按量计费。
// 写入接口为批量异步任务：提交 models → taskId → 轮询取结果。

const (
	FTStatusAPI   = "zeldaEasy.bailian-commerce.freeTrial.queryFreeTierInterceptStatus"
	FTActivateAPI = "zeldaEasy.bailian-commerce.freeTrial.batchActivateFreeTierOnly"
	FTDeactivate  = "zeldaEasy.bailian-commerce.freeTrial.batchDeactivateFreeTierOnly"
)

// FreeTierModel 是一个模型的用完即停状态。
type FreeTierModel struct {
	Model string `json:"model"`
	// Enabled 为 true 表示已开启用完即停（额度用尽即 403）。
	Enabled bool   `json:"enabled"`
	Raw     string `json:"raw,omitempty"`
}

// submitKeys 是两个批量接口各自的 Data 键名（现场捕获确认）。
func submitKey(api string) string {
	if api == FTActivateAPI {
		return "BatchActivateFreeTierOnlyRequest"
	}
	return "BatchDeactivateFreeTierOnlyRequest"
}

// SetFreeTierOnly 批量开启/关闭用完即停，返回任务执行后的原始响应。
// enable=true 开启（额度用尽即停），false 关闭（用尽后转按量计费）。
func (c *Client) SetFreeTierOnly(ctx context.Context, models []string, enable bool) (json.RawMessage, error) {
	if len(models) == 0 {
		return nil, fmt.Errorf("未指定模型")
	}
	token, err := c.GenerateCLIToken(ctx)
	if err != nil {
		return nil, err
	}
	api := FTActivateAPI
	if !enable {
		api = FTDeactivate
	}
	key := submitKey(api)

	raw, err := c.gatewayCall(ctx, token, api, map[string]any{
		key: map[string]any{"models": models},
	})
	if err != nil {
		return nil, err
	}

	// 提交响应里拿 taskId（异步任务）；若无 taskId 说明同步完成，直接返回。
	taskID := extractString(raw, "taskId")
	if taskID == "" {
		return raw, nil
	}

	// 轮询任务直到完成（页面前端同样是这个模式）。
	var last json.RawMessage
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
		poll, err := c.gatewayCall(ctx, token, api, map[string]any{
			key: map[string]any{"taskId": taskID},
		})
		if err != nil {
			return nil, err
		}
		last = poll
		if done, payload := freeTierTaskDone(poll); done {
			return payload, nil
		}
	}
	return last, fmt.Errorf("批量任务超时未完成（taskId=%s），最后响应：%s", taskID, last)
}

// QueryFreeTierStatus 查询模型的用完即停状态。
// 请求体形状官方未文档化，先按实验结果适配；未传 models 时查全部。
func (c *Client) QueryFreeTierStatus(ctx context.Context, models []string) (json.RawMessage, error) {
	token, err := c.GenerateCLIToken(ctx)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if len(models) > 0 {
		payload = map[string]any{"models": models}
	}
	return c.gatewayCall(ctx, token, FTStatusAPI, payload)
}

// extractString 在多层信封里找首个指定键的字符串值。
func extractString(raw json.RawMessage, key string) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return findString(m, key)
}

func findString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	for _, v := range m {
		if sub, ok := v.(map[string]any); ok {
			if s := findString(sub, key); s != "" {
				return s
			}
		}
	}
	return ""
}

// taskDoneStatus 是任务终态的取值（大小写不敏感）。
var taskDoneStatus = map[string]bool{
	"SUCCESS": true, "SUCCEEDED": true, "SUCCESSFUL": true,
	"FINISH": true, "FINISHED": true, "DONE": true, "COMPLETED": true,
}

// freeTierTaskDone 判断轮询响应是否已到终态。
func freeTierTaskDone(raw json.RawMessage) (bool, json.RawMessage) {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return false, raw
	}
	// success 布尔字段优先（部分响应直接给 true/false）
	if v, ok := findKey(m, "success"); ok {
		if b, ok := v.(bool); ok && b {
			return true, raw
		}
	}
	if v, ok := findKey(m, "status"); ok {
		if s, ok := v.(string); ok && taskDoneStatus[strings.ToUpper(s)] {
			return true, raw
		}
	}
	// 实测终态形状：{successModels: [...], failureModels: [...], taskId: ...}
	if _, ok := findKey(m, "successModels"); ok {
		return true, raw
	}
	if v, ok := findKey(m, "results"); ok {
		if arr, ok := v.([]any); ok && len(arr) > 0 {
			return true, raw
		}
	}
	return false, raw
}

func findKey(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	for _, v := range m {
		if sub, ok := v.(map[string]any); ok {
			if found, ok := findKey(sub, key); ok {
				return found, true
			}
		}
	}
	return nil, false
}
