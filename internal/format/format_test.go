package format

import (
	"math"
	"testing"
	"time"
)

func TestNumber(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1K"},
		{1250, "1.2K"}, // 四舍六入五成双，与 Python %.1f 一致
		{999668, "999.7K"},
		{1_000_000, "1M"},
		{1_500_000, "1M"}, // 整除截断，与旧版行为一致
		{36_000, "36K"},
		{30_000, "30K"},
		{10, "10"},
	}
	for _, c := range cases {
		if got := Number(c.in); got != c.want {
			t.Errorf("Number(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDate(t *testing.T) {
	if got := Date(0); got != "-" {
		t.Errorf("Date(0) = %q, want -", got)
	}
	if got := Date(-5); got != "-" {
		t.Errorf("Date(-5) = %q, want -", got)
	}
	ms := time.Date(2026, 11, 25, 0, 0, 0, 0, time.Local).UnixMilli()
	if got := Date(ms); got != "2026-11-25" {
		t.Errorf("Date = %q, want 2026-11-25", got)
	}
	if got := Date(int64(math.MaxInt64)); got != "-" {
		t.Errorf("超大时间戳应回退 -: %q", got)
	}
}
