// Package store 管理额度快照的 SQLite 存储。
//
// 库文件默认在项目内 data/quota.db（--db / BAILIAN_DB 可改），WAL 模式；
// 首次打开时会把旧版 JSON 归档（~/.local/share/bailian-quota/free-quota-*.json）
// 一次性迁移进来，旧文件保留不动。
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite" // 注册 database/sql 驱动 "sqlite"

	"github.com/HulnotHutu/dashscope-call/internal/bailian"
)

const dbFileName = "quota.db"

// legacyDir 返回旧版 JSON 归档目录，仅用于一次性迁移；测试中可替换。
var legacyDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "bailian-quota")
}

// Store 是 SQLite 存储句柄。
type Store struct {
	db     *sql.DB
	DBPath string
}

// Open 打开（必要时创建）SQLite 库并确保 schema 就绪。
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("数据库路径为空")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // 本地单写者场景，规避并发写锁竞争
	s := &Store{db: db, DBPath: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化 schema 失败：%w", err)
	}
	if err := s.importLegacyJSON(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS snapshots (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			fetched_at   TEXT NOT NULL,
			source       TEXT NOT NULL DEFAULT '',
			total_models INTEGER NOT NULL DEFAULT 0,
			with_quota   INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS model_quotas (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			snapshot_id       INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
			model             TEXT NOT NULL,
			type              TEXT NOT NULL DEFAULT '-',
			remaining         REAL NOT NULL DEFAULT 0,
			total             REAL NOT NULL DEFAULT 0,
			remaining_percent REAL NOT NULL DEFAULT 0,
			expires           TEXT NOT NULL DEFAULT '-'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_model_quotas_snapshot ON model_quotas(snapshot_id)`,
		`CREATE INDEX IF NOT EXISTS idx_model_quotas_model    ON model_quotas(model)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_fetched_at  ON snapshots(fetched_at)`,
	} {
		if _, err := s.db.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// Save 写入一份快照，返回快照 id。快照按抓取时点入库，保留完整历史。
func (s *Store) Save(snap *bailian.Snapshot) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT INTO snapshots (fetched_at, source, total_models, with_quota) VALUES (?, ?, ?, ?)`,
		snap.FetchedAt, snap.Source, snap.Summary.TotalModels, snap.Summary.WithQuota,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, m := range snap.Models {
		if _, err := tx.Exec(
			`INSERT INTO model_quotas
				(snapshot_id, model, type, remaining, total, remaining_percent, expires)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, m.Model, m.Type, m.Remaining, m.Total, m.RemainingPercent, m.Expires,
		); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// Latest 返回最新一份快照（模型按剩余比例升序）。
func (s *Store) Latest() (int64, *bailian.Snapshot, error) {
	var (
		id        int64
		fetchedAt string
		source    string
		total     int
		withQuota int
	)
	err := s.db.QueryRow(
		`SELECT id, fetched_at, source, total_models, with_quota
		 FROM snapshots ORDER BY id DESC LIMIT 1`,
	).Scan(&id, &fetchedAt, &source, &total, &withQuota)
	if err == sql.ErrNoRows {
		return 0, nil, fmt.Errorf("数据库 %s 里还没有快照", s.DBPath)
	}
	if err != nil {
		return 0, nil, err
	}

	rows, err := s.db.Query(
		`SELECT model, type, remaining, total, remaining_percent, expires
		 FROM model_quotas WHERE snapshot_id = ?
		 ORDER BY remaining_percent ASC, model ASC`, id,
	)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	snap := &bailian.Snapshot{
		FetchedAt: fetchedAt,
		Source:    source,
		Summary:   bailian.Summary{TotalModels: total, WithQuota: withQuota},
		Models:    []bailian.ModelQuota{},
	}
	for rows.Next() {
		var m bailian.ModelQuota
		if err := rows.Scan(&m.Model, &m.Type, &m.Remaining, &m.Total,
			&m.RemainingPercent, &m.Expires); err != nil {
			return 0, nil, err
		}
		snap.Models = append(snap.Models, m)
	}
	return id, snap, rows.Err()
}

// RunInfo 是一次抓取的摘要记录。
type RunInfo struct {
	ID          int64  `json:"id"`
	FetchedAt   string `json:"fetchedAt"`
	TotalModels int    `json:"totalModels"`
	WithQuota   int    `json:"withQuota"`
}

// History 返回最近 limit 次抓取记录（新的在前）。
func (s *Store) History(limit int) ([]RunInfo, error) {
	rows, err := s.db.Query(
		`SELECT id, fetched_at, total_models, with_quota
		 FROM snapshots ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []RunInfo
	for rows.Next() {
		var r RunInfo
		if err := rows.Scan(&r.ID, &r.FetchedAt, &r.TotalModels, &r.WithQuota); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// importLegacyJSON 把旧版 free-quota-*.json 迁移进库：仅当表为空时执行，
// 同 fetched_at 跳过（幂等），旧文件保留不动。
func (s *Store) importLegacyJSON() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM snapshots`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	dir := legacyDir()
	if dir == "" {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "free-quota-*.json"))
	if len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)

	imported := 0
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var snap bailian.Snapshot
		if json.Unmarshal(data, &snap) != nil || snap.FetchedAt == "" {
			continue
		}
		var exists int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM snapshots WHERE fetched_at = ?`, snap.FetchedAt,
		).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err := s.Save(&snap); err != nil {
			return fmt.Errorf("迁移 %s 失败：%w", path, err)
		}
		imported++
	}
	if imported > 0 {
		fmt.Fprintf(os.Stderr, "已从旧版 JSON 归档迁移 %d 份快照（来源 %s，旧文件保留）\n", imported, dir)
	}
	return nil
}
