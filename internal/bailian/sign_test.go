package bailian

import (
	"testing"
	"time"
)

// 黄金向量由旧版 Python 实现（free_quota_lite.py 的 sign_acs_request）对同一组
// 固定输入计算得出，用于交叉验证 Go 版签名算法的兼容性。
func TestSignRequestGoldenVector(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	nonce := "00000000-0000-4000-8000-000000000001"

	h := SignRequest("LTAI5tExample", "testSecretKey", "", now, nonce)

	const wantSig = "a9084bc66e607522c5a5c37632510b0233af570e893fe80e7507e353b7a84aba"
	auth := h.Get("Authorization")
	if auth == "" {
		t.Fatal("缺少 Authorization 头")
	}
	if got := paramValue(auth, "Signature="); got != wantSig {
		t.Errorf("Signature = %q, want %q", got, wantSig)
	}
	if got := paramValue(auth, "SignedHeaders="); got != "content-type;host;x-acs-action;x-acs-content-sha256;x-acs-date;x-acs-signature-nonce;x-acs-version" {
		t.Errorf("SignedHeaders = %q", got)
	}
	if got := paramValue(auth, "Credential="); got != "LTAI5tExample" {
		t.Errorf("Credential = %q", got)
	}
	if h.Get("X-Acs-Date") != "2026-10-27T00:00:00Z" {
		t.Errorf("x-acs-date = %q", h.Get("X-Acs-Date"))
	}
	if h.Get("X-Acs-Content-Sha256") !=
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("空请求体哈希不正确: %q", h.Get("X-Acs-Content-Sha256"))
	}
}

func TestSignRequestWithSTSToken(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	h := SignRequest("ak", "sk", "STS-TOKEN-123", now, "n")
	if h.Get("X-Acs-Security-Token") != "STS-TOKEN-123" {
		t.Fatal("缺少 x-acs-security-token 头")
	}
	// STS 头属于 x-acs-*，必须参与签名。
	if got := paramValue(h.Get("Authorization"), "SignedHeaders="); !contains(got, "x-acs-security-token") {
		t.Errorf("SignedHeaders 未包含 x-acs-security-token: %q", got)
	}
}

func paramValue(s, key string) string {
	start := indexOf(s, key)
	if start < 0 {
		return ""
	}
	start += len(key)
	end := start
	for end < len(s) && s[end] != ',' {
		end++
	}
	return s[start:end]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func contains(s, sub string) bool { return indexOf(s, sub) >= 0 }
