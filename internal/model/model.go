// Package model 定义跨模块共享的纯数据类型，禁止依赖其他内部包。
package model

import "time"

// Task 一条计划（与橙子日程数据模型同构）。
type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Date        string `json:"date"` // 开始日 YYYY-MM-DD
	EndDate     string `json:"endDate,omitempty"`
	Time        string `json:"time"`           // HH:MM
	Duration    int    `json:"duration"`       // 分钟
	Reminder    int    `json:"reminder"`       // 提前分钟；-1 不提醒
	Category    string `json:"category"`       // work|study|life
	Priority    string `json:"priority"`       // normal|high
	Notes       string `json:"notes,omitempty"`
	Done        bool   `json:"done"`
	SeriesID    string `json:"seriesId,omitempty"`
	SeriesIndex int    `json:"seriesIndex,omitempty"`
	NotifiedAt  int64  `json:"notifiedAt,omitempty"`  // 上次提醒时间(ms)
	SnoozeUntil int64  `json:"snoozeUntil,omitempty"` // 稍后提醒时间(ms)
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// At 本地时间轴上的触发时刻（date+time）。
func (t *Task) At() time.Time {
	d, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
	if err != nil {
		return time.Time{}
	}
	hh, mm := 0, 0
	if n, err := time.Parse("15:04", t.Time); err == nil {
		hh, mm = n.Hour(), n.Minute()
	}
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, time.Local)
}

// Settings 应用设置；AI Key 存系统凭据管理器，不在此处。
type Settings struct {
	Language      string `json:"language"`      // zh-CN | en
	ShowLunar     bool   `json:"showLunar"`     // 日历显示农历
	ShowHolidays  bool   `json:"showHolidays"`  // 日历显示节假日角标
	AIEndpoint    string `json:"aiEndpoint"`    // OpenAI 兼容 base URL
	AIModel       string `json:"aiModel"`       // 模型名
	WeekStart     int    `json:"weekStart"`     // 0 周日 1 周一
}

// DefaultSettings 返回出厂设置。
func DefaultSettings() Settings {
	return Settings{Language: "zh-CN", ShowLunar: true, ShowHolidays: true, WeekStart: 1}
}

// HistoryEntry 提醒历史（提醒中心数据源）。
type HistoryEntry struct {
	ID     int64  `json:"id"`
	TaskID string `json:"taskId"`
	At     int64  `json:"at"` // 触发时间(ms)
}
