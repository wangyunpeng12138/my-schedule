// 我的日程 —— Go 原生日历应用（WebView2 壳）。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"myschedule/internal/app"
	"myschedule/internal/model"
)

// Version 版本号（git tag 时以 -ldflags 覆盖）。
var Version = "0.1.0"

func main() {
	var (
		showVersion = flag.Bool("version", false, "显示版本号")
		selftest    = flag.Bool("selftest", false, "自检：核心链路快速验证后退出")
		evalCmd     = flag.String("eval", "", "调用一个命令（如 \"plans.list {}\"）并打印 JSON")
		dataDir     = flag.String("data-dir", "", "数据目录（默认 %AppData%\\MySchedule）")
		debug       = flag.Bool("debug", false, "启用 WebView2 开发者工具")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("my-schedule", Version)
		return
	}
	dir := *dataDir
	if dir == "" {
		dir = os.Getenv("MY_SCHEDULE_DATA")
	}
	if dir == "" {
		if ad, err := os.UserConfigDir(); err == nil { // Windows: %AppData%
			dir = filepath.Join(ad, "MySchedule")
		} else {
			dir = "."
		}
	}

	a, err := app.New(Version, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		os.Exit(1)
	}

	if *evalCmd != "" {
		if err := runEval(a, *evalCmd); err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			os.Exit(1)
		}
		return
	}
	if *selftest {
		if err := runSelftest(a); err != nil {
			fmt.Fprintln(os.Stderr, "自检失败:", err)
			os.Exit(1)
		}
		fmt.Println("selftest OK")
		return
	}

	if err := a.Run(*debug); err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
}

// runEval 命令行直调命令（与前端/未来的 MCP 完全同一条路径）。
func runEval(a *app.App, spec string) error {
	method, arg := spec, "{}"
	for i := 0; i < len(spec); i++ {
		if spec[i] == ' ' {
			method, arg = spec[:i], spec[i+1:]
			break
		}
	}
	var params json.RawMessage
	if arg != "" {
		params = json.RawMessage(arg)
	}
	out, err := a.Eval(method, params)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	return nil
}

// runSelftest 核心链路自检：写读改删 + 农历节假日 + 统计 + 提醒到期判定。
func runSelftest(a *app.App) error {
	const (
		list   = "plans.list"
		save   = "plans.save"
		toggle = "plans.toggle"
		del    = "plans.delete"
	)
	d := time.Now().Format("2006-01-02")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%100000)

	// 1) 新建
	created, err := a.Eval(save, json.RawMessage(fmt.Sprintf(
		`{"task":{"title":"自检-%s","date":%q,"time":"09:00","duration":60,"reminder":15,"category":"life"}}`, suffix, d)))
	if err != nil {
		return fmt.Errorf("save: %w", err)
	}
	m, _ := created.(map[string]any)
	ts, _ := m["tasks"].([]*model.Task)
	if len(ts) == 0 {
		return fmt.Errorf("save: 未返回任务")
	}
	t0 := ts[0]
	id := t0.ID

	// 2) 列表/读取
	if _, err = a.Eval(list, json.RawMessage(`{"query":"自检-"}`)); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	// 3) 完成/恢复
	if _, err = a.Eval(toggle, json.RawMessage(fmt.Sprintf(`{"id":%q,"done":true}`, id))); err != nil {
		return fmt.Errorf("toggle: %w", err)
	}
	// 4) 农历/节假日
	if _, err = a.Eval("holidays.day", json.RawMessage(fmt.Sprintf(`{"date":%q}`, d))); err != nil {
		return fmt.Errorf("holidays.day: %w", err)
	}
	if _, err = a.Eval("holidays.cn", json.RawMessage(`{"year":2026}`)); err != nil {
		return fmt.Errorf("holidays.cn: %w", err)
	}
	// 5) 统计/提醒
	if _, err = a.Eval("plans.stats", json.RawMessage(fmt.Sprintf(`{"from":%q,"to":%q}`, d, d))); err != nil {
		return fmt.Errorf("stats: %w", err)
	}
	if _, err = a.Eval("plans.due", json.RawMessage(`{}`)); err != nil {
		return fmt.Errorf("due: %w", err)
	}
	// 6) 清理
	if _, err = a.Eval(del, json.RawMessage(fmt.Sprintf(`{"id":%q}`, id))); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}
