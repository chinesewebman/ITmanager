# ITmanager — Trap 集中清单

> **维护**: 2026-07-01 B1-4 创建 (v2.3)
> **范围**: 项目从 v1.0 → v2.3 累积踩过的实战陷阱
> **使用**: 给接手人 / 未来自己 debug 时一眼对照
> **来源**: `~/.hermes/skills/software-development/itmanager-feature-impl/references/*.md` + skill SKILL.md "Top traps"
>
> **状态约定**:
> - **ACTIVE** — 当前仍然存在,改动时必须检查
> - **FIXED** — 已修复, 但容易回退/复发,值得知道避免再踩
> - **HISTORICAL** — 历史 trap,描述过时,新代码不会再撞,仅供考古

---

## 一、Go 后端陷阱

### T-1. gorm `Create` 默认产生 `INSERT ... RETURNING "id"` (PG)
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: sqlmock 报 "call to Query was not expected, next expectation is ExpectedExec"。
**解法**: 用 `ExpectQuery` + `WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))`, 不是 ExpectExec。
**陷阱**: `PreferSimpleProtocol: true` 时相反 — 用 ExpectExec + 事务 Begin/Commit (Trap 13)。

### T-2. gorm Create 默认包事务 → 测试要 Begin
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: 单条 Insert 报 "call to database transaction Begin was not expected"。
**解法**: 中间件用 `Session(&gorm.Session{SkipDefaultTransaction: true})`, 或测试 ExpectBegin+Exec+Commit 三件套。

### T-3. gorm `First(id)` 实际发 2 个 bind arg
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: `WithArgs(id)` 报 "expected 2 args, got 1"。
**根因**: gorm 隐式追加 `ORDER BY id LIMIT 1`, SQL 末尾是 `$1, $2`, args = [id, 1]。
**解法**: `WithArgs(id, 1)` 或 `WithArgs(id, sqlmock.AnyArg())`。

### T-4. gorm `Offset(0).Limit(N)` 不发 offset bind
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: page=1 测试 OK, page=2 测试报 arg 数不对。
**根因**: `Offset(0)` 被 elide, SQL 是 `LIMIT $1` (1 arg); `Offset(2)` 是 `LIMIT $2 OFFSET $1` (2 args)。
**解法**: 默认 page 路径测 1 arg, 非默认 page 测 2 args。

### T-5. gorm `IN ?` clause 1 个 arg (slice)
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: `db.Where("id IN ?", []string{...})` 报 "expected N args, got 1"。
**解法**: `WithArgs(sqlmock.AnyArg())`, 不要展开 slice。

### T-6. gorm model `column:` tag 缺失 = 静默数据丢失
**状态**: ACTIVE | **类别**: 生产 bug,数据完整性
**现象**: 字段永远 nil,无报错。`IPv6Address *string` 无 `gorm:"column:ipv_address"` tag → 读 `ipv_address` 列 = nil forever。
**根因**: gorm v2 不验证列存在性,字段缺 tag 用 snake_case 推断,但你的 schema 可能不同名。
**解法**: **永远审计 model tag vs DB schema**, 加 sqlmock row 测试时**先把列名列对**。

### T-7. AutoMigrate 是 per-model 查询,不是单事务
**状态**: ACTIVE | **类别**: sqlmock 测试
**现象**: 期望 Begin/Exec/Commit 失败,gorm 跑 N 个独立 query (SELECT count + CREATE TABLE + N×CREATE INDEX), 顺序随机。
**解法**: `sqlmock.New(sqlmock.QueryMatcherOption(...))` + `MatchExpectationsInOrder(false)` + ~50 个 generic expectation for 12 model。

### T-8. closure 捕获 New() 改的 config → nil panic
**状态**: ACTIVE | **类别**: production panic
**现象**: `New(cfg)` 内改 `cfg.KeyFunc`, 返回的 closure 还捕获原 cfg → 请求来时 keyFn() = nil deref。
**解法**: closure 捕获前先把需要的东西拷到 local var: `keyFn := rl.cfg.KeyFunc; return func(c) { key := keyFn(c) }`。

### T-9. pre-commit gofmt hook 假阳性
**状态**: ACTIVE | **类别**: hook
**现象**: 你 `go fmt` 过了, commit 还是报 "Go files need formatting"。
**根因 1**: hook 跑 `go fmt ./...`, 改了你没碰的旧文件 (Go 1.25 更激进 import 排序)。
**根因 2**: hook 有自己的 gofmt cache。
**解法**:
- (a) `gofmt -l .` 空=真干净 → 用 `git commit --no-verify` 一次
- (b) **推荐**: 先 commit `chore(go): re-format` 把 gofmt 改的无关文件清掉, 再 commit feat
- 检测: `git status --short` 看 ` M ` (空格+M = unstaged 但 gofmt 动了)

### T-10. git add 是叠加,不是替换
**状态**: ACTIVE | **类别**: commit hygiene
**现象**: 想做小而专的 chore commit,结果 diff stat 14 files / +616 行 — 上次 staged 的 feat 文件还在 index 里。
**解法**:
- (a) `git reset --soft HEAD~1` + `git reset HEAD <要排除的>` 拆开重做
- (b) `git commit --only <paths>` (前提: paths 已在 index)
- (c) **最稳**: commit 前必跑 `git status --short` 看 staged 区 `M ` 标记

### T-11. service 层 trigger 阻塞主流程
**状态**: ACTIVE | **类别**: concurrency
**现象**: status 变更触发通知/审计 → race,主流程等通知完成慢。
**解法**: trigger helper, fail-only-log, fire-and-forget。看 audit-batch-2026-06-17 §10。

### T-12. 外部 3rd-party 集成 token 永不过期
**状态**: ACTIVE | **类别**: Zabbix/NetBox/Jira
**现象**: session token 失效不检测,调接口一直 401。
**解法**: 3-layer: expiresAt 跟踪 + active re-login 检测 + -10002 auto-retry。audit-batch-2026-06-17 §11。

### T-13. Worker 复用 IntegrationService,不要自己 New client
**状态**: ACTIVE | **类别**: cron worker
**现象**: worker 自己 `NewZabbixClient(cfg, nil)`, UI Reload URL 后 worker 仍用旧 URL 静默漂移;auth cache 分裂;Prometheus label 重复计数。
**解法**: worker 构造函数收 `*IntegrationService`, 内部用 `w.svc.zabbix`。见 v2.3-cron-worker-pattern.md §2-3。

### T-14. Worker Stop() 必须查 `started` + `stopped` 双 bool
**状态**: ACTIVE | **类别**: close-channel panic
**现象**: 单 `started` bool → 二次 Stop 仍过检查 → `close(w.stop)` → `panic: close of closed channel`。
**解法**: 加 `stopped bool` 字段, `if !w.started || w.stopped` 命中抢先 return。
**影响范围**: `MetricSyncWorker.Stop`, `notification.Worker.Stop` (待修)。

### T-15. newMockDB 必须显式 `r.Use(gin.Recovery())`
**状态**: ACTIVE | **类别**: handler test
**现象**: svc=nil 触发的 panic 穿透 testing.tRunner,整个 test 进程 panic 而不是 fail。
**解法**: test router factory `gin.New()` 之后立即 `r.Use(gin.Recovery())`。

---

## 二、前端陷阱

### T-16. Settings.tsx 死表单 (B1-1/B1-2 修复中)
**状态**: PARTIAL FIX | **修复**: B1-1 (API 密钥), B1-2 (通知渠道)
**现象**: 表单/按钮渲染了但没接 API,看起来能保存其实啥也不发生。
**根因**: 早期迭代时只画了 UI,后端 API 跟前端没同步。
**检测方法**: 全 Settings 集成卡扫一遍 — `<Input>`/`<Button>` 找 `onClick`/`onFinish` 是否实接 API;`<Form.Item name>` 是否真的 `name` 到 state。
**推广**: 任何新加 Settings 集成页必须有 Save/Test/Sync 三按钮实接 API。

### T-17. CI 缺前端测试 (B1-3 已修)
**状态**: FIXED | **修复 commit**: 3725f40
**现象**: 前端 push 后只看 build, vitest/tsc 跑不跑无兜底。
**解法**: CI 加 `npx tsc --noEmit` + `npx vitest run`。

### T-18. Modal Save 接 API 必须 stringify/parse config 嵌套对象
**状态**: ACTIVE | **类别**: 后端 model JSON 字段
**现象**: 后端 `NotificationChannel.Config` 是 `string` (JSON-serialized),前端 form 是 nested object → 直接发会塞错位置。
**解法**: save 时 `JSON.stringify(configObj)`, edit 时 parse 回 nested object (try/catch 兜底)。

### T-19. ESLint `--report-unused-disable-directives` 严抓 disable 注释
**状态**: ACTIVE | **类别**: lint
**现象**: 你加 `// eslint-disable-next-line`, 后来代码改完 disable 不再需要 → 报 "Unused eslint-disable directive"。
**解法**: 加 disable 前想清楚,改完代码后检查 disable 是否多余。

### T-20. Antd v5 Modal `transitionName=""` + waitFor timeout
**状态**: ACTIVE | **类别**: vitest
**现象**: 测试断言 `expect(input).not.toBeInTheDocument()`, Modal 关闭后 DOM 还在 (jsdom transition 不 fire onTransitionEnd)。
**解法**: `transitionName=""` 禁用动画 + `await waitFor(() => expect(...).not.toBeInTheDocument(), { timeout: 3000 })`。

### T-21. tsc baseline 24 errors = 不要新增
**状态**: ACTIVE | **类别**: regression 检测
**现象**: 项目 tsc --noEmit 历史 24 errors, 新代码提交又想"我代码没问题" — 但 baseline 不许涨。
**解法**: 改前先 `git stash -u` 跑一遍记 baseline,改完对比。
**新 test**: 用 `.toBeTruthy()` 替代 `.toBeInTheDocument()` 避免贡献 baseline。

---

## 三、跨模块陷阱

### T-22. 新 endpoint 漏 1 处 = 编译/404/nil panic
**状态**: ACTIVE | **类别**: 新 feature
**现象**: 加 endpoint 只改 service 忘 route,或只改 route 忘 handler,或忘 openapi.yaml sync。
**解法**: **4 件套检查清单** — service 构造 + handler 构造 + route 注册 + handler 实现, 每次加 endpoint 必查 4 项。
**openapi.yaml**: 项目手维护 48K,新 path + schema fields 必同步。

### T-23. mockXxxService 编译失败 = 接口加方法忘 mock field
**状态**: ACTIVE | **类别**: service 测试
**现象**: service 接口加方法,但 hand-rolled mock struct 没同步加字段 → 编译错。
**解法**: 接口加方法必同步 mock struct field + mock method。

### T-24. seed test "no column X" 错误
**状态**: ACTIVE | **类别**: migration 同步
**现象**: model 加列, gorm AutoMigrate 不被 seed test 用 (手写 SQL), `cmd/seed/main_test.go` CREATE TABLE 缺列 → 测报错。
**解法**: model 加列必同步 `cmd/seed/main_test.go` 的手写 CREATE TABLE。

### T-25. routes.go duplicate `alerts := ...` (patch residue)
**状态**: ACTIVE | **类别**: patch hygiene
**现象**: patch 加 handler 时复制粘贴 `alerts := ...` 行,留下 duplicate → 编译歧义。
**解法**: patch 后扫一遍 routes.go block scope, 检查重复定义。

### T-26. `t.Context()` 不是 Go 1.23
**状态**: ACTIVE | **类别**: test helper
**现象**: `testing.T.Context()` 是 Go 1.24+,项目用 1.23 → 编译错。
**解法**: 用 `context.Background()`, grep `t\.Context()` 全仓审计。

### T-27. patch `old_string` 多匹配
**状态**: ACTIVE | **类别**: 工具使用
**现象**: `old_string: "}"` 或 `"if err != nil"` 之类短 snippet 命中 8+ matches → patch 拒绝。
**解法**: 包含 2-3 行 surrounding context, 或 `replace_all=true` (有意全改时)。

---

## 四、历史 / 已修陷阱 (供考古)

### H-1. pre-commit hook 改 `cmd/server/main.go` 漏 build
**状态**: HISTORICAL (Trap 16) | **修法**: `cmd/server/main.go` 不再在 .gitignore, 文件已 tracked。
**当前**: `backend/cmd/server/main.go` + `grpc.go` 都正常 tracked, trap 已无意义。改 main.go 后**仍建议**手动 `cd backend && go build ./...` 验证 (CI 现在也会跑)。

### H-2. vet 缓存假阳性
**状态**: HISTORICAL (Trap 2) | **现象**: vet 说 "undefined: X", 实际存在。
**修法**: `go test` 才是 ground truth, 不信 vet 缓存。
**当前**: 偶尔仍发生, 解法不变。

### H-3. `cmd/server/main.go` 被 `.gitignore` 拦截
**状态**: HISTORICAL (Trap 14) | **修法**: Trap 14 修过, 现在 tracked。

### H-4. Trap 21 (v2.2 Zabbix) — handler test router Recovery
**状态**: FIXED | **修法**: handler test helper 已统一 `r.Use(gin.Recovery())`。

---

## 五、Traps 索引 (按 trap 号 → 本文档映射)

| skill trap # | 本文档 | 状态 |
|---|---|---|
| 1 | T-9 | ACTIVE |
| 2 | H-2 (vet 缓存) | HISTORICAL |
| 3 | T-27 | ACTIVE |
| 4 | T-9 (same as 1) | ACTIVE |
| 5 | T-1 | ACTIVE |
| 6 | T-2 | ACTIVE |
| 7 | T-7 | ACTIVE |
| 8 | T-27 (same as 3) | ACTIVE |
| 9 | T-3 | ACTIVE |
| 10 | T-4 | ACTIVE |
| 11 | T-6 | ACTIVE |
| 12 | T-9 (same as 1) | ACTIVE |
| 13 | T-1 (postgres variant) | ACTIVE |
| 14 | H-3 | HISTORICAL |
| 15 | (历史 main.go import) | HISTORICAL |
| 16 | H-1 | HISTORICAL |
| 17 | (handler cursor 解析) | ACTIVE |
| 18 | (next_cursor 触发条件) | ACTIVE |
| 19 | (DI setter race) | ACTIVE |
| 20 | T-22 | ACTIVE |
| 21 | T-15 | ACTIVE |
| 22 | T-9 (Go 1.25 variant) | ACTIVE |
| 23 | T-10 | ACTIVE |
| 24 | T-13 | ACTIVE |
| 25 | T-14 | ACTIVE |

---

## 六、Trap 添加约定

新 trap 必须满足:
1. **真实踩过** (不是理论可能)
2. **有 commit/日期/SHA 可追溯**
3. **有明确"如何检测" + "如何修"**
4. **状态**: ACTIVE / FIXED / HISTORICAL, 不允许"无状态"

加新 trap:
1. 找本文档对应类别章节
2. 加 entry, 给唯一编号 (T-N, 接上一个)
3. 更新 §五 索引
4. commit: `docs(traps): T-N <one-liner>`

---

_文档生成于 B1-4 (v2.3 + nightly batch 自动化)。下次审计后请更新状态列。_