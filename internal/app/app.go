// Package app 应用装配：WebView2 窗口 + 命令注册表 + 提醒调度。
package app

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jchv/go-webview2"
	"github.com/zalando/go-keyring"

	"myschedule/internal/ai"
	"myschedule/internal/dates"
	"myschedule/internal/holidays"
	"myschedule/internal/model"
	"myschedule/internal/plans"
	"myschedule/internal/registry"
	"myschedule/internal/scheduler"
	"myschedule/internal/storage"
	webui "myschedule/web"
)

const keyringService = "my-schedule"
const keyringUser = "ai-key"

// App 应用实例。
type App struct {
	Version string
	DataDir string

	store *storage.Store
	svc   *plans.Service
	hol   *holidays.Holidays
	sched *scheduler.Scheduler
	reg   *registry.Registry

	wv webview2.WebView
}

// New 打开存储、装配服务与命令注册表（不创建窗口）。
func New(version, dataDir string) (*App, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	store, err := storage.Open(filepath.Join(dataDir, "schedule.db"))
	if err != nil {
		return nil, err
	}
	a := &App{
		Version: version,
		DataDir: dataDir,
		store:   store,
		svc:     plans.New(store, nil),
		reg:     registry.New(),
	}
	hol, err := holidays.New()
	if err != nil {
		return nil, fmt.Errorf("初始化节假日数据失败: %w", err)
	}
	a.hol = hol
	a.sched = scheduler.New(a.svc, a.onRemind)
	a.register()
	return a, nil
}

// Run 启动 WebView2 窗口并阻塞至关闭。
func (a *App) Run(debug bool) error {
	// 1) 本地回环静态服务（WebView2 加载嵌入式前端）
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("监听本地端口失败: %w", err)
	}
	defer ln.Close()
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			name := filepath.Base(r.URL.Path)
			if name == "/" || name == "." || name == "" {
				name = "index.html"
			}
			data, err := webui.FS.ReadFile(name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			switch filepath.Ext(name) {
			case ".css":
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
			case ".js":
				w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			default:
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			}
			_, _ = w.Write(data)
		})
		_ = http.Serve(ln, mux)
	}()

	// 2) WebView2 窗口
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		Debug:     debug,
		DataPath:  filepath.Join(a.DataDir, "webview2"),
		WindowOptions: webview2.WindowOptions{
			Title:  "我的日程",
			Width:  1180,
			Height: 760,
			Center: true,
		},
	})
	a.wv = wv
	if err := wv.Bind("native", a.bridge); err != nil {
		return fmt.Errorf("绑定桥接失败: %w", err)
	}
	wv.Navigate(fmt.Sprintf("http://%s/index.html", ln.Addr().String()))

	a.sched.Start()
	defer a.sched.Stop()
	defer a.store.Close()

	wv.Run() // 阻塞至窗口关闭
	return nil
}

// bridge 前端统一入口：native('module.method', params)。
func (a *App) bridge(method string, params json.RawMessage) (any, error) {
	return a.reg.Invoke(method, params)
}

// eval 在 UI 线程执行 JS（推送事件到前端）。
func (a *App) eval(js string) {
	if a.wv == nil {
		return
	}
	a.wv.Dispatch(func() { a.wv.Eval(js) })
}

// onRemind 调度回调：弹 Windows 通知 + 推送应用内提醒浮层。
func (a *App) onRemind(tasks []*model.Task) {
	if len(tasks) == 0 {
		return
	}
	// 系统通知（尽力而为，失败不阻塞）
	titles := make([]string, len(tasks))
	for i, t := range tasks {
		titles[i] = t.Title
	}
	body := titles[0]
	if len(tasks) > 1 {
		body = fmt.Sprintf("%s 等 %d 项计划", titles[0], len(tasks))
	}
	go toastNotify("我的日程 · 计划提醒", body)

	payload, err := json.Marshal(tasks)
	if err != nil {
		return
	}
	a.eval(fmt.Sprintf("window.__push&&__push('reminder', %s)", payload))
}

// ---- 命令注册（与 docs/module-api.md 一一对应） ----

func (a *App) register() {
	r := a.reg

	r.Register("plans", "list", "列出计划（可按日期区间/分类/状态/关键词过滤）", `{"from":"2026-01-01","to":"2026-01-31"}`, func(p json.RawMessage) (any, error) {
		var f plans.ListFilter
		if len(p) > 0 {
			if err := json.Unmarshal(p, &f); err != nil {
				return nil, invalid("plans.list 参数", err)
			}
		}
		tasks, err := a.svc.List(f)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tasks": tasks}, nil
	})

	r.Register("plans", "get", "取单条计划", `{"id":"…"}`, func(p json.RawMessage) (any, error) {
		var q struct{ ID string `json:"id"` }
		if err := json.Unmarshal(p, &q); err != nil || q.ID == "" {
			return nil, invalid("plans.get 参数", err)
		}
		return a.svc.Task(q.ID)
	})

	r.Register("plans", "save", "新建或更新计划；repeat 支持 4|8|12（每周一次）或 {frequency:daily|weekdays,until}", `{"task":{"title":"…"},"repeat":null}`, func(p json.RawMessage) (any, error) {
		var q struct {
			Task   model.Task       `json:"task"`
			Repeat json.RawMessage `json:"repeat"`
			ID     string          `json:"id"`
		}
		if err := json.Unmarshal(p, &q); err != nil {
			return nil, invalid("plans.save 参数", err)
		}
		var existing *model.Task
		if q.ID != "" {
			t, err := a.svc.Task(q.ID)
			if err != nil {
				return nil, err
			}
			existing = t
		}
		tasks, err := a.svc.Save(q.Task, q.Repeat, existing)
		if err != nil {
			return nil, err
		}
		a.sched.Wake()
		return map[string]any{"tasks": tasks}, nil
	})

	r.Register("plans", "toggle", "完成/恢复计划", `{"id":"…","done":true}`, func(p json.RawMessage) (any, error) {
		var q struct {
			ID   string `json:"id"`
			Done bool   `json:"done"`
		}
		if err := json.Unmarshal(p, &q); err != nil || q.ID == "" {
			return nil, invalid("plans.toggle 参数", err)
		}
		t, err := a.svc.Toggle(q.ID, q.Done)
		if err != nil {
			return nil, err
		}
		a.sched.Wake()
		return t, nil
	})

	r.Register("plans", "delete", "删除计划", `{"id":"…"}`, func(p json.RawMessage) (any, error) {
		var q struct{ ID string `json:"id"` }
		if err := json.Unmarshal(p, &q); err != nil || q.ID == "" {
			return nil, invalid("plans.delete 参数", err)
		}
		if err := a.svc.Delete(q.ID); err != nil {
			return nil, err
		}
		a.sched.Wake()
		return map[string]any{"ok": true}, nil
	})

	r.Register("plans", "move", "移动计划到新日期/时间（保持跨度）", `{"id":"…","date":"2026-01-02","time":"10:00"}`, func(p json.RawMessage) (any, error) {
		var q struct {
			ID   string `json:"id"`
			Date string `json:"date"`
			Time string `json:"time"`
		}
		if err := json.Unmarshal(p, &q); err != nil || q.ID == "" {
			return nil, invalid("plans.move 参数", err)
		}
		t, err := a.svc.Move(q.ID, q.Date, q.Time)
		if err != nil {
			return nil, err
		}
		a.sched.Wake()
		return t, nil
	})

	r.Register("plans", "stats", "区间统计（总数/完成/完成率/待办小时）", `{"from":"2026-01-01","to":"2026-01-31"}`, func(p json.RawMessage) (any, error) {
		var q struct{ From, To string }
		if err := json.Unmarshal(p, &q); err != nil {
			return nil, invalid("plans.stats 参数", err)
		}
		return a.svc.Stats(q.From, q.To)
	})

	r.Register("plans", "due", "当前到点应提醒的计划", `{}`, func(p json.RawMessage) (any, error) {
		tasks, err := a.svc.Due()
		if err != nil {
			return nil, err
		}
		return map[string]any{"tasks": tasks}, nil
	})

	r.Register("plans", "snooze", "稍后提醒", `{"id":"…","minutes":10}`, func(p json.RawMessage) (any, error) {
		var q struct {
			ID      string `json:"id"`
			Minutes int    `json:"minutes"`
		}
		if err := json.Unmarshal(p, &q); err != nil || q.ID == "" {
			return nil, invalid("plans.snooze 参数", err)
		}
		if q.Minutes <= 0 || q.Minutes > 24*60 {
			return nil, fmt.Errorf("invalid_input: 分钟数需为 1–1440")
		}
		until := a.svc.Now().Add(time.Duration(q.Minutes) * time.Minute).UnixMilli()
		if err := a.svc.Snooze(q.ID, until); err != nil {
			return nil, err
		}
		a.sched.Wake()
		return map[string]any{"ok": true}, nil
	})

	r.Register("plans", "previewAction", "预览系列操作（complete/reopen/delete），返回带 token 的待操作列表", `{"id":"…","action":"complete"}`, func(p json.RawMessage) (any, error) {
		var q struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal(p, &q); err != nil || q.ID == "" {
			return nil, invalid("plans.previewAction 参数", err)
		}
		return a.svc.PreviewAction(q.ID, q.Action)
	})

	r.Register("plans", "applyAction", "应用系列操作（需 preview 返回的 token）", `{"id":"…","action":"complete","token":"…"}`, func(p json.RawMessage) (any, error) {
		var q plans.SeriesAction
		if err := json.Unmarshal(p, &q); err != nil {
			return nil, invalid("plans.applyAction 参数", err)
		}
		n, err := a.svc.ApplyAction(q)
		if err != nil {
			return nil, err
		}
		a.sched.Wake()
		return map[string]any{"count": n}, nil
	})

	r.Register("holidays", "day", "某天的农历与节假日信息", `{"date":"2026-01-01"}`, func(p json.RawMessage) (any, error) {
		var q struct{ Date string `json:"date"` }
		if err := json.Unmarshal(p, &q); err != nil || !dates.IsDateKey(q.Date) {
			return nil, fmt.Errorf("invalid_input: 需要合法日期 YYYY-MM-DD")
		}
		info, err := a.hol.Day(q.Date)
		if err != nil {
			return nil, err
		}
		return info, nil
	})

	r.Register("holidays", "cn", "全年中国法定节假日（含调休）", `{"year":2026}`, func(p json.RawMessage) (any, error) {
		y, err := yearOf(p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"year": y, "days": a.hol.CN(y)}, nil
	})

	r.Register("holidays", "us", "全年美国联邦假日", `{"year":2026}`, func(p json.RawMessage) (any, error) {
		y, err := yearOf(p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"year": y, "days": a.hol.US(y)}, nil
	})

	r.Register("settings", "get", "读取设置", `{}`, func(json.RawMessage) (any, error) {
		s, err := a.store.Settings()
		if err != nil {
			return nil, err
		}
		return s, nil
	})

	r.Register("settings", "set", "更新设置（patch 部分字段；apiKey 存系统凭据管理器）", `{"patch":{"showLunar":true},"apiKey":"sk-…"}`, func(p json.RawMessage) (any, error) {
		var q struct {
			Patch struct {
				Language      *string `json:"language"`
				WeekStart     *int    `json:"weekStart"`
				ShowLunar     *bool   `json:"showLunar"`
				ShowHolidays  *bool   `json:"showHolidays"`
				AIEndpoint    *string `json:"aiEndpoint"`
				AIModel       *string `json:"aiModel"`
			} `json:"patch"`
			APIKey *string `json:"apiKey"`
		}
		if err := json.Unmarshal(p, &q); err != nil {
			return nil, invalid("settings.set 参数", err)
		}
		s, err := a.store.Settings()
		if err != nil {
			return nil, err
		}
		if q.Patch.Language != nil {
			s.Language = *q.Patch.Language
		}
		if q.Patch.WeekStart != nil {
			s.WeekStart = *q.Patch.WeekStart
		}
		if q.Patch.ShowLunar != nil {
			s.ShowLunar = *q.Patch.ShowLunar
		}
		if q.Patch.ShowHolidays != nil {
			s.ShowHolidays = *q.Patch.ShowHolidays
		}
		if q.Patch.AIEndpoint != nil {
			s.AIEndpoint = *q.Patch.AIEndpoint
		}
		if q.Patch.AIModel != nil {
			s.AIModel = *q.Patch.AIModel
		}
		if err := a.store.SaveSettings(s); err != nil {
			return nil, err
		}
		if q.APIKey != nil {
			if *q.APIKey == "" {
				_ = keyring.Delete(keyringService, keyringUser)
			} else {
				if err := keyring.Set(keyringService, keyringUser, *q.APIKey); err != nil {
					return nil, fmt.Errorf("保存 API Key 失败: %w", err)
				}
			}
		}
		return s, nil
	})

	r.Register("backup", "export", "导出全部数据（橙子日程兼容 JSON）", `{}`, func(json.RawMessage) (any, error) {
		return a.store.Export()
	})

	r.Register("backup", "import", "导入备份（覆盖现有数据）", `{"backup":{…}}`, func(p json.RawMessage) (any, error) {
		var q struct {
			Backup json.RawMessage `json:"backup"`
		}
		if err := json.Unmarshal(p, &q); err != nil || len(q.Backup) == 0 {
			return nil, invalid("backup.import 参数", err)
		}
		var b storage.Backup
		if err := json.Unmarshal(q.Backup, &b); err != nil {
			return nil, invalid("备份内容不是合法 JSON", err)
		}
		if err := a.store.Import(&b); err != nil {
			return nil, err
		}
		a.sched.Wake()
		return map[string]any{"ok": true}, nil
	})

	r.Register("ai", "parse", "自然语言解析为计划草稿（OpenAI 兼容端点）", `{"text":"下周三下午三点开会"}`, func(p json.RawMessage) (any, error) {
		var q struct{ Text string `json:"text"` }
		if err := json.Unmarshal(p, &q); err != nil || q.Text == "" {
			return nil, invalid("ai.parse 参数", err)
		}
		s, err := a.store.Settings()
		if err != nil {
			return nil, err
		}
		key, err := keyring.Get(keyringService, keyringUser)
		if err != nil || key == "" {
			return nil, fmt.Errorf("invalid_input: 请先在设置中填写 AI API Key")
		}
		if s.AIEndpoint == "" || s.AIModel == "" {
			return nil, fmt.Errorf("invalid_input: 请先在设置中填写 AI 端点与模型")
		}
		c := ai.NewClient(s.AIEndpoint, s.AIModel, key)
		draft, raw, err := c.Parse(a.ctx(), q.Text)
		if err != nil {
			return nil, err
		}
		return map[string]any{"draft": draft, "raw": raw}, nil
	})

	r.Register("app", "info", "应用信息（版本/数据目录/平台）", `{}`, func(json.RawMessage) (any, error) {
		return map[string]any{
			"version": a.Version, "dataDir": a.DataDir,
			"os": "windows", "arch": "amd64",
		}, nil
	})

	r.Register("app", "testReminder", "发送一条测试提醒（验证通知链路）", `{}`, func(json.RawMessage) (any, error) {
		t := &model.Task{Title: "测试提醒", Date: dates.Key(a.svc.Now()), Time: a.svc.Now().Format("15:04")}
		a.onRemind([]*model.Task{t})
		return map[string]any{"ok": true}, nil
	})

	r.Register("app", "quit", "退出应用", `{}`, func(json.RawMessage) (any, error) {
		go func() {
			a.sched.Stop()
			a.store.Close()
			if a.wv != nil {
				a.wv.Terminate()
			} else {
				os.Exit(0)
			}
		}()
		return map[string]any{"ok": true}, nil
	})
}

func invalid(what string, err error) error {
	if err == nil {
		return fmt.Errorf("invalid_input: %s", what)
	}
	return fmt.Errorf("invalid_input: %s: %w", what, err)
}

func yearOf(p json.RawMessage) (int, error) {
	var q struct{ Year int `json:"year"` }
	if err := json.Unmarshal(p, &q); err != nil || q.Year < 2000 || q.Year > 2100 {
		var s struct{ Year string `json:"year"` }
		if err2 := json.Unmarshal(p, &s); err2 == nil {
			if y, err3 := strconv.Atoi(s.Year); err3 == nil && y >= 2000 && y <= 2100 {
				return y, nil
			}
		}
		return 0, fmt.Errorf("invalid_input: 需要合法年份 2000–2100")
	}
	return q.Year, nil
}
