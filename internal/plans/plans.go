// Package plans 计划领域服务：校验、CRUD、重复生成、系列操作、提醒触发判定。
// 数据模型与规则 1:1 对齐橙子日程（MIT），含其全部校验常量。
package plans

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"myschedule/internal/dates"
	"myschedule/internal/model"
	"myschedule/internal/storage"
)

// 领域常量（对齐橙子日程 core.cjs）。
const (
	TitleLimitMax = 160
	NotesLimit    = 5000
	TaskLimit     = 10000
	SeriesLimit   = 10000
)

var (
	// ErrInvalid 非法输入。
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound 不存在。
	ErrNotFound = os.ErrNotExist
	// ErrLimit 超出上限。
	ErrLimit = errors.New("limit exceeded")

	timeRe  = regexp.MustCompile(`^\d{2}:\d{2}$`)
	cats    = map[string]bool{"work": true, "study": true, "life": true}
	prios   = map[string]bool{"normal": true, "high": true}
	remSet  = map[int]bool{-1: true, 0: true, 5: true, 10: true, 15: true, 30: true, 60: true, 1440: true}
)

// ListFilter plans.list 过滤参数。
type ListFilter struct {
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Category string `json:"category,omitempty"`
	Status   string `json:"status,omitempty"` // all|todo|done
	Query    string `json:"query,omitempty"`
}

// Stats plans.stats 返回。
type Stats struct {
	Total   int     `json:"total"`
	Done    int     `json:"done"`
	Hours   float64 `json:"hours"`
	Rate    float64 `json:"completionRate"`
}

// Service 计划服务。
type Service struct {
	store *storage.Store
	Now   func() time.Time
}

// New 创建服务。
func New(store *storage.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, Now: now}
}

// Store 暴露底层存储（供 backup 等模块复用）。
func (s *Service) Store() *storage.Store { return s.store }

// Clean 校验并规整一条计划（无 id 时新建）。
func (s *Service) Clean(input model.Task, existing *model.Task) (*model.Task, error) {
	t := input
	if t.Title == "" || len([]rune(t.Title)) > TitleLimitMax {
		return nil, fmt.Errorf("%w: 标题需为 1–160 字", ErrInvalid)
	}
	if len([]rune(t.Notes)) > NotesLimit {
		return nil, fmt.Errorf("%w: 备注最长 5000 字", ErrInvalid)
	}
	if !dates.IsDateKey(t.Date) {
		return nil, fmt.Errorf("%w: 非法开始日期 %q", ErrInvalid, t.Date)
	}
	if !timeRe.MatchString(t.Time) {
		return nil, fmt.Errorf("%w: 非法时间 %q", ErrInvalid, t.Time)
	}
	if _, err := time.Parse("15:04", t.Time); err != nil {
		return nil, fmt.Errorf("%w: 非法时间 %q", ErrInvalid, t.Time)
	}
	if t.Duration < 15 || t.Duration > 1440 {
		return nil, fmt.Errorf("%w: 时长需为 15–1440 分钟", ErrInvalid)
	}
	if !remSet[t.Reminder] {
		return nil, fmt.Errorf("%w: 非法提前分钟数", ErrInvalid)
	}
	if !cats[t.Category] {
		t.Category = "work"
	}
	if !prios[t.Priority] {
		t.Priority = "normal"
	}
	if t.EndDate != "" {
		if !dates.IsDateKey(t.EndDate) {
			return nil, fmt.Errorf("%w: 非法结束日期", ErrInvalid)
		}
		if t.EndDate < t.Date {
			return nil, fmt.Errorf("%w: 结束日期不能早于开始日期", ErrInvalid)
		}
	}
	if existing != nil { // 更新：保留身份与审计字段
		t.ID = existing.ID
		t.SeriesID = existing.SeriesID
		t.SeriesIndex = existing.SeriesIndex
		t.NotifiedAt = existing.NotifiedAt
		t.CreatedAt = existing.CreatedAt
	} else {
		t.ID = uuid.NewString()
		t.CreatedAt = s.Now().UnixMilli()
	}
	t.UpdatedAt = s.Now().UnixMilli()
	return &t, nil
}

// Save 新建/更新；repeat 为空时单条保存。
// repeat: 1|4|8|12（每周 N 次）或 {"frequency":"daily"|"weekdays","until":"YYYY-MM-DD"}。
func (s *Service) Save(draft model.Task, repeat json.RawMessage, existing *model.Task) ([]*model.Task, error) {
	if len(repeat) == 0 || string(repeat) == "null" || string(repeat) == "1" {
		t, err := s.Clean(draft, existing)
		if err != nil {
			return nil, err
		}
		if err := s.store.SaveTask(t); err != nil {
			return nil, err
		}
		return []*model.Task{t}, nil
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: 重复规则仅用于新建", ErrInvalid)
	}
	t, err := s.Clean(draft, nil)
	if err != nil {
		return nil, err
	}
	series, err := s.MakeSeries(t, repeat)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveTasks(series); err != nil {
		return nil, err
	}
	return series, nil
}

// MakeSeries 生成重复系列（新建路径专用）。
func (s *Service) MakeSeries(first *model.Task, repeat json.RawMessage) ([]*model.Task, error) {
	var weekly int
	var spec struct {
		Frequency string `json:"frequency"`
		Until     string `json:"until"`
	}
	raw := strings.TrimSpace(string(repeat))
	if raw[0] == '{' {
		if err := json.Unmarshal(repeat, &spec); err != nil {
			return nil, fmt.Errorf("%w: 非法重复规则", ErrInvalid)
		}
		if spec.Frequency != "daily" && spec.Frequency != "weekdays" {
			return nil, fmt.Errorf("%w: 非法重复频率", ErrInvalid)
		}
		if !dates.IsDateKey(spec.Until) || spec.Until < first.Date {
			return nil, fmt.Errorf("%w: 非法重复结束日期", ErrInvalid)
		}
	} else {
		if err := json.Unmarshal(repeat, &weekly); err != nil || weekly < 2 || weekly > 12 {
			return nil, fmt.Errorf("%w: 非法每周重复次数", ErrInvalid)
		}
	}

	all, err := s.store.Tasks()
	if err != nil {
		return nil, err
	}
	if len(all)+1 > TaskLimit {
		return nil, fmt.Errorf("%w: 计划总数上限 %d", ErrLimit, TaskLimit)
	}

	seriesID := uuid.NewString()
	out := []*model.Task{}
	push := func(date string, idx int) *model.Task {
		c := *first
		c.ID = uuid.NewString()
		c.SeriesID = seriesID
		c.SeriesIndex = idx
		c.Date = date
		if c.EndDate != "" {
			span := dates.SpanDays(first)
			c.EndDate, _ = dates.AddDays(date, span-1)
		}
		out = append(out, &c)
		return &c
	}
	switch {
	case weekly > 0: // 每 7 天一次，共 weekly 次
		for i := 0; i < weekly; i++ {
			d, err := dates.AddDays(first.Date, i*7)
			if err != nil {
				return nil, err
			}
			push(d, i)
		}
	case spec.Frequency == "daily":
		n, _ := dates.DiffDays(spec.Until, first.Date)
		if n+1 > SeriesLimit {
			return nil, fmt.Errorf("%w: 系列上限 %d", ErrLimit, SeriesLimit)
		}
		for i := 0; i <= n; i++ {
			d, _ := dates.AddDays(first.Date, i)
			push(d, i)
		}
	default: // weekdays
		n, _ := dates.DiffDays(spec.Until, first.Date)
		if n+1 > SeriesLimit {
			return nil, fmt.Errorf("%w: 系列上限 %d", ErrLimit, SeriesLimit)
		}
		idx := 0
		for i := 0; i <= n; i++ {
			d, _ := dates.AddDays(first.Date, i)
			t, _ := dates.Parse(d)
			if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
				continue
			}
			push(d, idx)
			idx++
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: 所选范围内没有工作日", ErrInvalid)
		}
	}
	if len(all)+len(out) > TaskLimit {
		return nil, fmt.Errorf("%w: 计划总数上限 %d", ErrLimit, TaskLimit)
	}
	return out, nil
}

// List 按过滤条件列出。
func (s *Service) List(f ListFilter) ([]*model.Task, error) {
	all, err := s.store.Tasks()
	if err != nil {
		return nil, err
	}
	if f.From != "" && !dates.IsDateKey(f.From) {
		return nil, fmt.Errorf("%w: 非法起始日期", ErrInvalid)
	}
	if f.To != "" && !dates.IsDateKey(f.To) {
		return nil, fmt.Errorf("%w: 非法结束日期", ErrInvalid)
	}
	if f.From != "" && f.To != "" && f.From > f.To {
		f.From, f.To = f.To, f.From
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	out := []*model.Task{}
	for _, t := range all {
		if f.Category != "" && t.Category != f.Category {
			continue
		}
		switch f.Status {
		case "todo":
			if t.Done {
				continue
			}
		case "done":
			if !t.Done {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(t.Title+" "+t.Notes), q) {
			continue
		}
		if f.From != "" || f.To != "" {
			if !dates.OverlapsRange(t, orDefault(f.From, "0000-01-01"), orDefault(f.To, "9999-12-31")) {
				continue
			}
		}
		out = append(out, t)
	}
	return out, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Task 取单条。
func (s *Service) Task(id string) (*model.Task, error) {
	return s.store.Task(id)
}

// Toggle 完成/恢复。
func (s *Service) Toggle(id string, done bool) (*model.Task, error) {
	t, err := s.store.Task(id)
	if err != nil {
		return nil, err
	}
	t.Done = done
	t.UpdatedAt = s.Now().UnixMilli()
	if err := s.store.SaveTask(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Delete 删除。
func (s *Service) Delete(id string) error {
	t, err := s.store.Task(id)
	if err != nil {
		return err
	}
	return s.store.DeleteTask(t.ID)
}

// Move 平移到新日期/时间（保持跨度）。
func (s *Service) Move(id, date, timeStr string) (*model.Task, error) {
	t, err := s.store.Task(id)
	if err != nil {
		return nil, err
	}
	if !dates.IsDateKey(date) {
		return nil, fmt.Errorf("%w: 非法日期", ErrInvalid)
	}
	t.Date, t.EndDate, err = dates.ShiftStart(t, date)
	if err != nil {
		return nil, err
	}
	if timeStr != "" {
		if _, err := time.Parse("15:04", timeStr); err != nil {
			return nil, fmt.Errorf("%w: 非法时间", ErrInvalid)
		}
		t.Time = timeStr
	}
	t.NotifiedAt, t.SnoozeUntil = 0, 0 // 改期后重新提醒
	t.UpdatedAt = s.Now().UnixMilli()
	if err := s.store.SaveTask(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Stats 区间统计（总数、完成、未完成小时数、完成率）。
func (s *Service) Stats(from, to string) (Stats, error) {
	if !dates.IsDateKey(from) || !dates.IsDateKey(to) {
		return Stats{}, fmt.Errorf("%w: 非法区间", ErrInvalid)
	}
	all, err := s.List(ListFilter{From: from, To: to})
	if err != nil {
		return Stats{}, err
	}
	st := Stats{}
	for _, t := range all {
		st.Total++
		if t.Done {
			st.Done++
			continue
		}
		st.Hours += float64(t.Duration) / 60
	}
	if st.Total > 0 {
		st.Rate = float64(st.Done) / float64(st.Total)
	}
	return st, nil
}

// Due 当前到点应提醒的计划（橙子 dueTasks 语义）。
func (s *Service) Due() ([]*model.Task, error) {
	all, err := s.store.Tasks()
	if err != nil {
		return nil, err
	}
	now := s.Now()
	out := []*model.Task{}
	for _, t := range all {
		if t.Done || t.Reminder < 0 {
			continue
		}
		at := t.At()
		fired := at.Add(-time.Duration(t.Reminder) * time.Minute)
		if t.SnoozeUntil > 0 {
			if t.SnoozeUntil <= now.UnixMilli() {
				out = append(out, t)
			}
			continue
		}
		if t.NotifiedAt == 0 && !fired.After(now) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At().Before(out[j].At()) })
	return out, nil
}

// NextDueMs 下一次提醒触发的时间戳(ms)；无待提醒返回 0。
func (s *Service) NextDueMs() int64 {
	all, err := s.store.Tasks()
	if err != nil {
		return 0
	}
	nowMs := s.Now().UnixMilli()
	best := int64(0)
	for _, t := range all {
		if t.Done || t.Reminder < 0 {
			continue
		}
		var at int64
		if t.SnoozeUntil > 0 {
			at = t.SnoozeUntil
		} else if t.NotifiedAt == 0 {
			at = t.At().Add(-time.Duration(t.Reminder) * time.Minute).UnixMilli()
		} else {
			continue
		}
		if at < nowMs {
			at = nowMs // 已到点：立即
		}
		if best == 0 || at < best {
			best = at
		}
	}
	return best
}

// MarkNotified 标记已提醒。
func (s *Service) MarkNotified(t *model.Task, atMs int64) error {
	t.NotifiedAt = atMs
	t.SnoozeUntil = 0
	return s.store.SaveTask(t)
}

// Snooze 稍后提醒。
func (s *Service) Snooze(id string, untilMs int64) error {
	t, err := s.store.Task(id)
	if err != nil {
		return err
	}
	t.SnoozeUntil = untilMs
	t.UpdatedAt = s.Now().UnixMilli()
	return s.store.SaveTask(t)
}

// ---- 系列操作（橙子 task-series.cjs：preview + token 防漂移）----

// SeriesAction 系列批量动作。
type SeriesAction struct {
	Action string `json:"action"` // complete|reopen|delete
	ID     string `json:"id"`
	Token  string `json:"token"`
}

// SeriesPreview 系列动作预览。
type SeriesPreview struct {
	Task           *model.Task  `json:"task"`
	Action         string       `json:"action"`
	Source         string       `json:"source"`
	FollowingCount int          `json:"followingCount"`
	Items          []*model.Task `json:"items"`
	Token          string       `json:"token"`
	SingleToken    string       `json:"singleToken"`
}

// PreviewAction 预览系列动作（不落库）。
func (s *Service) PreviewAction(id, action string) (*SeriesPreview, error) {
	switch action {
	case "complete", "reopen", "delete":
	default:
		return nil, fmt.Errorf("%w: 非法系列动作", ErrInvalid)
	}
	t, err := s.store.Task(id)
	if err != nil {
		return nil, err
	}
	items := []*model.Task{}
	if t.SeriesID != "" {
		all, err := s.store.Tasks()
		if err != nil {
			return nil, err
		}
		for _, o := range all {
			if o.SeriesID == t.SeriesID && o.Date > t.Date && o.SeriesIndex >= t.SeriesIndex {
				items = append(items, o)
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	}
	p := &SeriesPreview{Task: t, Action: action, Source: dates.Key(s.Now()), Items: items, FollowingCount: len(items)}
	p.Token = seriesToken(p)
	p.SingleToken = seriesToken(&SeriesPreview{Task: t, Action: action, Source: p.Source, Items: nil})
	return p, nil
}

func seriesToken(p *SeriesPreview) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%d", p.Task.ID, p.Action, p.Source, len(p.Items))
	for _, t := range p.Items {
		fmt.Fprintf(h, "|%s@%s", t.ID, t.Date)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ApplyAction 应用系列动作；token 不匹配则报冲突。
func (s *Service) ApplyAction(req SeriesAction) (int, error) {
	p, err := s.PreviewAction(req.ID, req.Action)
	if err != nil {
		return 0, err
	}
	if p.Token != req.Token && p.SingleToken != req.Token {
		return 0, fmt.Errorf("conflict: %w", ErrInvalid)
	}
	applyOne := func(id string) error {
		switch req.Action {
		case "complete", "reopen":
			_, err := s.Toggle(id, req.Action == "complete")
			return err
		default:
			return s.Delete(id)
		}
	}
	n := 0
	if err := applyOne(p.Task.ID); err != nil { // 当前项始终生效
		return n, err
	}
	n++
	if req.Token == p.Token { // 组内后续项
		for _, t := range p.Items {
			if err := applyOne(t.ID); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}
