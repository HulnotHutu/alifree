package bailian

// ModelQuota 是单个模型的免费额度条目。
// JSON 字段名与旧版 Python 工具的归档格式保持一致。
type ModelQuota struct {
	Model            string  `json:"model"`
	Type             string  `json:"type"`
	Remaining        float64 `json:"remaining"`
	Total            float64 `json:"total"`
	RemainingPercent float64 `json:"remainingPercent"`
	Expires          string  `json:"expires"`
}

// Summary 是快照的汇总信息。
type Summary struct {
	TotalModels int `json:"totalModels"`
	WithQuota   int `json:"withQuota"`
}

// Snapshot 是一次额度抓取的完整结果。
type Snapshot struct {
	FetchedAt string       `json:"fetched_at"`
	Source    string       `json:"source"`
	Summary   Summary      `json:"summary"`
	Models    []ModelQuota `json:"models"`
}

// rawQuota 是 queryFreeTierQuota 返回的原始条目（服务端字段，见 status.md）。
type rawQuota struct {
	Model               string  `json:"model"`
	QuotaTotal          float64 `json:"quotaTotal"`
	QuotaInitTotal      float64 `json:"quotaInitTotal"`
	QuotaValidityPeriod int64   `json:"quotaValidityPeriod"`
	QuotaStatus         string  `json:"quotaStatus"`
}

// capabilityToType 把模型能力标签映射为展示类型（对齐官方 CLI usage/shared.ts）。
var capabilityToType = map[string]string{
	"Reasoning": "Text", "TG": "Text", "VU": "Text",
	"IG": "Vision", "VG": "Vision",
	"Realtime-Omni": "Multimodal", "Multimodal-Omni": "Multimodal",
	"ASR": "Audio", "TTS": "Audio", "Voice-Replication": "Audio",
	"Realtime-Text-to-Speech": "Audio", "Realtime-Voice-Replication": "Audio",
	"Realtime-ASR": "Audio", "Realtime-Audio-Translate": "Audio",
	"ME": "Embedding", "TR": "Embedding",
}

// resolveModelType 从能力标签列表推断展示类型，未知返回 "-"。
func resolveModelType(capabilities []string) string {
	for _, c := range capabilities {
		if t, ok := capabilityToType[c]; ok {
			return t
		}
	}
	return "-"
}
