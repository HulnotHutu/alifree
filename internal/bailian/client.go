package bailian

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/HulnotHutu/dashscope-call/internal/format"
)

const (
	pageSize    = 50
	httpTimeout = 30 * time.Second
)

// Client 抓取百炼免费额度：AK/SK → CLI token → 控制台网关。
type Client struct {
	AK, SK, STS string
	HTTP        *http.Client

	// Now / NewNonce 可注入，测试时用固定值复现签名。
	Now      func() time.Time
	NewNonce func() string
}

// NewClient 创建客户端；ak/sk 必填，sts 为 STS 临时凭证（可空）。
func NewClient(ak, sk, sts string) *Client {
	return &Client{
		AK: ak, SK: sk, STS: sts,
		HTTP: &http.Client{Timeout: httpTimeout},
		Now:  time.Now,
		NewNonce: func() string {
			var b [16]byte
			_, _ = rand.Read(b[:])
			b[6] = (b[6] & 0x0f) | 0x40 // version 4
			b[8] = (b[8] & 0x3f) | 0x80 // variant 10
			return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
		},
	}
}

// GenerateCLIToken 用 AK/SK 换取 CLI access token。
func (c *Client) GenerateCLIToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://"+openAPIHost+openAPIPath, nil)
	if err != nil {
		return "", err
	}
	for k, v := range SignRequest(c.AK, c.SK, c.STS, c.Now(), c.NewNonce()) {
		req.Header[k] = v
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var out struct {
		Success        bool   `json:"Success"`
		Code           string `json:"Code"`
		Message        string `json:"Message"`
		CLIAccessToken string `json:"cliAccessToken"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("OpenAPI 返回非 JSON：HTTP %d, %.300s", resp.StatusCode, body)
	}
	if !out.Success && out.Code != "" {
		return "", fmt.Errorf("GenerateCLIAccessToken 失败：%s - %s", out.Code, out.Message)
	}
	if out.CLIAccessToken == "" {
		return "", fmt.Errorf("响应中没有 cliAccessToken：%.300s", body)
	}
	return out.CLIAccessToken, nil
}

// modelListPage 是 listFoundationModels 分页响应的业务数据。
type modelListPage struct {
	Total int `json:"total"`
	List  []struct {
		Model string `json:"model"`
		Items []struct {
			Model        string   `json:"model"`
			Capabilities []string `json:"capabilities"`
		} `json:"items"`
		Capabilities []string `json:"capabilities"`
	} `json:"list"`
}

// ListModelTypes 返回全部模型 → 展示类型（匿名接口，分页拉全）。
func (c *Client) ListModelTypes(ctx context.Context) (map[string]string, error) {
	types := map[string]string{}
	for page := 1; ; page++ {
		raw, err := c.gatewayCall(ctx, "", ModelListAPI, map[string]any{
			"input": map[string]any{
				"pageNo": page, "pageSize": pageSize, "name": "",
				"providers": []any{}, "inferenceProviders": []any{}, "features": []any{},
				"group": true, "capabilities": []any{}, "contextWindows": []any{},
			},
		})
		if err != nil {
			return nil, err
		}
		var data modelListPage
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
		found := 0
		for _, group := range data.List {
			add := func(name string, caps []string) {
				if name != "" {
					types[name] = resolveModelType(caps)
					found++
				}
			}
			if len(group.Items) > 0 {
				for _, item := range group.Items {
					add(item.Model, item.Capabilities)
				}
			} else {
				add(group.Model, group.Capabilities)
			}
		}
		if found == 0 || page*pageSize >= data.Total {
			return types, nil
		}
	}
}

// QueryFreeQuotas 查询指定模型的免费额度原始条目。
func (c *Client) QueryFreeQuotas(ctx context.Context, token string, models []string) ([]rawQuota, error) {
	raw, err := c.gatewayCall(ctx, token, FreeTierAPI, map[string]any{
		"queryFreeTierQuotaRequest": map[string]any{"models": models},
	})
	if err != nil {
		return nil, err
	}
	var data struct {
		FreeTierQuotas []rawQuota `json:"freeTierQuotas"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data.FreeTierQuotas, nil
}

// Snapshot 抓取并组装额度快照：有额度 = VALID 且总额度 > 0，
// 按剩余比例升序（最紧张在前，与官方 CLI 一致）。
func (c *Client) Snapshot(ctx context.Context) (*Snapshot, error) {
	token, err := c.GenerateCLIToken(ctx)
	if err != nil {
		return nil, err
	}
	types, err := c.ListModelTypes(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	quotas, err := c.QueryFreeQuotas(ctx, token, names)
	if err != nil {
		return nil, err
	}
	return BuildSnapshot(c.Now(), sourceURL(), types, quotas), nil
}

func sourceURL() string {
	return gatewayURL + "?action=" + gatewayAction + "&product=" + gatewayProd + "&api=" + FreeTierAPI
}

// BuildSnapshot 过滤、排序并组装快照（纯函数，便于测试）。
func BuildSnapshot(now time.Time, source string, types map[string]string, quotas []rawQuota) *Snapshot {
	models := make([]ModelQuota, 0, len(quotas))
	for _, q := range quotas {
		if q.QuotaStatus != "VALID" || q.QuotaInitTotal <= 0 {
			continue
		}
		modelType, ok := types[q.Model]
		if !ok {
			modelType = "-"
		}
		models = append(models, ModelQuota{
			Model:            q.Model,
			Type:             modelType,
			Remaining:        q.QuotaTotal,
			Total:            q.QuotaInitTotal,
			RemainingPercent: round1(q.QuotaTotal / q.QuotaInitTotal * 100),
			Expires:          format.Date(q.QuotaValidityPeriod),
		})
	}
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].Remaining/models[i].Total < models[j].Remaining/models[j].Total
	})
	return &Snapshot{
		FetchedAt: now.Format(time.RFC3339),
		Source:    source,
		Summary:   Summary{TotalModels: len(types), WithQuota: len(models)},
		Models:    models,
	}
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
