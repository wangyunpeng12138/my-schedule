# my-schedule 架构设计

> 依据 `E:\agentWork\agendling\docs\event-reminder-design.md`（v1.1）落地，融合：
> - **agendling**（Go 原生）：单定时器调度、SQLite WAL、零空闲 CPU 的内核思路；
> - **orange-schedule**（Electron）：页面结构、计划数据模型、四视图与交互习惯。

## 1. 目标

Windows 桌面事件提醒应用：仿橙子日程界面，农历 + 中国法定节假日（含调休补班）+ 美国联邦假日，命令式模块接口（可 MCP 化），AI 接入，离线优先，**空闲内存 ≤ 60 MB、空闲 CPU ≈ 0%、冷启动 < 1s**。

## 2. 技术选型（决策记录）

| 决策点 | 选择 | 理由 |
| --- | --- | --- |
| 外壳 | Go + **WebView2**（`jchv/go-webview2`，纯 Go 无 CGO） | HTML/CSS 一比一复刻橙子界面；WebView2 为 Windows 10/11 系统自带共享运行时，不随包分发 Chromium |
| 存储 | **SQLite**（`modernc.org/sqlite`，WAL） | 单文件、纯 Go、断电安全；索引查询支撑 1 万条冷启动 < 1s |
| 农历 | `6tail/lunar-go` | 算法级离线实现 |
| 凭据 | `zalando/go-keyring` | Windows 凭据管理器（DPAPI），AI Key 不明文入库 |
| 提醒 | 单定时器 `time.Timer` + Windows 通知 + 站内弹层 | 沿用 agendling「无轮询」调度模型 |
| 前端 | 原生 HTML/CSS/JS，`go:embed` 进二进制 | 零构建链、离线、低开销 |

被否方案：Electron（空闲 150–300 MB，违反低占用约束）；纯 Win32 自绘（复刻成本高）。

## 3. 进程模型与通信桥

```
┌────────────────────────── my-schedule.exe（单进程）──────────────────────────┐
│  main ─→ app.App ─┬─ webview2 主窗口（加载 go:embed 的 web/）                  │
│                   │     JS ⇄ 桥：window.external.invoke(JSON-RPC)             │
│                   │            ⇄ registry.Invoke("module.method", params)     │
│                   ├─ storage（SQLite WAL，单连接）                             │
│                   ├─ scheduler（单 Timer：到点 → 通知 + 站内提醒）              │
│                   └─ ai（OpenAI 兼容 HTTP，用户显式触发）                       │
└──────────────────────────────────────────────────────────────────────────────┘
```

- 桥协议：`{id, method, params}` → `{id, result}` / `{id, error}`，与 MCP `tools/call` 天然对齐。
- 所有能力收敛为命令式接口（`docs/module-api.md`），未来加 MCP server 只需包一层 stdio。

## 4. 数据

- 数据目录：`%AppData%\MySchedule`（`MY_SCHEDULE_DATA` 可覆盖），内含 `schedule.db`。
- 备份格式：`{version:1, tasks, settings, history}` JSON，与橙子日程同构，便于互导。

## 5. 模块与里程碑映射

| 模块 | 职责 | 里程碑 |
| --- | --- | --- |
| `dates` / `model` | 日期纯函数、数据类型 | M1 |
| `storage` | SQLite CRUD + 备份导入导出 | M1 |
| `plans` | 校验、重复计划、系列操作、统计 | M2 |
| `holidays` | 农历、CN 节假日数据（2025–2027，可热更）、US 联邦假日观测算法 | M2 |
| `scheduler` | 单定时器提醒，含补发 | M2 |
| `registry` / `app` | 命令注册与桥接 | M3 |
| `ai` | OpenAI 兼容端点调用 → 结构化草稿 | M3 |
| `web` | 界面（四视图、编辑器、设置、提醒） | M4 |

## 6. 扩展路线（后续版本，接口已预留）

1. **托盘常驻**：窗口关闭缩到托盘，提醒后台运行；
2. **MCP server**：registry 直接导出 `tools/list`（`ExportMCPTools` 已实现）；
3. **云端合并同步**：LWW + 墓碑，`tasks.updated_at` 字段已预留；
4. **语音输入**：whisper.cpp 本地模型接入 `ai.parse`；
5. **桌面便签**：第二 WebView2 小窗复用同一数据桥。
