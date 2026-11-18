package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HulnotHutu/dashscope-call/internal/bailian"
)

func TestMain(m *testing.M) {
	// 默认把 legacyDir 指向空目录，避免测试读到用户真实的旧归档。
	empty, err := os.MkdirTemp("", "store-legacy-empty")
	if err != nil {
		panic(err)
	}
	old := legacyDir
	legacyDir = func() string { return empty }
	code := m.Run()
	legacyDir = old
	os.RemoveAll(empty)
	os.Exit(code)
}

func sampleSnapshot(fetchedAt string, withQuota int) *bailian.Snapshot {
	models := []bailian.ModelQuota{}
	if withQuota > 0 {
		models = append(models, bailian.ModelQuota{
			Model: "qwen3.8-max", Type: "Text",
			Remaining: 999668, Total: 1_000_000, RemainingPercent: 100, Expires: "2026-11-01",
		})
	}
	return &bailian.Snapshot{
		FetchedAt: fetchedAt,
		Source:    "src",
		Summary:   bailian.Summary{TotalModels: 365, WithQuota: withQuota},
		Models:    models,
	}
}

func TestOpenCreatesDBFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "quota.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("库文件未创建：%v", err)
	}
}

func TestSaveAndLatestRoundtrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, _, err := s.Latest(); err == nil {
		t.Fatal("空库时 Latest 应报错")
	}

	id1, err := s.Save(sampleSnapshot("2026-10-17T00:26:11+08:00", 1))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Save(sampleSnapshot("2026-10-27T09:00:00+08:00", 0))
	if err != nil {
		t.Fatal(err)
	}
	if id2 <= id1 {
		t.Fatalf("快照 id 应递增：%d then %d", id1, id2)
	}

	gotID, snap, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if gotID != id2 {
		t.Errorf("Latest = 快照 #%d, want #%d", gotID, id2)
	}
	if snap.FetchedAt != "2026-10-27T09:00:00+08:00" || snap.Summary.WithQuota != 0 {
		t.Errorf("最新快照内容不对：%+v", snap.Summary)
	}

	runs, err := s.History(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != id2 || runs[1].FetchedAt != "2026-10-17T00:26:11+08:00" {
		t.Errorf("History = %+v", runs)
	}
}

func TestLatestModelsSortedByRatio(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	snap := sampleSnapshot("2026-10-27T09:00:00+08:00", 2)
	snap.Models = []bailian.ModelQuota{
		{Model: "b-full", Type: "Text", Remaining: 1_000_000, Total: 1_000_000, RemainingPercent: 100},
		{Model: "a-tight", Type: "Text", Remaining: 250_000, Total: 1_000_000, RemainingPercent: 25},
	}
	if _, err := s.Save(snap); err != nil {
		t.Fatal(err)
	}
	_, got, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	// 读取顺序应按剩余比例升序，与写入顺序无关。
	if got.Models[0].Model != "a-tight" || got.Models[1].Model != "b-full" {
		t.Errorf("排序不对：[0]=%s [1]=%s", got.Models[0].Model, got.Models[1].Model)
	}
}

func TestLegacyJSONMigration(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "free-quota-2026-10-17.json")
	os.WriteFile(legacy, []byte(`{
		"fetched_at": "2026-10-17T00:26:11+08:00",
		"source": "legacy",
		"summary": {"totalModels": 365, "withQuota": 1},
		"models": [{"model":"qwen3.8-max","type":"Text","remaining":999668.0,
			"total":1000000.0,"remainingPercent":100.0,"expires":"2026-11-01"}]
	}`), 0o644)

	orig := legacyDir
	legacyDir = func() string { return dir }
	defer func() { legacyDir = orig }()

	s, err := Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, snap, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Summary.TotalModels != 365 || snap.Models[0].Model != "qwen3.8-max" {
		t.Errorf("迁移数据不对：%+v", snap)
	}
	// 旧文件保留不动。
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("旧 JSON 应保留：%v", err)
	}

	// 重开不重复导入：id 不变、仍只有一份。
	s.Close()
	s2, err := Open(s.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	runs, err := s2.History(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].FetchedAt != "2026-10-17T00:26:11+08:00" {
		t.Errorf("重复迁移了：%+v", runs)
	}
}

func TestMigrationSkippedWhenDBHasData(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "free-quota-2026-01-01.json"),
		[]byte(`{"fetched_at":"2026-01-01T00:00:00+08:00","summary":{"totalModels":1,"withQuota":1},"models":[]}`), 0o644)

	orig := legacyDir
	legacyDir = func() string { return dir }
	defer func() { legacyDir = orig }()

	s, err := Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Save(sampleSnapshot("2026-10-27T09:00:00+08:00", 0)); err != nil {
		t.Fatal(err)
	}

	// 重开：库已有数据（首次打开迁入 1 条 + 手工保存 1 条），旧 JSON 不应再迁入。
	s.Close()
	s2, err := Open(s.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	runs, _ := s2.History(10)
	if len(runs) != 2 {
		t.Errorf("重开不应重复迁移，got %d 条", len(runs))
	}
}

// 保证 Save 保留快照自身的抓取时间（迁移场景依赖这一点）。
func TestSavePreservesFetchedAt(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	want := time.Date(2026, 10, 17, 0, 26, 11, 0, time.FixedZone("", 8*3600)).Format(time.RFC3339)
	if _, err := s.Save(sampleSnapshot(want, 1)); err != nil {
		t.Fatal(err)
	}
	_, snap, _ := s.Latest()
	if snap.FetchedAt != want {
		t.Errorf("FetchedAt = %q, want %q", snap.FetchedAt, want)
	}
}

// TestSaveRingRetention 验证环形保留：窗口恒定 maxSnapshots，最旧被淘汰且明细级联清理。
func TestSaveRingRetention(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const extra = 5
	var lastID int64
	for i := 0; i < maxSnapshots+extra; i++ {
		id, err := s.Save(sampleSnapshot(fmt.Sprintf("2026-01-01T00:%02d:00+08:00", i), 1))
		if err != nil {
			t.Fatal(err)
		}
		lastID = id
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM snapshots`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != maxSnapshots {
		t.Fatalf("环形窗口应恒定 %d，实际 %d", maxSnapshots, n)
	}

	// 最新快照在窗口内，最旧 5 份已被淘汰
	if _, _, err := s.Latest(); err != nil {
		t.Fatal(err)
	}
	var minID int64
	if err := s.db.QueryRow(`SELECT MIN(id) FROM snapshots`).Scan(&minID); err != nil {
		t.Fatal(err)
	}
	// 共写入 maxSnapshots+extra 份，窗口留 30：最旧保留 id = extra+1（前 extra 份被淘汰）
	if minID != extra+1 {
		t.Errorf("最旧保留 id = %d, want %d", minID, extra+1)
	}

	// 明细靠 CASCADE 清理：只剩窗口内快照的行
	var qn int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM model_quotas WHERE snapshot_id NOT IN (SELECT id FROM snapshots)`,
	).Scan(&qn); err != nil {
		t.Fatal(err)
	}
	if qn != 0 {
		t.Errorf("存在孤儿明细 %d 行，CASCADE 未生效", qn)
	}

	// History 只返回窗口内的数据，最新在前
	runs, err := s.History(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != maxSnapshots || runs[0].ID != lastID {
		t.Errorf("History = %d 条，最新 id = %d, want %d 条 / %d", len(runs), runs[0].ID, maxSnapshots, lastID)
	}
}
