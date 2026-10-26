package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HulnotHutu/dashscope-call/internal/bailian"
)

func sampleSnapshot() *bailian.Snapshot {
	return &bailian.Snapshot{
		FetchedAt: time.Date(2026, 10, 27, 9, 0, 0, 0, time.Local).Format(time.RFC3339),
		Source:    "src",
		Summary:   bailian.Summary{TotalModels: 365, WithQuota: 1},
		Models: []bailian.ModelQuota{{
			Model: "qwen3.8-max", Type: "Text",
			Remaining: 999668, Total: 1_000_000, RemainingPercent: 100, Expires: "2026-11-01",
		}},
	}
}

func TestSaveAndLatestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	path, err := s.Save(sampleSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "free-quota-"+time.Now().Format("2006-01-02")+".json" {
		t.Errorf("归档文件名不对：%s", path)
	}

	gotPath, snap, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	// 与旧版一致：Latest 返回的是 latest.json 软链路径。
	if gotPath != filepath.Join(dir, "latest.json") {
		t.Errorf("Latest = %s, want %s", gotPath, filepath.Join(dir, "latest.json"))
	}
	if snap.Summary.WithQuota != 1 || snap.Models[0].Model != "qwen3.8-max" {
		t.Errorf("roundtrip 数据不对：%+v", snap)
	}
	if _, err := os.Stat(filepath.Join(dir, "run.log")); err != nil {
		t.Errorf("run.log 未生成：%v", err)
	}
}

func TestLatestPrefersSymlink(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if _, err := s.Save(sampleSnapshot()); err != nil {
		t.Fatal(err)
	}

	// 手工放一个名字更新的归档：latest.json 软链仍应胜出。
	newer := filepath.Join(dir, "free-quota-2999-01-01.json")
	if err := os.WriteFile(newer, []byte(`{"fetched_at":"x","summary":{"totalModels":9,"withQuota":9},"models":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, snap, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Summary.WithQuota != 1 {
		t.Errorf("应优先 latest.json（withQuota=1），得到 %d", snap.Summary.WithQuota)
	}
}

func TestLatestFallsBackToNewestDatedFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	os.WriteFile(filepath.Join(dir, "free-quota-2026-10-17.json"), []byte(`{"summary":{"totalModels":1,"withQuota":1}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "free-quota-2026-10-20.json"), []byte(`{"summary":{"totalModels":2,"withQuota":2}}`), 0o644)

	_, snap, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Summary.TotalModels != 2 {
		t.Errorf("应取文件名最新的归档，got %d", snap.Summary.TotalModels)
	}
}

func TestLatestEmptyDir(t *testing.T) {
	_, _, err := New(t.TempDir()).Latest()
	if err == nil {
		t.Fatal("空目录应报错")
	}
}
