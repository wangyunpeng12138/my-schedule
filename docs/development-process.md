# 开发流程（Development Process）

> my-schedule · v1.0 · 2026-11-23
> 本流程依据框架选型（Go + WebView2，见 `docs/architecture.md` §2）制定，是本项目所有开发活动的执行标准。

## 1. 里程碑划分

| 阶段 | 里程碑 | 交付物 | 验收标准 |
| --- | --- | --- | --- |
| M0 | 规范先行 | 本文档 + `coding-standards.md` + `architecture.md` + `module-api.md` | 评审通过后 git 提交（commit 类别 `docs`） |
| M1 | 内核地基 | `internal/dates`、`internal/model`、`internal/storage`（SQLite WAL） | `go test ./...` 全绿 |
| M2 | 领域模块 | `internal/plans`（CRUD/重复/系列）、`internal/holidays`（农历+中美节假日）、`internal/scheduler` | 单元测试覆盖核心分支（见 §4） |
| M3 | 桥与命令层 | `internal/registry`、`internal/ai`、`internal/app` 桥接 | 命令式接口可通过 `--eval` 冒烟 |
| M4 | 界面复刻 | `web/`（橙子风格页面：四视图、编辑器、设置、提醒弹层） | 手工验收清单（§5）通过 |
| M5 | 集成与交付 | 可执行文件、README、CHANGELOG | `go build` + `go vet` + 全量测试 + 冒烟运行 |

## 2. 迭代节奏

1. **领任务**：从 `docs/architecture.md` 的模块清单中取一个模块作为一个迭代单元。
2. **先接口后实现**：每个模块先在 `docs/module-api.md` 登记命令签名，再写 Go 实现。
3. **先测试后联调**：模块级单元测试先于 UI 联调完成。
4. **小步提交**：每完成一个可验证单元即 git 提交，提交粒度 = 一个模块或一个修复。

## 3. Git 工作流

- 单主干开发（trunk-based），直接在 `main` 上小步提交；规模化后再开 feature 分支。
- **提交信息格式**：`<type>: <摘要>`，type 取值：
  - `feat` 新功能 · `fix` 缺陷修复 · `docs` 文档 · `test` 测试 · `refactor` 重构 · `chore` 构建/工具
- **中间文件纪律**（硬性约束）：
  - 一切临时产物（覆盖率报告、调试输出、日志、试验脚本、构建中间物）放入 `temp/`，该目录已被 `.gitignore` 忽略；
  - 构建产物输出到 `build/`，同样不入库；
  - 严禁把二进制、`*.exe`、依赖目录提交进仓库。
- 里程碑达成时打 tag：`v0.1.0` 等，遵循语义化版本。

## 4. 测试要求

| 层 | 要求 |
| --- | --- |
| 单元测试 | 每个 `internal/*` 模块至少一个 `_test.go`；日期/系列/节假日等纯逻辑分支全覆盖 |
| 集成冒烟 | `myschedule --selftest`：内存库上跑通 plans 增删改查 + 提醒触发 + 导出导入闭环 |
| 手工验收 | 按 §5 清单在 Windows 11 实机走查 |
| 回归 | 提交前 `go vet ./... && go test ./...` 必须全绿 |

## 5. 手工验收清单（M4/M5）

- [ ] 冷启动 < 1s；空闲内存 ≤ 60 MB（任务管理器观察）
- [ ] 月/四周/周/清单 四视图切换正常，翻页、回到今天
- [ ] 日期单元格显示农历（如「廿三」）、中国节假日「休/班」角标、美国联邦假日名称
- [ ] 新建/编辑/完成/删除计划；跨天（endDate ↔ spanDays 联动）
- [ ] 重复计划（每日/工作日/每周 4/8/12 次）
- [ ] 到点提醒：站内弹层 + Windows 通知；完成/稍后 10 分钟/知道了
- [ ] 拖动改期（月视图单元格间）
- [ ] 备份导出/导入（JSON）
- [ ] AI 解析：配置 OpenAI 兼容端点后，文本 → 结构化草稿 → 确认入库

## 6. 变更管理

- 影响 `docs/module-api.md` 已登记接口的变更，须同步更新文档并在提交信息中注明 `api-change`。
- 数据库 schema 变更必须走 `storage.go` 内的版本化迁移，禁止破坏既有用户数据。
