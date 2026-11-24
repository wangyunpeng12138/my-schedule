# 开发规范（Coding Standards）

> my-schedule · v1.0 · 2026-11-23
> 适用于本项目全部 Go 与前端代码。评审与自检均以本文档为准。

## 1. 语言与工具链

| 项 | 约定 |
| --- | --- |
| Go | 1.26（`go.mod` 锁定），仅标准库 + 白名单依赖 |
| 依赖白名单 | `jchv/go-webview2`（窗口）· `modernc.org/sqlite`（存储，纯 Go 无 CGO）· `6tail/lunar-go`（农历）· `zalando/go-keyring`（凭据） |
| 前端 | 无构建步骤：原生 HTML/CSS/JS，ES2020，无框架、无 npm 依赖 |
| 格式化 | `gofmt`（Go）；前端 2 空格缩进 |
| 静态检查 | 提交前 `go vet ./...` 必须 0 告警 |

## 2. Go 代码规范

### 2.1 包组织（可扩展、模块化）

```
cmd/myschedule/     # 入口：只做装配，不写业务
internal/
  model/            # 纯数据类型（Task、Settings…），零依赖
  dates/            # 日期纯函数（可测试，无 IO）
  storage/          # SQLite WAL 持久化，唯一 SQL 所在地
  plans/            # 计划领域：校验、CRUD、重复、系列
  holidays/         # 农历 + 中国法定节假日 + 美国联邦假日
  scheduler/        # 提醒调度：单定时器，无轮询
  registry/         # 命令注册表：module.method → handler
  ai/               # OpenAI 兼容客户端（纯 HTTP，无 SDK）
  app/              # 装配层：桥接 webview ↔ registry
web/                # 前端资源，go:embed 打包进二进制
```

**分层依赖方向**：`model` ← `dates` ← `plans/holidays/scheduler` ← `storage` ← `app`。
禁止反向依赖；禁止跨层直连（如 `plans` 不得 import `app`）。

### 2.2 命名与注释

- 包名小写单词；导出符号必须有中文或英文一句注释说明「做什么」。
- 每个文件顶部不写包注释（只有 `doc.go` 或主文件写）。
- 错误信息小写开头、不以标点结尾（`errors.New("invalid date")`），面向用户的文案由前端 i18n 层翻译。
- 模块对外暴露的错误统一为 `*ApiError{Code, Message}`，`Code` 取值见 `docs/module-api.md`。

### 2.3 并发与资源（低占用硬性要求）

- **零轮询**：后台逻辑一律用定时器/事件驱动，禁止 `for { sleep }` 空转扫描。
- 单定时器上限：scheduler 主循环 `time.Timer`，重设上限 10 分钟（防睡眠/改时钟）。
- 所有 `Close/Stop/Destroy` 必须幂等；goroutine 必须可退出。
- SQLite 连接数 = 1，`journal_mode=WAL`，`busy_timeout=5s`；写入用事务批量。

### 2.4 错误处理

- 可预期错误返回 `error`，不用 panic；panic 仅限程序性不变量被破坏。
- `if err != nil` 立即处理，禁止 `_ = err`（除非注释说明为何安全忽略）。

### 2.5 测试

- 表驱动测试优先；时间相关测试注入 `now`，禁止依赖真实当前时间。
- 测试产生的临时文件一律写 `t.TempDir()` 或项目 `temp/`。

## 3. 前端代码规范

- `web/app.js` 单文件渲染层；`web/i18n.js` 文案字典（中文为源语言，键即中文原文，同橙子模式）。
- 不引入任何外部 CDN/字体/图标资源——离线可用是硬性约束；图标用内联 SVG path 常量。
- DOM 渲染统一 `render()` 入口 + `esc()` HTML 转义，禁止拼接未转义的用户输入。
- 与 Go 的所有通信走 `native('module.method', params)`（返回 Promise），禁止新增旁路通信。

## 4. 安全规范

- 用户数据只落 `%AppData%\MySchedule`；不上传任何数据。
- AI/账号凭据经 `go-keyring`（Windows 凭据管理器）存储，禁止明文入库。
- CSP 头限制 `default-src 'self'`；AI 请求仅允许用户显式配置的 HTTPS 端点。

## 5. 性能预算（评审不通过项）

| 指标 | 预算 |
| --- | --- |
| 空闲内存 | ≤ 60 MB（WebView2 进程组合计） |
| 空闲 CPU | ≈ 0%（无轮询） |
| 冷启动（1 万条内） | < 1 s |
| 首帧渲染 | < 300 ms（本地资源，无网络） |

## 6. 提交评审清单（PR / 自检）

1. `gofmt` 无 diff、`go vet` 零告警、`go test ./...` 全绿；
2. 新增依赖是否在白名单内？否则须评审会决议；
3. 是否破坏 `module-api` 已登记接口（须标注 `api-change`）；
4. 中间文件是否进 `temp/`，未混入提交；
5. 超出性能预算的改动须附测量数据。
