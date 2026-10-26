package bailian

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256Hex(key, s string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(s))
	return hex.EncodeToString(mac.Sum(nil))
}
