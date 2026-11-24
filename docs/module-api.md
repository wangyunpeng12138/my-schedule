# 模块命令接口（Module API）

> 所有能力收敛为命令式 JSON 接口：`invoke(method, params) → result | {code, message}`。
> 与 MCP `tools/call` 对齐：`registry.ExportMCPTools()` 可直接导出工具定义。
> 通用错误码：`invalid_input`（参数无效）· `not_found`（对象不存在）· `conflict`（并发冲突）· `unknown_method`。

## plans.*

| 方法 | 参数 | 返回 | 说明 |
| --- | --- | --- | --- |
| `plans.list` | `{from?, to?, category?, status?: all\|todo\|done, query?, limit?}` | `{tasks: Task[]}` | 按日期升序 |
| `plans.get` | `{id}` | `Task` | |
| `plans.save` | `{task: Draft, repeat?: 1\|4\|8\|12\|{frequency: daily\|weekdays, until}}` | `{tasks: Task[]}` | 新建/更新统一入口；repeat 生成系列 |
| `plans.toggle` | `{id, done}` | `Task` | |
| `plans.delete` | `{id}` | `{ok: true}` | |
| `plans.move` | `{id, date, time?}` | `Task` | 跨天计划整体平移 |
| `plans.stats` | `{from, to}` | `{total, done, hours, completionRate}` | |
| `plans.due` | `{}` | `{tasks: Task[]}` | 当前应提醒列表 |

**Task**：`{id, title, date, endDate?, time, duration, reminder, category: work|study|life, priority: normal|high, notes, done, seriesId?, seriesIndex?, createdAt, updatedAt}`。
**校验**：title 1–160 字；date `YYYY-MM-DD`；time `HH:MM`；duration 15–1440；reminder ∈ {-1,0,5,10,15,30,60,1440}；endDate ≥ date。

## holidays.*

| 方法 | 参数 | 返回 |
| --- | --- | --- |
| `holidays.lunar` | `{date}` | `{text: "十一月廿三", month, day, yearGanZhi, animal, term?, solarTerm}` |
| `holidays.cn` | `{year}` | `[{date, name, off, work}]`（放假 off=true；补班 work=true） |
| `holidays.us` | `{year}` | `[{date, name}]`（含周末顺延观测规则） |
| `holidays.day` | `{date}` | `{lunar, cn?, us?, weekend}` 一次取全 |

中国节假日数据为**打包数据文件**（2025–2027，源自官方放假安排），按年存 `internal/holidays/data/cn-YYYY.json`，可替换热更。

## settings.* / backup.*

| 方法 | 参数 | 返回 |
| --- | --- | --- |
| `settings.get` | `{}` | `{language, aiEndpoint, aiModel, …}` |
| `settings.set` | `{patch}` | `{settings}` |
| `backup.export` | `{}` | `{json}`（完整备份对象，前端负责落盘） |
| `backup.import` | `{json}` | `{tasks, skipped}`（逐条校验，坏行跳过并计数） |

## ai.*

| 方法 | 参数 | 返回 |
| --- | --- | --- |
| `ai.parse` | `{text}` | `{draft: Draft, raw}`（LLM 输出结构化草稿，前端预览确认后才调 `plans.save`） |

配置（endpoint/model/key）经 `settings.set` 写入；**key 存系统凭据管理器，不落库**。

## app.*

| 方法 | 参数 | 返回 |
| --- | --- | --- |
| `app.info` | `{}` | `{version, dataDir}` |
| `app.testReminder` | `{}` | 触发一条测试提醒（验收用） |
| `app.quit` | `{}` | 退出应用 |

## 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-11-23 | v1 初版（M0 冻结） |
