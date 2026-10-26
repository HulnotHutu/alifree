package bailian

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// 控制台网关常量（对齐官方 CLI）。
const (
	gatewayURL    = "https://bailian-cs.console.aliyun.com/cli/api.json"
	gatewayAction = "BroadScopeAspnGateway"
	gatewayProd   = "sfm_bailian"
	gatewayRegion = "cn-beijing"

	// ModelListAPI 匿名可用；FreeTierAPI 需要 Bearer CLI token。
	ModelListAPI = "zeldaHttp.dashscopeModel./zelda/api/v1/modelCenter/listFoundationModels"
	FreeTierAPI  = "zeldaEasy.bailian-commerce.freeTrial.queryFreeTierQuota"
)

// gatewayEnvelope 剥掉 data.DataV2.data.data 双层信封（对齐 CLI 的 unwrapResponse）。
type gatewayEnvelope struct {
	Data struct {
		Success   bool            `json:"success"`
		ErrorCode string          `json:"errorCode"`
		DataV2    json.RawMessage `json:"DataV2"`
		Data      json.RawMessage `json:"data"`
	} `json:"data"`
}

// unwrap 返回信封内的业务数据：DataV2.data.data → DataV2.data → DataV2 → data。
func (e *gatewayEnvelope) unwrap() (json.RawMessage, error) {
	d := e.Data
	if !d.Success && d.ErrorCode != "" {
		return nil, fmt.Errorf("网关错误：%s", d.ErrorCode)
	}
	if len(d.DataV2) > 0 {
		var level1 struct {
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(d.DataV2, &level1)
		if len(level1.Data) == 0 {
			return d.DataV2, nil
		}
		var level2 struct {
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(level1.Data, &level2)
		if len(level2.Data) > 0 {
			return level2.Data, nil
		}
		return level1.Data, nil
	}
	if len(d.Data) > 0 {
		return d.Data, nil
	}
	return nil, fmt.Errorf("网关响应缺少业务数据")
}

// gatewayCall 调用控制台网关的指定 API；token 为空表示匿名调用。
func (c *Client) gatewayCall(ctx context.Context, token, api string, payload any) (json.RawMessage, error) {
	data := map[string]any{
		"cornerstoneParam": map[string]any{
			"protocol":       "V2",
			"console":        "ONE_CONSOLE",
			"productCode":    "p_efm",
			"switchUserType": 3,
			"consoleSite":    "BAILIAN_ALIYUN",
		},
	}
	if payload != nil {
		// gatewayCall 的两种用法：payload 即 Data 的字段集。
		for k, v := range payload.(map[string]any) {
			data[k] = v
		}
	}
	params, err := json.Marshal(map[string]any{"Api": api, "V": "1.0", "Data": data})
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("params", string(params))
	form.Set("region", gatewayRegion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %.500s", resp.StatusCode, body)
	}

	var envelope gatewayEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("网关返回非 JSON：%.300s", body)
	}
	return envelope.unwrap()
}
