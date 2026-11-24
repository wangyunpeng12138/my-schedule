// Package scheduler 单定时器提醒调度：零空闲 CPU，到点补发。
package scheduler

import (
	"sync"
	"time"

	"myschedule/internal/model"
	"myschedule/internal/plans"
)

const (
	maxSleep = 10 * time.Minute // 防睡眠/改时钟上限
	minSleep = 5 * time.Second  // 唤醒风暴下限
)

// Scheduler 调度器。
type Scheduler struct {
	svc    *plans.Service
	notify func([]*model.Task) // 触发回调（后续项为本次新到点）
	Now    func() time.Time

	mu     sync.Mutex
	timer  *time.Timer
	stop   chan struct{}
	closed bool
}

// New 创建调度器。notify 会在触发线程被同步调用。
func New(svc *plans.Service, notify func([]*model.Task)) *Scheduler {
	return &Scheduler{svc: svc, notify: notify, Now: time.Now, stop: make(chan struct{})}
}

// Start 启动调度循环。
func (s *Scheduler) Start() {
	go s.loop()
}

// Stop 停止（幂等）。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.stop)
	if s.timer != nil {
		s.timer.Stop()
	}
}

func (s *Scheduler) loop() {
	s.tick() // 启动即对齐一次
	for {
		next := s.snoozeTarget()
		var d time.Duration
		if next.IsZero() {
			d = maxSleep
		} else {
			d = time.Until(next)
		}
		if d > maxSleep {
			d = maxSleep
		}
		if d < minSleep {
			d = minSleep
		}
		t := time.NewTimer(d)
		s.setTimer(t)
		select {
		case <-s.stop:
			t.Stop()
			return
		case <-t.C:
			s.tick()
		}
	}
}

func (s *Scheduler) setTimer(t *time.Timer) {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = t
	s.mu.Unlock()
}

// snoozeTarget 下一个到点时刻（空则 maxSleep 兜底）。
func (s *Scheduler) snoozeTarget() time.Time {
	ms := s.svc.NextDueMs()
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// tick 扫描到点任务：标记 + 记历史 + 一次回调（首次与稍后提醒同一路径）。
func (s *Scheduler) tick() {
	due, err := s.svc.Due()
	if err != nil || len(due) == 0 {
		return
	}
	nowMs := s.Now().UnixMilli()
	var fresh []*model.Task
	for _, t := range due {
		_ = s.svc.MarkNotified(t, nowMs)
		_ = s.svc.Store().AddHistory(t.ID, nowMs)
		fresh = append(fresh, t)
	}
	if len(fresh) > 0 && s.notify != nil {
		s.notify(fresh)
	}
}

// Wake 外部数据变更后立即重对齐（保持定时器单例）。
func (s *Scheduler) Wake() {
	s.mu.Lock()
	t := s.timer
	s.mu.Unlock()
	if t != nil {
		t.Reset(time.Millisecond)
	}
}
