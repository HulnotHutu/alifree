// Package bailian 实现阿里云百炼（Model Studio）免费额度的抓取客户端。
//
// 链路从官方 bailian-cli（https://github.com/modelstudioai/cli）源码裁减：
//  1. AK/SK 做 ACS3-HMAC-SHA256 签名，调 OpenAPI GenerateCLIAccessToken 换 CLI token
//  2. 匿名调控制台网关 listFoundationModels 拿模型清单
//  3. 带 Bearer token 调 queryFreeTierQuota 拿额度，过滤出有额度的（VALID）
package bailian

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// OpenAPI 端点常量（对齐 CLI：domestic + cn-beijing）。
const (
	openAPIHost    = "modelstudio.cn-beijing.aliyuncs.com"
	openAPIPath    = "/modelstudio/cli/generateAccessToken"
	openAPIAction  = "GenerateCLIAccessToken"
	openAPIVersion = "2026-02-10"
)

// hashedEmptyBody 是空请求体的 SHA-256（GenerateCLIAccessToken 无请求体）。
var hashedEmptyBody = sha256.Sum256(nil)

// SignRequest 为 GenerateCLIAccessToken 构造带 ACS3-HMAC-SHA256 签名的请求头。
//
// now 与 nonce 由调用方注入：生产路径取当前时间与随机 UUID，测试路径传固定值以复现签名。
func SignRequest(ak, sk, stsToken string, now time.Time, nonce string) http.Header {
	dateISO := now.UTC().Format("2006-01-02T15:04:05Z")
	hashedBody := hex.EncodeToString(hashedEmptyBody[:])

	headers := map[string]string{
		"host":                  openAPIHost,
		"x-acs-action":          openAPIAction,
		"x-acs-version":         openAPIVersion,
		"x-acs-date":            dateISO,
		"x-acs-signature-nonce": nonce,
		"x-acs-content-sha256":  hashedBody,
		"content-type":          "application/json",
	}
	if stsToken != "" {
		headers["x-acs-security-token"] = stsToken
	}

	// 签名只覆盖 host、content-type 与 x-acs-* 头（ACS3 规范）。
	signed := make([]string, 0, len(headers))
	for k := range headers {
		if k == "host" || k == "content-type" || strings.HasPrefix(k, "x-acs-") {
			signed = append(signed, k)
		}
	}
	sort.Strings(signed)

	var canonicalHeaders strings.Builder
	for _, k := range signed {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(headers[k])
		canonicalHeaders.WriteByte('\n')
	}
	canonicalRequest := strings.Join([]string{
		"POST", openAPIPath, "", canonicalHeaders.String(),
		strings.Join(signed, ";"), hashedBody,
	}, "\n")

	stringToSign := "ACS3-HMAC-SHA256\n" + hashHex(canonicalRequest)
	signature := hmacSHA256Hex(sk, stringToSign)

	out := http.Header{}
	for k, v := range headers {
		out.Set(k, v)
	}
	out.Set("Authorization", fmt.Sprintf(
		"ACS3-HMAC-SHA256 Credential=%s,SignedHeaders=%s,Signature=%s",
		ak, strings.Join(signed, ";"), signature,
	))
	// CLI 附加的渠道标识头（非签名头，对齐官方 client.ts + headers.ts）。
	out.Set("x-dashscope-source-config",
		`{"channel":"bailian-cli","tags":{"t1":"public","t2":"bl","t3":"2.3.1"}}`)
	out.Set("x-dashscope-openapisource", "BailianCLI")
	out.Set("X-Dashscope-Service", "bailian-cli")
	return out
}
