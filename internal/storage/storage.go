// Package storage SQLite 持久化层：唯一 SQL 所在地，WAL 模式、单连接、版本化迁移。
package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动

	"myschedule/internal/dates"
	"myschedule/internal/model"
)

// Store SQLite 存储句柄。
type Store struct {
	db   *sql.DB
	path string
}

// Open 打开（必要时创建）数据库并执行迁移。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	// modernc sqlite 需要显式开启并发读
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // 单连接，杜绝写冲突
	db.SetConnMaxIdleTime(0)
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库（幂等）。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path 数据库文件路径。
func (s *Store) Path() string { return s.path }

// DB 暴露底层连接（迁移/调试/测试用；业务代码不得绕过 Store 方法）。
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS tasks(
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  date TEXT NOT NULL,
  end_date TEXT NOT NULL DEFAULT '',
  time TEXT NOT NULL,
  duration INTEGER NOT NULL,
  reminder INTEGER NOT NULL,
  category TEXT NOT NULL,
  priority TEXT NOT NULL DEFAULT 'normal',
  notes TEXT NOT NULL DEFAULT '',
  done INTEGER NOT NULL DEFAULT 0,
  series_id TEXT NOT NULL DEFAULT '',
  series_index INTEGER NOT NULL DEFAULT -1,
  notified_at INTEGER NOT NULL DEFAULT 0,
  snooze_until INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_date ON tasks(date);
CREATE INDEX IF NOT EXISTS idx_tasks_series ON tasks(series_id);
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS history(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  at INTEGER NOT NULL,
  dismissed INTEGER NOT NULL DEFAULT 0
);
`)
	return err
}

const taskCols = `id,title,date,end_date,time,duration,reminder,category,priority,notes,
done,series_id,series_index,notified_at,snooze_until,created_at,updated_at`

func scanTask(row interface{ Scan(...any) error }) (*model.Task, error) {
	t := &model.Task{}
	var endD string
	err := row.Scan(&t.ID, &t.Title, &t.Date, &endD, &t.Time, &t.Duration, &t.Reminder,
		&t.Category, &t.Priority, &t.Notes, &t.Done, &t.SeriesID, &t.SeriesIndex,
		&t.NotifiedAt, &t.SnoozeUntil, &t.CreatedAt, &t.UpdatedAt)
	if endD != "" {
		t.EndDate = endD
	}
	return t, err
}

// Tasks 读全部计划（上限 1 万条，一次性载入内存）。
func (s *Store) Tasks() ([]*model.Task, error) {
	rows, err := s.db.Query(`SELECT ` + taskCols + ` FROM tasks ORDER BY date,time,created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Task 按 id 取单条。
func (s *Store) Task(id string) (*model.Task, error) {
	row := s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id=?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, os.ErrNotExist
	}
	return t, err
}

// SaveTask upsert 一条计划。
func (s *Store) SaveTask(t *model.Task) error {
	_, err := s.db.Exec(`INSERT INTO tasks(`+taskCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
 title=excluded.title,date=excluded.date,end_date=excluded.end_date,time=excluded.time,
 duration=excluded.duration,reminder=excluded.reminder,category=excluded.category,
 priority=excluded.priority,notes=excluded.notes,done=excluded.done,
 series_id=excluded.series_id,series_index=excluded.series_index,
 notified_at=excluded.notified_at,snooze_until=excluded.snooze_until,
 updated_at=excluded.updated_at`,
		t.ID, t.Title, t.Date, t.EndDate, t.Time, t.Duration, t.Reminder, t.Category,
		t.Priority, t.Notes, t.Done, t.SeriesID, t.SeriesIndex, t.NotifiedAt,
		t.SnoozeUntil, t.CreatedAt, t.UpdatedAt)
	return err
}

// SaveTasks 事务批量写。
func (s *Store) SaveTasks(ts []*model.Task) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO tasks(` + taskCols + `) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
 title=excluded.title,date=excluded.date,end_date=excluded.end_date,time=excluded.time,
 duration=excluded.duration,reminder=excluded.reminder,category=excluded.category,
 priority=excluded.priority,notes=excluded.notes,done=excluded.done,
 series_id=excluded.series_id,series_index=excluded.series_index,
 notified_at=excluded.notified_at,snooze_until=excluded.snooze_until,
 updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range ts {
		if _, err := stmt.Exec(t.ID, t.Title, t.Date, t.EndDate, t.Time, t.Duration, t.Reminder,
			t.Category, t.Priority, t.Notes, t.Done, t.SeriesID, t.SeriesIndex, t.NotifiedAt,
			t.SnoozeUntil, t.CreatedAt, t.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteTask 删除一条。
func (s *Store) DeleteTask(id string) error {
	_, err := s.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	return err
}

// DeleteTasksWhere 按系列删除（seriesId 为空时全删，仅备份导入用）。
func (s *Store) DeleteTasksWhere(seriesID string) error {
	if seriesID == "" {
		_, err := s.db.Exec(`DELETE FROM tasks`)
		return err
	}
	_, err := s.db.Exec(`DELETE FROM tasks WHERE series_id=?`, seriesID)
	return err
}

// Settings 读设置（不存在返回默认值）。
func (s *Store) Settings() (model.Settings, error) {
	out := model.DefaultSettings()
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key='settings'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return model.DefaultSettings(), nil // 配置损坏回退默认
	}
	if out.Language == "" {
		out.Language = "zh-CN"
	}
	if out.WeekStart == 0 {
		out.WeekStart = 1
	}
	return out, nil
}

// SaveSettings 写设置。
func (s *Store) SaveSettings(v model.Settings) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings(key,value) VALUES('settings',?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw))
	return err
}

// History 读未清除的提醒历史。
func (s *Store) History() ([]model.HistoryEntry, error) {
	rows, err := s.db.Query(`SELECT id,task_id,at FROM history WHERE dismissed=0 ORDER BY at DESC LIMIT 300`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.HistoryEntry{}
	for rows.Next() {
		h := model.HistoryEntry{}
		if err := rows.Scan(&h.ID, &h.TaskID, &h.At); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// AddHistory 记录一次提醒触发。
func (s *Store) AddHistory(taskID string, at int64) error {
	_, err := s.db.Exec(`INSERT INTO history(task_id,at) VALUES(?,?)`, taskID, at)
	return err
}

// DismissHistory 清除一条提醒历史。
func (s *Store) DismissHistory(id int64) error {
	_, err := s.db.Exec(`UPDATE history SET dismissed=1 WHERE id=?`, id)
	return err
}

// DismissAllHistory 清除全部提醒历史。
func (s *Store) DismissAllHistory() error {
	_, err := s.db.Exec(`UPDATE history SET dismissed=1 WHERE dismissed=0`)
	return err
}

// Backup 完整备份对象。
type Backup struct {
	Version  int             `json:"version"`
	Exported string          `json:"exportedAt"`
	Tasks    []*model.Task   `json:"tasks"`
	Settings model.Settings  `json:"settings"`
}

// Export 导出备份。
func (s *Store) Export() (*Backup, error) {
	ts, err := s.Tasks()
	if err != nil {
		return nil, err
	}
	st, err := s.Settings()
	if err != nil {
		return nil, err
	}
	return &Backup{Version: 1, Exported: dates.Key(time.Now()), Tasks: ts, Settings: st}, nil
}

// Import 全量导入（覆盖式）。
func (s *Store) Import(b *Backup) error {
	if b.Version != 1 {
		return fmt.Errorf("unsupported backup version %d", b.Version)
	}
	if err := s.DeleteTasksWhere(""); err != nil { // 覆盖式导入：先清空
		return err
	}
	if err := s.SaveTasks(b.Tasks); err != nil {
		return err
	}
	return s.SaveSettings(b.Settings)
}
