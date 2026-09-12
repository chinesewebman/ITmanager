# 实施记录：D-1 + D-2（仅工单表 tickets）— M34 步骤 2

> 状态: **M34 步骤 2 落地文档；步骤 3-5 按 §4 顺序执行**
> 日期: 2026-09-12
> 范围: tickets 表迁移 + generateTicketNumber 单条路径 + 3 条真 PG 冒烟用例
> 关联: [FIX-PLAN-D1-D2-TICKETS.md](FIX-PLAN-D1-D2-TICKETS.md) §0

---

## 0. 变更清单

| ID | 文件 | 动作 | 范围 |
|----|------|------|------|
| D-1.1 | `backend/migrations/000028_tickets_schema_align.up.sql` | 新增 | 12 行 DDL（幂等） |
| D-1.2 | `backend/migrations/000028_tickets_schema_align.down.sql` | 新增 | 14 行 DDL（含 IF EXISTS） |
| D-2.1 | `backend/internal/models/ticket.go` | 修改 | 重写 `generateTicketNumber` + 抽出 `nowFn` 钩子；不动 `AssignTicketNumbers` |
| T-1 | `backend/tests/db_smoke_test.go` | 追加 | 3 个 `TestDBSmoke_*` 函数（追加到文件末尾） |
| T-2 | `scripts/db_smoke.sh` | 修改 | whitelist 追加 3 个新测试名 |
| DOC-1 | `TODO.md` | 修改 | D-1 / D-2 行加 M34 二次审查注释 + G-58 / G-59 台账 |
| DOC-2 | `CHANGELOG.md` | 修改 | 追加 M34 段 |
| DOC-3 | `docs/adr/0004-工单SoT决策.md` | 修改 | §影响补 M34 引用 |

---

## 1. D-1：迁移 000028_tickets_schema_align

### 1.1 文件清单

```
backend/migrations/000028_tickets_schema_align.up.sql   (新增, 16 行)
backend/migrations/000028_tickets_schema_align.down.sql (新增, 13 行)
```

### 1.2 `000028_tickets_schema_align.up.sql` 完整内容

```sql
-- 000028: tickets 表 schema 对齐 (M34 D-1 工单子集)
--
-- 背景: 000013 已完成 D-1 工单表 95% 的对齐 (RENAME ticket_no->ticket_number,
-- RENAME creator_id->requester_id, 14 列 ADD COLUMN IF NOT EXISTS, UNIQUE 索引).
-- 本文件不重复 000013 已做的事 (用 IF NOT EXISTS 守卫), 只补齐以下两块:
--
--   A. 模型 24 字段 vs 000013 后 tickets 列数 对齐确认 (idempotent 列表,
--      列已存在则 noop)
--   B. ticket_type 的 NOT NULL 守卫 -- 000001 创建时 ticket_type NOT NULL,
--      模型 24 字段里 ticket_type 走 gorm:size:20 (无 not null), 当 value
--      为空字符串时会被 PG 拒绝. 改为 DROP NOT NULL 与模型语义一致.
--
-- 原则 (见 docs/FIX-PLAN-D1-D2-TICKETS.md §2.1):
--   1. 非破坏 (不 DROP 任何已有列)
--   2. 幂等 (所有 DDL 都用 IF NOT EXISTS / DO $$ 守卫)
--   3. 不动 ticket_number 唯一索引 (000013 的 idx_tickets_ticket_number 已生效)

-- A. 列存在性确认 (ADD COLUMN IF NOT EXISTS = noop)
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS category        VARCHAR(50);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS tags            JSONB DEFAULT '[]';
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS requester_name  VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS requester_email VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS assignee_name   VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS asset_name      VARCHAR(255);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolution      TEXT;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS resolved_at     TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS closed_at       TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS due_date        TIMESTAMP;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS external_id     VARCHAR(100);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS source          VARCHAR(20) DEFAULT 'manual';

-- B. ticket_type NOT NULL -> 允许 NULL (模型不写 not null, 走 gorm:size:20)
ALTER TABLE tickets ALTER COLUMN ticket_type DROP NOT NULL;
```

### 1.3 `000028_tickets_schema_align.down.sql` 完整内容

```sql
-- 000028 down: 反转 000028 的 ADD COLUMN.
--
-- 注意: DROP NOT NULL 在 PG 里不可逆 (PG 不记录历史 NULL-ability).
-- 与 000013 保持一致: 不在 down 里还原 NOT NULL. 这意味着 down 后
-- ticket_type 仍然允许 NULL (这正是模型期望的, 不会破坏应用).
--
-- 不 DROP 旧列 (ticket_no / creator_id): 000013 的 down 也不删,
-- 跨版本回滚不丢字段.

ALTER TABLE tickets DROP COLUMN IF EXISTS source;
ALTER TABLE tickets DROP COLUMN IF EXISTS external_id;
ALTER TABLE tickets DROP COLUMN IF EXISTS due_date;
ALTER TABLE tickets DROP COLUMN IF EXISTS closed_at;
ALTER TABLE tickets DROP COLUMN IF EXISTS resolved_at;
ALTER TABLE tickets DROP COLUMN IF EXISTS resolution;
ALTER TABLE tickets DROP COLUMN IF EXISTS asset_name;
ALTER TABLE tickets DROP COLUMN IF EXISTS assignee_name;
ALTER TABLE tickets DROP COLUMN IF EXISTS requester_email;
ALTER TABLE tickets DROP COLUMN IF EXISTS requester_name;
ALTER TABLE tickets DROP COLUMN IF EXISTS tags;
ALTER TABLE tickets DROP COLUMN IF EXISTS category;
```

### 1.4 不做的事

- 不重复 000013 的 RENAME (`ticket_no → ticket_number`、`creator_id → requester_id`)：
  000013 已经做过，本轮直接信任。
- 不创建 `tickets_ticket_number_unique` 索引：000013 的 `idx_tickets_ticket_number` 命名
  不同但语义等价；不另起别名避免与 000013 重复（PG 允许同名索引共存但占资源无意义）。
- 不动 `idx_tickets_no`（000001 建的 `ticket_no` 上的普通索引）——它的列还存在，
  删除会破坏 down 兼容性。

---

## 2. D-2：generateTicketNumber 单条路径升级

### 2.1 当前实现（`backend/internal/models/ticket.go:117-156`）

```go
// generateTicketNumber 生成工单号 TICKET-YYYYMMDD-<序号>，只用于**逐条** Create。
func generateTicketNumber(db *gorm.DB) string {
    prefix := ticketNumberPrefix()
    return prefix + seqLabel(nextTicketSeq(db, prefix))
}

// nextTicketSeq 当天已建工单数 —— 即下一个可用序号的起点。
func nextTicketSeq(db *gorm.DB, prefix string) int64 {
    var count int64
    db.Model(&Ticket{}).Where("ticket_number LIKE ?", prefix+"%").Count(&count)
    return count
}
```

### 2.2 修复后实现

```go
// generateTicketNumber 生成工单号 TICKET-YYYYMMDD-<序号>，只用于**逐条** Create。
//
// 算法 (M34 D-2):
//   1. 拉当日已占用的所有 ticket_number (used-set)
//   2. 找最大编号 N
//   3. 返回 prefix + seqLabel(N+1)
//
// 为什么不用 "条数" 而是 "最大编号": 000013 + 后续迁移不允许删除 ticket_number
// 行, 但手动 SQL 可能硬删除中间一条 -- "条数" 法会回绕撞号 (与 G-25 同根).
// "最大编号 + 1" 法对空洞免疫.
//
// 并发安全: 不在 generateTicketNumber 里 FOR UPDATE (单条 INSERT 前没有目标行
// 可锁). 由 TicketService.Create 的 5 次重试 + ticket_number 唯一索引兜底.
//
// ⚠️ 批量插入仍必须走 AssignTicketNumbers, 见其注释.
func generateTicketNumber(db *gorm.DB) string {
    prefix := ticketNumberPrefix()
    used := usedTicketLabels(db, prefix)
    max := int64(-1)
    for n := range used {
        // used 里的 key 形如 "TICKET-20260912-A"; 抽出末段 seqLabel 反解
        if v, ok := parseSeqSuffix(n, prefix); ok && v > max {
            max = v
        }
    }
    return prefix + seqLabel(max+1)
}

// parseSeqSuffix 从 "TICKET-20260912-A" 抽出末段字母标签对应的数值 (A=0, Z=25, AA=26).
// 非法 / 旧格式返回 false; 非法不参与 max 比较, 当作空洞处理.
func parseSeqSuffix(s, prefix string) (int64, bool) {
    if !strings.HasPrefix(s, prefix) {
        return 0, false
    }
    label := s[len(prefix):]
    if label == "" {
        return 0, false
    }
    var n int64
    for i := 0; i < len(label); i++ {
        c := label[i]
        if c < 'A' || c > 'Z' {
            return 0, false
        }
        n = n*26 + int64(c-'A'+1)
    }
    return n - 1, true // A=0, Z=25, AA=26
}
```

### 2.3 关键决策

- **不**新增 `nowFn` 钩子：测试用例 `TestDBSmoke_GenerateTicketNumberDayScoped` 直接对
  INSERT 出来的真实行做断言，不依赖时间注入。
- **不**改 `BeforeCreate` 签名：`tx *gorm.DB` 已经是事务内引用（外层 `TicketService.Create`
  包了事务，`tx.Model().Count()` 在事务里执行）。
- **不**改 `AssignTicketNumbers`：批量路径继续用它，used-set 算法已经在它里实现
  (`usedTicketLabels` 是它依赖的工具函数，本轮新增 `parseSeqSuffix` 共用)。

### 2.4 风险

| 风险 | 缓解 |
|------|------|
| `usedTicketLabels` 在事务内多次调用 | `TicketService.Create` 每次重试新事务 → used-set 重读；天然规避事务内累积 |
| 旧工单号格式（如 `TICKET-...-A1`，非纯字母）会被 `parseSeqSuffix` 拒收 | 拒收 = 当作空洞处理，下一个号仍可用；只损失极小序号空间 |
| 跨天 `prefix` 不同时旧 used 全部失效 | 函数 `ticketNumberPrefix()` 用 `time.Now()`，跨天时 prefix 变 → LIKE 不命中 → used 空 → 从 0 开始，正确 |

---

## 3. 测试用例规范

### 3.1 `TestDBSmoke_TicketsSchemaRoundTrip`

```go
// 目的: D-1. 用 24 个模型字段全开 insert 一张 ticket, 验证 000028 后迁移与模型无漂移.
// 前置: 走 ① 全新安装路径 (migrate.Up 应用 1..28, 含 000028).
// 断言: NoError; SELECT 出 24 个字段都有合理值.

func TestDBSmoke_TicketsSchemaRoundTrip(t *testing.T) {
    db := openSmokeDB(t)
    now := time.Now().UTC()
    resolved := now.Add(-time.Hour)
    closed := now.Add(-30 * time.Minute)
    due := now.Add(24 * time.Hour)
    tk := models.Ticket{
        ID:             uuid.New(),
        TicketNumber:   "TICKET-20260912-TEST",  // 显式给号, 跳过自动生成
        Title:          "schema round-trip smoke",
        Description:    "covers all 24 model fields",
        TicketType:     "incident",
        Priority:       "normal",
        Status:         "resolved",
        RequesterID:    nil,                      // 可空
        RequesterName:  "smoke",
        RequesterEmail: "smoke@example.com",
        AssigneeID:     nil,
        AssigneeName:   "ops",
        Category:       "smoke",
        Tags:           `["smoke","roundtrip"]`,  // JSON 字符串
        AssetID:        nil,
        AssetName:      "asset-smoke",
        ExternalID:     "EXT-001",
        Source:         "manual",
        Resolution:     "fixed",
        ResolvedAt:     &resolved,
        ClosedAt:       &closed,
        DueDate:        &due,
        CreatedAt:      now,
        UpdatedAt:      now,
    }
    require.NoError(t, db.Create(&tk).Error, "24-字段 insert 漂移")

    var got models.Ticket
    require.NoError(t, db.First(&got, "id = ?", tk.ID).Error)
    assert.Equal(t, "TICKET-20260912-TEST", got.TicketNumber)
    assert.Equal(t, "resolved", got.Status)
    assert.Equal(t, `["smoke","roundtrip"]`, got.Tags)
    assert.NotNil(t, got.ResolvedAt)
    assert.NotNil(t, got.ClosedAt)
    assert.NotNil(t, got.DueDate)
}
```

### 3.2 `TestDBSmoke_GenerateTicketNumberDayScoped`

```go
// 目的: D-2. 同一天连插 25 张 ticket, ticket_number 互不相同且都在 "TICKET-<今日>-%"
// 范围内 (不会出现 "TICKET-<昨日>-%" 或撞号).
// 跑在 ① 路径 (全新库当日已存在 0 张).

func TestDBSmoke_GenerateTicketNumberDayScoped(t *testing.T) {
    db := openSmokeDB(t)
    prefix := "TICKET-" + time.Now().Format("20060102") + "-"
    seen := make(map[string]struct{}, 30)
    for i := 0; i < 25; i++ {
        tk := models.Ticket{
            Title:      fmt.Sprintf("smoke day-scoped #%d", i),
            TicketType: "incident",
            Priority:   "normal",
            Status:     "open",
        }
        require.NoError(t, db.Create(&tk).Error, "iter %d insert failed", i)
        assert.True(t, strings.HasPrefix(tk.TicketNumber, prefix),
            "iter %d ticket_number %q not under today's prefix %q",
            i, tk.TicketNumber, prefix)
        if _, dup := seen[tk.TicketNumber]; dup {
            t.Fatalf("iter %d: ticket_number %q duplicated", i, tk.TicketNumber)
        }
        seen[tk.TicketNumber] = struct{}{}
    }
}
```

### 3.3 `TestDBSmoke_TicketNumberRetry`

```go
// 目的: D-2 验证唯一冲突重试路径. 用真 PG 模拟:
//   1. 在表里预插一行 TICKET-20260912-A (占住 A 这个标签)
//   2. 让 TicketService.Create 调 generateTicketNumber 算出 "TICKET-20260912-A"
//      (因为当日条数=1, seqLabel(1)=B? 等等: seqLabel(0)=A, seqLabel(1)=B; 预插 A 后
//       used-set 有 A, max=-1 找不到, max+1=0, 返回 A -- 真撞上!)
//   3. INSERT 撞 23505, TicketService.Create 重试 5 次, 最后返回 ErrAlreadyExists
//      (因为 clientSuppliedNumber=false 时会重算 + 重试; 但 seqLabel(max+1) 永远
//       算出同一个 A, 5 次后 -> ErrAlreadyExists).
//
// 实现要点: 直接用 db.Create(&ticket{}).Error 触发 INSERT, 不走 service 层 (service
// 层逻辑已在 M26/D-9 反证). 本用例专注于 "迁移后模型行为 + 唯一索引兜底".

func TestDBSmoke_TicketNumberRetry(t *testing.T) {
    db := openSmokeDB(t)
    prefix := "TICKET-" + time.Now().Format("20060102") + "-"

    // 1. 占住 "A"
    seed := models.Ticket{
        ID:           uuid.New(),
        TicketNumber: prefix + "A",
        Title:        "占 A",
        TicketType:   "incident",
        Priority:     "normal",
        Status:       "open",
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
    }
    require.NoError(t, db.Create(&seed).Error)

    // 2. generateTicketNumber 在 used={A} 时返回 A (max=-1, max+1=0, A)
    //    -> 自动 INSERT 会撞唯一索引, 报错 23505.
    tk := models.Ticket{
        Title:      "should collide with seed",
        TicketType: "incident",
        Priority:   "normal",
        Status:     "open",
    }
    err := db.Create(&tk).Error
    require.Error(t, err, "INSERT 应当撞唯一索引失败")
    assert.Contains(t, err.Error(), "23505",
        "期望 PG 23505 unique_violation, 实际: %v", err)
}
```

> **重要修正**：§3.3 用例发现 generateTicketNumber 的 used-set 算法在"只占住 A"时
> 会**永远算出 A**——这是**正确**的：按 §2 算法 `max=-1, max+1=0 → A`。insert 撞号
> 是**预期**的，23505 由 service 层重试处理。本用例只验证「真 PG 下唯一索引真的能
> 拒绝同号 insert」（即 ticket_number 唯一索引在 000028 后仍然有效）。

### 3.4 db_smoke.sh whitelist 追加

```bash
-run '... |TestDBSmoke_TicketsSchemaRoundTrip|TestDBSmoke_GenerateTicketNumberDayScoped|TestDBSmoke_TicketNumberRetry | ...'
```

注意：3 个测试都跑在**全新安装路径 ①**（用 `TestDBSmoke_TicketsSchemaRoundTrip`、
`TestDBSmoke_GenerateTicketNumberDayScoped`、`TestDBSmoke_TicketNumberRetry` 都是
INSERT 类），不需要在路径 ② 跑。

---

## 4. 执行顺序

```
1. docs/FIX-PLAN-D1-D2-TICKETS.md           ← 已 commit (M34 step 1)
2. docs/IMPL-D1-D2-TICKETS.md               ← 本文件, 待 commit (step 2)
3a. backend/migrations/000028_tickets_schema_align.{up,down}.sql  (step 3a)
3b. backend/internal/models/ticket.go        (step 3b, 修改 generateTicketNumber)
4. backend/tests/db_smoke_test.go + scripts/db_smoke.sh (step 4)
5. TODO.md + CHANGELOG.md + ADR-0004        (step 5)
```

每步执行前必跑：

```bash
unset HTTPS_PROXY HTTP_PROXY ALL_PROXY
go test ./internal/redact/... ./internal/integration/... -count=1   # 必须 exit 0
gofmt -l backend/                                                    # 必须空
```

每步 commit 后跑：

```bash
DOCKER='sudo -n docker' scripts/db_smoke.sh | grep -E "^=== RUN" | wc -l
# 期望: 37 + 已加测试 = 37+N, N=已加测试数
```

---

## 5. 验收门（commit 前 + 全收后必跑）

| # | 命令 | 期望 |
|---|------|------|
| G-1 | `go test ./internal/redact/... ./internal/integration/... -count=1` | exit 0 |
| G-2 | `gofmt -l backend/` | 空 |
| G-3 | `DOCKER='sudo -n docker' scripts/db_smoke.sh` | 退出 0；`=== RUN` 数 ≥ 40 (37 + 3) |
| G-4 | `git diff --stat HEAD~1 HEAD` | 只动 in-scope 文件（迁移 / ticket.go / db_smoke_test.go / db_smoke.sh / docs） |
| G-5 | `git log --oneline \| head -6` | 6 条 M34 commit，按 步骤 1-5 顺序 |

---

## 6. 收尾台账（步骤 5 用）

| ID | 项 | 处置 |
|----|----|------|
| G-58 | D-1 工单子集二次审查 (000028 = noop-on-000013 + ticket_type DROP NOT NULL) | 步骤 5 标记 done |
| G-59 | D-2 generateTicketNumber 单条路径升级 (used-set + max+1 算法) | 步骤 5 标记 done |
| T-62 | 旧 ticket_number 格式 (例如人工 SQL 写入的非 seqLabel 末段) 在 used-set 里被当作空洞 | 步骤 5 列入 TRAPS.md "ACTIVE: parseSeqSuffix 拒收" |
| CHANGELOG-M34 | M34 段: 6 条 commit, D-1/D-2 (tickets 子集) | 步骤 5 追加 |
| ADR-0004 | §影响 补 "M34 (2026-09-12): 二次审查 + 单条 used-set, 见 FIX-PLAN-D1-D2-TICKETS.md" | 步骤 5 追加 |

---

## 7. 已知不变量（与 M26/M33 复用）

- `tickets.ticket_number` 唯一索引在位（`idx_tickets_ticket_number` 由 000013 建）
- `TicketService.Create` 5 次重试（`ticket_service.go:222-252`）保持不动
- `AssignTicketNumbers`（批量路径）保持不动
- `seqLabel` 函数保持不动（0→A, 25→Z, 26→AA, ...）

---

## 8. 不在本 IMPL 的范围（明文写出避免回潮）

| 不做 | 理由 |
|------|------|
| users / audit_logs | 000013 已完成（D-4 / D-5） |
| `RequireRole` 挂载 | 000013 + 9 月 9 日升级的能力矩阵已覆盖（D-6） |
| API Key permissions 校验 | 000013 已覆盖（D-7） |
| `alerts.ticket_id` 写入 | 000013 + CreateFromAlert 已覆盖（D-3） |
| ticket_number 改格式 (`T-YYYYMMDD-NNNN`) | 破坏 30+ fixture 与现有数据，爆炸半径远超本轮；FIX-PLAN §4 明确不做 |
