package plans

import (
	"encoding/json"
	"testing"
	"time"

	"myschedule/internal/model"
	"myschedule/internal/storage"
)

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

func newSvc(t *testing.T) (*Service, *fixedClock) {
	t.Helper()
	st, err := storage.Open(t.TempDir() + "\\test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clk := &fixedClock{now: time.Date(2026, 11, 24, 8, 0, 0, 0, time.Local)}
	return New(st, clk.Now), clk
}

func draft(title, date, tm string, reminder int) model.Task {
	return model.Task{Title: title, Date: date, Time: tm, Duration: 60,
		Reminder: reminder, Category: "work", Priority: "normal"}
}

func TestSaveAndClean(t *testing.T) {
	s, _ := newSvc(t)
	tasks, err := s.Save(draft("写周报", "2026-11-24", "09:00", 15), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID == "" || tasks[0].CreatedAt == 0 {
		t.Fatalf("save: %+v", tasks[0])
	}
	if _, err := s.Save(draft("", "2026-11-24", "09:00", 15), nil, nil); err == nil {
		t.Fatal("empty title should fail")
	}
	if _, err := s.Save(draft("x", "2026-11-24", "09:00", 15), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(draft("x", "2026-11-24", "09:00", 7), nil, nil); err == nil {
		t.Fatal("reminder 7 should fail")
	}
	if _, err := s.Save(draft("x", "2026-2-30", "09:00", 0), nil, nil); err == nil {
		t.Fatal("bad date should fail")
	}
	if _, err := s.Save(draft("x", "2026-11-24", "9:00", 0), nil, nil); err == nil {
		t.Fatal("bad time should fail")
	}
}

func TestWeeklyRepeat(t *testing.T) {
	s, _ := newSvc(t)
	tasks, err := s.Save(draft("周会", "2026-11-24", "10:00", 15), json.RawMessage(`4`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 4 {
		t.Fatalf("want 4, got %d", len(tasks))
	}
	if tasks[0].Date != "2026-11-24" || tasks[1].Date != "2026-12-01" || tasks[3].Date != "2026-12-15" {
		t.Fatalf("dates: %s %s %s", tasks[0].Date, tasks[1].Date, tasks[3].Date)
	}
	for i, tk := range tasks {
		if tk.SeriesID == "" || tk.SeriesIndex != i {
			t.Fatalf("series meta: %+v", tk)
		}
	}
	// 重复规则仅限新建
	if _, err := s.Save(draft("改", "2026-11-25", "10:00", 15), json.RawMessage(`4`), tasks[0]); err == nil {
		t.Fatal("repeat on update should fail")
	}
}

func TestDailyAndWeekdays(t *testing.T) {
	s, _ := newSvc(t)
	daily, err := s.Save(draft("背单词", "2026-11-24", "08:00", 0), json.RawMessage(`{"frequency":"daily","until":"2026-11-26"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 3 {
		t.Fatalf("daily: %d", len(daily))
	}
	wd, err := s.Save(draft("通勤打卡", "2026-11-24", "09:00", -1), json.RawMessage(`{"frequency":"weekdays","until":"2026-11-29"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	// 24(二) 25(三) 26(四) 27(五)；28/29 是周末
	if len(wd) != 4 {
		t.Fatalf("weekdays: %d", len(wd))
	}
	if wd[0].Category != "work" {
		t.Fatal("category default")
	}
}

func TestDueAndSnooze(t *testing.T) {
	s, clk := newSvc(t)
	// 现在 08:00；08:00 提前 15 分钟 → 07:45 已过，应提醒
	tasks, err := s.Save(draft("早会", "2026-11-24", "08:00", 15), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	due, err := s.Due()
	if err != nil || len(due) != 1 {
		t.Fatalf("due: %v %d", err, len(due))
	}
	// 标记已提醒后不再出现
	if err := s.MarkNotified(tasks[0], clk.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	due, _ = s.Due()
	if len(due) != 0 {
		t.Fatalf("after mark: %d", len(due))
	}
	// 稍后 10 分钟：未到点不出
	if err := s.Snooze(tasks[0].ID, clk.Now().Add(10*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	due, _ = s.Due()
	if len(due) != 0 {
		t.Fatal("snooze not yet due")
	}
	// 到点出
	clk.now = clk.now.Add(10 * time.Minute)
	due, _ = s.Due()
	if len(due) != 1 {
		t.Fatal("snooze due missing")
	}
	// 完成后不再提醒
	if _, err := s.Toggle(tasks[0].ID, true); err != nil {
		t.Fatal(err)
	}
	due, _ = s.Due()
	if len(due) != 0 {
		t.Fatal("done should not due")
	}
}

func TestMoveResetsReminder(t *testing.T) {
	s, clk := newSvc(t)
	tasks, err := s.Save(draft("搬迁", "2026-11-24", "09:00", 15), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkNotified(tasks[0], clk.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	moved, err := s.Move(tasks[0].ID, "2026-11-26", "10:30")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Date != "2026-11-26" || moved.Time != "10:30" || moved.NotifiedAt != 0 {
		t.Fatalf("move: %+v", moved)
	}
}

func TestSeriesPreviewApply(t *testing.T) {
	s, _ := newSvc(t)
	tasks, err := s.Save(draft("周报", "2026-11-24", "10:00", 15), json.RawMessage(`4`), nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewAction(tasks[0].ID, "complete")
	if err != nil {
		t.Fatal(err)
	}
	if p.FollowingCount != 3 {
		t.Fatalf("following: %d", p.FollowingCount)
	}
	// token 不匹配 → 冲突
	if _, err := s.ApplyAction(SeriesAction{Action: "complete", ID: tasks[0].ID, Token: "bad"}); err == nil {
		t.Fatal("bad token should fail")
	}
	// 仅当前项
	if n, err := s.ApplyAction(SeriesAction{Action: "complete", ID: tasks[0].ID, Token: p.SingleToken}); err != nil || n != 1 {
		t.Fatalf("single: %v %d", err, n)
	}
	got, _ := s.Task(tasks[0].ID)
	if !got.Done {
		t.Fatal("single complete failed")
	}
	if _, err := s.Task(tasks[1].ID); err != nil {
		t.Fatal(err)
	}
	// 后续全部
	p2, err := s.PreviewAction(tasks[1].ID, "complete")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.ApplyAction(SeriesAction{Action: "complete", ID: tasks[1].ID, Token: p2.Token}); err != nil || n != 3 {
		t.Fatalf("series: %v %d", err, n)
	}
	for _, tk := range tasks[1:] {
		got, _ := s.Task(tk.ID)
		if !got.Done {
			t.Fatalf("series complete: %s", tk.Date)
		}
	}
	// 系列删除
	p3, _ := s.PreviewAction(tasks[0].ID, "delete")
	if n, err := s.ApplyAction(SeriesAction{Action: "delete", ID: tasks[0].ID, Token: p3.Token}); err != nil || n != 4 {
		t.Fatalf("delete: %v %d", err, n)
	}
	if _, err := s.Task(tasks[0].ID); err == nil {
		t.Fatal("should be deleted")
	}
}

func TestListFilterAndStats(t *testing.T) {
	s, _ := newSvc(t)
	for i, d := range []string{"2026-11-20", "2026-11-21", "2026-12-01"} {
		tasks, err := s.Save(draft("t"+d, d, "09:00", 15), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := s.Toggle(tasks[0].ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	inRange, err := s.List(ListFilter{From: "2026-11-01", To: "2026-11-30"})
	if err != nil || len(inRange) != 2 {
		t.Fatalf("list: %v %d", err, len(inRange))
	}
	byCat, _ := s.List(ListFilter{Category: "work"})
	if len(byCat) != 3 {
		t.Fatalf("cat: %d", len(byCat))
	}
	st, err := s.Stats("2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 2 || st.Done != 1 || st.Rate != 0.5 {
		t.Fatalf("stats: %+v", st)
	}
}
