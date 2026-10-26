// Package format 提供额度数字与到期时间的展示格式化，规则与旧版 Python 工具一致。
package format

import (
	"strconv"
	"strings"
	"time"
)

// Number 把额度数值渲染成紧凑形式：整百万 → 2M，千位以上 → 999.7K，其余原样。
func Number(v float64) string {
	n := int64(v)
	switch {
	case n >= 1_000_000 && n%100_000 == 0:
		return strconv.FormatInt(n/1_000_000, 10) + "M"
	case n >= 1_000:
		s := strconv.FormatFloat(float64(n)/1_000, 'f', 1, 64)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		return s + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

// Date 把毫秒时间戳渲染成本地时区的 YYYY-MM-DD；0 或异常返回 "-"。
func Date(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	t := time.UnixMilli(ms)
	if t.Year() < 0 || t.Year() > 9999 {
		return "-"
	}
	return t.Format("2006-01-02")
}
