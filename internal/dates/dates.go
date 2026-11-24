// Package dates 提供日期纯函数：无 IO、无全局状态，全部可注入时间测试。
package dates

import (
	"errors"
	"time"

	"myschedule/internal/model"
)

// ErrInvalidDate 非法日期。
var ErrInvalidDate = errors.New("invalid date")

// Key 本地日期 → YYYY-MM-DD。
func Key(t time.Time) string { return t.Format("2006-01-02") }

// TodayKey 今天。
func TodayKey(now time.Time) string { return Key(now) }

// Parse 解析 YYYY-MM-DD 为本地零点；不合法返回错误。
func Parse(key string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", key, time.Local)
	if err != nil || t.Format("2006-01-02") != key {
		return time.Time{}, ErrInvalidDate
	}
	return t, nil
}

// IsDateKey 是否合法 YYYY-MM-DD。
func IsDateKey(s string) bool { _, err := Parse(s); return err == nil }

// AddDays 日期平移 n 天。
func AddDays(key string, n int) (string, error) {
	t, err := Parse(key)
	if err != nil {
		return "", err
	}
	return Key(t.AddDate(0, 0, n)), nil
}

// DiffDays 两个日期相差天数（a-b）。
func DiffDays(a, b string) (int, error) {
	ta, err := Parse(a)
	if err != nil {
		return 0, err
	}
	tb, err := Parse(b)
	if err != nil {
		return 0, err
	}
	return int(ta.Sub(tb).Hours() / 24), nil
}

// ClampTime 规范化 HH:MM。
func ClampTime(s string) string {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return "09:00"
	}
	return t.Format("15:04")
}

// Monday 返回所在周的周一（weekStart 可配置时按周起始日取整）。
func Monday(t time.Time, weekStart int) time.Time {
	wd := int(t.Weekday()) // 0 周日
	offset := (wd - weekStart + 7) % 7
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, -offset)
}

// TaskEndDate 计划结束日（无 endDate 时与开始日相同）。
func TaskEndDate(t *model.Task) string {
	if t.EndDate != "" && t.EndDate >= t.Date {
		return t.EndDate
	}
	return t.Date
}

// SpanDays 跨天天数（含首尾）。
func SpanDays(t *model.Task) int {
	end := TaskEndDate(t)
	if n, err := DiffDays(end, t.Date); err == nil {
		return n + 1
	}
	return 1
}

// EndDateForDays 由开始日+天数推结束日。
func EndDateForDays(start string, days int) (string, error) {
	if days < 1 || days > 366 {
		return "", ErrInvalidDate
	}
	return AddDays(start, days-1)
}

// OccursOn 计划是否覆盖某日（含跨天）。
func OccursOn(t *model.Task, key string) bool {
	return t.Date <= key && key <= TaskEndDate(t)
}

// OverlapsRange 计划是否与 [start,end] 相交。
func OverlapsRange(t *model.Task, start, end string) bool {
	return t.Date <= end && TaskEndDate(t) >= start
}

// ShiftStart 把计划整体平移到 newStart，保持跨度，返回新的 (date, endDate)。
func ShiftStart(t *model.Task, newStart string) (string, string, error) {
	n, err := DiffDays(newStart, t.Date)
	if err != nil {
		return "", "", err
	}
	ne, err := AddDays(TaskEndDate(t), n)
	if err != nil {
		return "", "", err
	}
	return newStart, ne, nil
}
