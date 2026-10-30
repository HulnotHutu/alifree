package aihelpme

import (
	"fmt"
	"sort"

	"github.com/HulnotHutu/dashscope-call/internal/store"
)

// ModelInfo describes one usable Text model from the quota database
// (main.model_quotas of the quota tool's sqlite file).
type ModelInfo struct {
	Model     string  `json:"model"`
	Remaining float64 `json:"remaining"`
	Total     float64 `json:"total"`
	Expires   string  `json:"expires"`
}

// AvailableTextModels reads the latest quota snapshot from the sqlite db at
// dbPath and returns the Text models with quota left, most remaining first.
// An empty dbPath, a missing file, or an empty snapshot comes back as an
// error for the caller to surface.
func AvailableTextModels(dbPath string) ([]ModelInfo, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("未配置 quota_db（config.yaml），无法读取可用模型")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	_, snap, err := st.Latest()
	if err != nil {
		return nil, fmt.Errorf("读取最新额度快照失败（%s）：%w", dbPath, err)
	}
	var out []ModelInfo
	for _, m := range snap.Models {
		// "Text" 是 bailian 类型归一化后的取值（types.go 的映射表）
		if m.Type == "Text" && m.Remaining > 0 {
			out = append(out, ModelInfo{
				Model:     m.Model,
				Remaining: m.Remaining,
				Total:     m.Total,
				Expires:   m.Expires,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Remaining > out[j].Remaining })
	return out, nil
}
