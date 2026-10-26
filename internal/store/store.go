// Package store 管理免费额度快照的本地归档目录。
//
// 落盘约定与旧版 run_free_quota.sh 完全一致：
//
//	free-quota-YYYY-MM-DD.json   每次抓取的完整 JSON
//	latest.json                  指向最新一次结果的软链
//	run.log                      运行记录
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/HulnotHutu/dashscope-call/internal/bailian"
)

// Store 是归档目录的读写句柄。
type Store struct {
	Dir string
}

func New(dir string) *Store { return &Store{Dir: dir} }

// Latest 返回最新归档：优先 latest.json 软链，否则文件名最大的 free-quota-*.json。
func (s *Store) Latest() (path string, snap *bailian.Snapshot, err error) {
	candidates := []string{}
	if latest := filepath.Join(s.Dir, "latest.json"); len(latest) > 0 {
		if _, statErr := os.Stat(latest); statErr == nil {
			candidates = append(candidates, latest)
		}
	}
	if len(candidates) == 0 {
		matches, _ := filepath.Glob(filepath.Join(s.Dir, "free-quota-*.json"))
		sort.Strings(matches)
		if len(matches) > 0 {
			candidates = append(candidates, matches[len(matches)-1])
		}
	}
	if len(candidates) == 0 {
		return "", nil, fmt.Errorf("数据目录 %s 里没有归档 JSON", s.Dir)
	}

	path = candidates[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	var snapshot bailian.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return path, nil, fmt.Errorf("解析 %s 失败：%w", path, err)
	}
	return path, &snapshot, nil
}

// Save 把快照写入归档目录：当日 JSON + latest.json 软链 + run.log。
func (s *Store) Save(snap *bailian.Snapshot) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')

	outPath := filepath.Join(s.Dir, "free-quota-"+time.Now().Format("2006-01-02")+".json")
	if err := writeFileAtomic(outPath, data); err != nil {
		return "", err
	}
	if err := symlinkAtomic(outPath, filepath.Join(s.Dir, "latest.json")); err != nil {
		return "", err
	}

	logLine := fmt.Sprintf("%s ok -> %s (quota)\n", time.Now().Format("2006-01-02 15:04:05"), outPath)
	if err := appendLine(filepath.Join(s.Dir, "run.log"), logLine); err != nil {
		return outPath, err
	}
	return outPath, nil
}

// writeFileAtomic 先写临时文件再原子替换，避免留下半个 JSON。
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// symlinkAtomic 原子替换软链（os.Symlink 不能覆盖已存在的链接）。
func symlinkAtomic(target, linkPath string) error {
	tmp := linkPath + fmt.Sprintf(".tmp.%d", os.Getpid())
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, linkPath)
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.TrimSpace(line) + "\n")
	return err
}
