package storage

import (
	"path/filepath"
	"testing"

	"myschedule/internal/model"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var idSeq int

func sampleTask(title string) *model.Task {
	idSeq++
	return &model.Task{
		ID: "t-" + title, Title: title, Date: "2026-11-24", Time: "09:00",
		Duration: 60, Reminder: 15, Category: "work", Priority: "normal",
		CreatedAt: int64(1000 + idSeq), UpdatedAt: int64(2000 + idSeq),
	}
}

func TestSaveAndQuery(t *testing.T) {
	s := openTemp(t)
	tk := sampleTask("写周报")
	if err := s.SaveTask(tk); err != nil {
		t.Fatal(err)
	}
	got, err := s.Task(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "写周报" || got.CreatedAt == 0 {
		t.Fatalf("roundtrip: %+v", got)
	}
	all, err := s.Tasks()
	if err != nil || len(all) != 1 {
		t.Fatalf("tasks: %v %d", err, len(all))
	}
}

func TestUpdatePersists(t *testing.T) {
	s := openTemp(t)
	tk := sampleTask("旧标题")
	_ = s.SaveTask(tk)
	tk.Title = "新标题"
	tk.Done = true
	if err := s.SaveTask(tk); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Tasks()
	if len(all) != 1 || all[0].Title != "新标题" || !all[0].Done {
		t.Fatalf("update: %+v", all)
	}
}

func TestDeleteAndSeries(t *testing.T) {
	s := openTemp(t)
	a, b := sampleTask("A"), sampleTask("B")
	a.SeriesID = "s1"
	b.SeriesID = "s1"
	_ = s.SaveTask(a)
	_ = s.SaveTask(b)
	_ = s.SaveTask(sampleTask("C"))
	if err := s.DeleteTasksWhere("s1"); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Tasks()
	if len(all) != 1 || all[0].Title != "C" {
		t.Fatalf("series delete: %+v", all)
	}
}

func TestSettings(t *testing.T) {
	s := openTemp(t)
	got, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Language != "zh-CN" || got.WeekStart != 1 {
		t.Fatalf("defaults: %+v", got)
	}
	got.ShowLunar = false
	got.AIEndpoint = "https://api.deepseek.com/v1"
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	got2, err := s.Settings()
	if err != nil || got2.AIEndpoint != "https://api.deepseek.com/v1" || got2.ShowLunar {
		t.Fatalf("saved: %+v", got2)
	}
}

func TestBackupRoundTrip(t *testing.T) {
	s := openTemp(t)
	_ = s.SaveTask(sampleTask("备份"))
	_ = s.SaveTask(sampleTask("两条"))
	b, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Tasks) != 2 || b.Version != 1 {
		t.Fatalf("export: %+v", b)
	}
	// 覆盖式导入：先清后写
	b.Tasks = b.Tasks[:1]
	if err := s.Import(b); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Tasks()
	if len(all) != 1 {
		t.Fatalf("import: %d", len(all))
	}
}

func TestCorruptSettingsFallback(t *testing.T) {
	s := openTemp(t)
	if _, err := s.DB().Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES ('settings', '{bad json')`); err != nil {
		t.Fatal(err)
	}
	got, err := s.Settings()
	if err != nil || got.Language != "zh-CN" {
		t.Fatalf("fallback: %+v %v", got, err)
	}
}
