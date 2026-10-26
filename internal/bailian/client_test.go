package bailian

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUnwrapDoubleEnvelope(t *testing.T) {
	raw := `{"data":{"success":true,"DataV2":{"data":{"data":{"freeTierQuotas":[{"model":"qwen"}]}}}}}`
	var env gatewayEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	inner, err := env.unwrap()
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		FreeTierQuotas []map[string]any `json:"freeTierQuotas"`
	}
	if err := json.Unmarshal(inner, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.FreeTierQuotas) != 1 || out.FreeTierQuotas[0]["model"] != "qwen" {
		t.Fatalf("unwrap 结果不对：%s", inner)
	}
}

func TestUnwrapDataV2WithoutInnerData(t *testing.T) {
	// 有的接口 DataV2.data 本身就是业务数据（没有再包一层 data）。
	raw := `{"data":{"DataV2":{"data":{"total":365,"list":[]}}}}`
	var env gatewayEnvelope
	_ = json.Unmarshal([]byte(raw), &env)
	inner, err := env.unwrap()
	if err != nil {
		t.Fatal(err)
	}
	if string(inner) != `{"total":365,"list":[]}` {
		t.Fatalf("unwrap 应回退到 DataV2.data：%s", inner)
	}
}

func TestUnwrapGatewayError(t *testing.T) {
	raw := `{"data":{"success":false,"errorCode":"BailianGateway.Login.NotLogined"}}`
	var env gatewayEnvelope
	_ = json.Unmarshal([]byte(raw), &env)
	if _, err := env.unwrap(); err == nil || !containsStr(err.Error(), "NotLogined") {
		t.Fatalf("应透出网关错误码，得到 %v", err)
	}
}

func TestBuildSnapshotFilterAndSort(t *testing.T) {
	types := map[string]string{
		"m-full":    "Text",
		"m-tight":   "Text",
		"m-exhaust": "Text",
		"m-invalid": "Audio",
		"m-unknown": "-",
	}
	quotas := []rawQuota{
		{Model: "m-full", QuotaStatus: "VALID", QuotaTotal: 1_000_000, QuotaInitTotal: 1_000_000},
		{Model: "m-tight", QuotaStatus: "VALID", QuotaTotal: 250_000, QuotaInitTotal: 1_000_000,
			QuotaValidityPeriod: mustMilli(t, 2026, 11, 25)},
		{Model: "m-exhaust", QuotaStatus: "VALID", QuotaTotal: 0, QuotaInitTotal: 1_000_000},
		{Model: "m-invalid", QuotaStatus: "UNKNOWN", QuotaTotal: 1, QuotaInitTotal: 1},
		{Model: "m-notyped", QuotaStatus: "VALID", QuotaTotal: 500, QuotaInitTotal: 1_000},
		{Model: "m-unknown", QuotaStatus: "VALID", QuotaTotal: 900_000, QuotaInitTotal: 1_000_000},
	}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	snap := BuildSnapshot(now, "src", types, quotas)

	if snap.Summary.TotalModels != len(types) || snap.Summary.WithQuota != 5 {
		t.Fatalf("summary = %+v", snap.Summary)
	}
	// 排序：剩余比例升序（最紧张在前）。m-exhaust 剩 0 但总额度有效，保留展示。
	want := []string{"m-exhaust", "m-tight", "m-notyped", "m-unknown", "m-full"}
	for i, name := range want {
		if snap.Models[i].Model != name {
			t.Fatalf("顺序不对：got %v，want %v", modelNames(snap), want)
		}
	}
	if got := snap.Models[1].RemainingPercent; got != 25 {
		t.Errorf("RemainingPercent = %v, want 25", got)
	}
	if snap.Models[1].Expires != "2026-11-25" {
		t.Errorf("Expires = %q", snap.Models[1].Expires)
	}
	if snap.FetchedAt != "2026-10-27T12:00:00Z" {
		t.Errorf("FetchedAt = %q", snap.FetchedAt)
	}
}

func mustMilli(t *testing.T, y int, m time.Month, d int) int64 {
	t.Helper()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local).UnixMilli()
}

func modelNames(s *Snapshot) []string {
	names := make([]string, 0, len(s.Models))
	for _, m := range s.Models {
		names = append(names, m.Model)
	}
	return names
}

func containsStr(s, sub string) bool { return contains(s, sub) }
