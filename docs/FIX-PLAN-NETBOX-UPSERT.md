# FIX-PLAN-NETBOX-UPSERT：外部系统同步的 upsert 全链路修复（TODO G-22）

- **状态**：已实现并完成两轮审计（正确性 + 测试有效性）与迭代（单测 + 真 PG dbsmoke + 变异反证 15 项，见 §4）；审查发现的 F-1…F-9 已处置（F-1→G-27 划界、F-8 顺手修、其余已并入）
- **日期**：2026-09-09
- **关联**：TODO G-22（`integration/upsert.go` 用 Go 字段名当列名）；由 G-20 审查连带发现
- **影响面**：NetBox / Zabbix / GLPI **三条同步路径在真 PG 上全部失败**（不只是 G-22 记的那一处）

## 1. 问题（What / Why）

### 1.1 现象

`integration/upsert.go` 的 `buildUpsertClause` 把**调用方传进来的字符串原样**拼进 `ON CONFLICT`：

```go
updates[c] = clause.Expr{SQL: "EXCLUDED." + c}   // c 由调用方给
```

实测（gorm v1.30 DryRun，见 §4 V-0）：

| 调用点 | 生成 SQL（实测） | 真 PG 上的结果（实测，见 §4 V-0） |
|---|---|---|
| `SyncFromNetBox` | `ON CONFLICT (`netbox_id`) DO UPDATE SET `Name`=EXCLUDED.Name, `UpdatedAt`=EXCLUDED.UpdatedAt, …` | `42703 column "netbox_id" does not exist`（HINT: 也许你想引用 `net_box_id`） |
| `SyncFromZabbix` | `ON CONFLICT ("trigger_id") DO UPDATE SET "Status"=EXCLUDED.Status` | `42703 column "Status" of relation "alerts" does not exist` |
| `SyncFromGLPI` | `ON CONFLICT ("external_id") DO UPDATE SET "id"="id"` | `42702 column reference "id" is ambiguous`（**实测**：SET 的歧义在**解析期**就报错，早于仲裁索引检查 —— 所以「缺唯一索引」不是它当时的报错原因；补唯一索引也救不了，见 F-4） |

三类错误**互相独立**，任一处都会让整条语句失败（PG 解析整条 SQL，与是否真的发生冲突无关）。

**F-4（本轮新增的关键事实）**：空 `updateCols` 时 gorm 渲染 `SET "id"="id"`，在 PG 上**不是无害 no-op** —— `ON CONFLICT DO UPDATE` 的 SET 表达式里 `id` 同时存在于目标表与 `excluded`，PG 直接报 `42702 ambiguous`。所以「空更新列 → `DoNothing`」是**修缺陷**，不是风格统一。

**F-6（审查发现，已修）**：原 `SyncFromNetBox` 的预查询写 `Where("netbox_id IN ?", …)`，列名同样是错的（真列名 `net_box_id`）→ 真 PG 上 `42703`，与 upsert 语句是**两处独立的错误**。删掉预查询后该错误随之消失。

**F-7（审查发现，已修）**：更新列原含 `status`，而 `ConvertToAsset` 硬编码 `Status: "active"`（`netbox.go` 不读 NetBox 状态）——写进去是常量、零信息量，却会把本地已退役（`status='retired'`，`retired_*` 不在更新列里）或维护中的资产**静默改回 active**，产出「active + 已退役」的矛盾行。修 G-22 之前整条语句失败、从不发生，是这次修复**解锁**了它 → 更新列**去掉 `status`**（新增行仍带 active），并补上漏掉的 `site_name`（设备换机房后本地必须跟着走）。

### 1.2 根因（三层，逐层挖出来的）

1. **命名层**：`buildUpsertClause` 的注释写「列名」，调用方传的却是 **Go 字段名**（`Name`/`AssetType`/`Status`/`SN`/`UpdatedAt`）；NetBox 的冲突目标 `netbox_id` 也是错的——真实列名是 **`net_box_id`**（gorm 对 `NetBoxID` 的默认蛇形命名，000013 按此建列）。
2. **约束层**：`ON CONFLICT` 要求冲突目标上有 **UNIQUE 约束/索引**。实测三个目标全部**没有**：
   - `assets.net_box_id`：000013 建的是普通索引 `idx_assets_net_box_id`；
   - `alerts.trigger_id`：000013 只 `ADD COLUMN`，无索引；
   - `tickets.external_id`：同上。
   → 即便把列名改对，NetBox/GLPI 仍会 `42P10`。
3. **语义层**：`alerts.trigger_id` **本来就不该唯一**——同一 trigger 会「触发 → 恢复 → 再触发」，`SyncFromZabbix` 自己的预过滤条件就是 `status = 'problem'`（只跳过「当前未恢复」的），说明模型预期同一 trigger 有多行历史。给它加唯一索引会**破坏告警历史**。

### 1.3 为什么单测没抓到

`internal/integration/*_test.go` 里的同步用例只测 HTTP 客户端（httptest mock server + `SyncDevices` 解析），**没有任何一条用例走到 DB 写入**；这三条同步路径的测试里 `database.DB` 从未初始化（同目录 `metric_sync_test.go` 另有 sqlite 库，但只覆盖 metric 路径）。dbsmoke（真 PG）也只覆盖迁移与核心表，不覆盖同步。→ 三条同步路径的 DB 路径**零覆盖**。

**F-5（测试手段的陷阱，实测）**：**sqlite 抓不到「Go 字段名当 SET 列名」这一半**。实测 `ON CONFLICT (net_box_id) DO UPDATE SET "Name"=EXCLUDED.Name` 在 sqlite 上**成功执行**（sqlite 列名解析大小写不敏感，`"Name"` → `name`），而 PG 上 `42703`。但**冲突目标写错**（`netbox_id`）sqlite 会报 `no such column: netbox_id`。
→ 结论：单测必须**同时**做「渲染 SQL 的列名字符串断言」（跨方言确定性地抓 Go 字段名）+「sqlite 功能断言」（抓冲突目标/索引）；真 PG 由 dbsmoke 兜底。

## 2. 方案（How）

### 2.1 候选对比

| # | 方案 | 取舍 |
|---|---|---|
| A | 只改列名字符串（`"name"`、`"net_box_id"`…） | 最小，但 `42P10` 仍在 → 同步依然全挂，等于没修 |
| B | 列名改对 + 给三个冲突目标都加唯一索引 | 能跑通，但 `alerts.trigger_id` 唯一会**吃掉告警历史**（§1.2-3）→ 否决 |
| **C** | **列名改对 + 仅 NetBox 走真 upsert（加唯一索引）+ Zabbix/GLPI 退回普通插入** | ✅ 采用：贴合各自语义，改动面可控 |

### 2.2 定案

1. **helper 契约收紧**（`integration/upsert.go`）：
   - 参数语义明确为 **DB 列名**；
   - 用 `clause.AssignmentColumns(cols)` 生成 `SET "col"="excluded"."col"`（带引号，交给 gorm 渲染，不再手拼 `EXCLUDED.`）；
   - `updateCols` 为空 → 显式 `DoNothing: true`（此前靠 gorm 空 `Set` 兜底渲染成 `SET id=id`，在 PG 上是 `42702 ambiguous`，见 F-4）。
2. **NetBox（唯一需要真 upsert 的路径）**：
   - 冲突目标 `net_box_id`，更新列提成包级变量 `netboxUpdateCols = {name, asset_type, brand, model, sn, site_name, updated_at}`（**单测断言的就是这个变量**，杜绝「测试另抄一份、调用点改了测试照样绿」，见 §4 V-3）：**不含 `status`**（见 F-7）；**不含** `tags`/`custom_fields`（同步里硬编码 `"[]"`/`"{}"`，更新它们会抹掉人工标签）；**不含** `rack_name`（`ConvertToAsset` 不映射机柜，更新等于清空）；
   - 批次内按 `net_box_id` 去重（同一批出现重复 id 时 PG 报 `21000 ON CONFLICT DO UPDATE command cannot affect row a second time` → 整批回滚；§5 无此风险项，见 §4 V-9）；
   - `now := time.Now().UTC()`（与 gorm `NowFunc` 对齐，见 §5 R-6）；
   - 迁移 **000015** 把 `idx_assets_net_box_id` 从普通索引改为 **UNIQUE**（实测 PG 唯一索引允许多个 NULL，手工录入的资产不受影响）；
   - 模型 `Asset.NetBoxID` 标签 `index` → `uniqueIndex`，与迁移对齐（dev/测试走 AutoMigrate 时同样有约束；实测 gorm 生成的索引名恰为 `idx_assets_net_box_id`，与迁移同名 → AutoMigrate 不会重复建）；
   - 删掉 `SyncFromNetBox` 里的「预查询已存在 + 回填 ID」——它存在的唯一理由是「ON CONFLICT 不生效时靠主键插入」，有了仲裁索引后由 `DO UPDATE` 接管（少一次全表 IN 查询）。**实测**：真 PG 上 gorm 对零值 uuid 主键**省略该列**（DB 生成），`CreateInBatches` 混合批次（2 冲突 + 1 新增）经 `ON CONFLICT (net_box_id) DO UPDATE` 后行数正确、冲突行被更新且 **id 未被改写**（§4 V-0）。
3. **Zabbix**：去掉 `ON CONFLICT`，保留「跳过当前未恢复的同 trigger」预过滤 + 普通 `CreateInBatches`（语义：只追加新告警，保留历史）。
4. **GLPI**：去掉 `ON CONFLICT`（原语句 `DO UPDATE SET "id"="id"` 既是等价 no-op，又因 `id` 同时属于目标表与 `excluded` 在**解析期**报 `42702`）；保留「跳过已存在」预过滤。
5. **测试**（§4）：
   - sqlite 单测：① **DryRun 渲染 SQL 的列名断言**（`ON CONFLICT (\`net_box_id\`)` + `SET \`name\`=\`excluded\`.\`name\``，抓 Go 字段名回归，见 F-5）；② 真 `SyncFromNetBox` 走通「插入 → 冲突更新」（抓冲突目标/索引，杜绝测试与生产漂移）；
   - dbsmoke 真 PG：断言 000015 的唯一索引存在 + 真 `ON CONFLICT (net_box_id)` 插入/更新/多 NULL 三态正确（PG 才能抓 `SET "Name"` 那半）；
   - **连带**：`TestDBSmoke_DownPreservesLegacyColumns` 目前固定回滚两次（14 → 13）。新增 000015 后第一次 Down 变成回滚 15，该用例会**静默空转**（它自己的注释就在警告这件事）→ 必须补第三次 Down 并断言 000015 的索引回到非唯一。

## 3. Where（变更清单）

| 文件 | 改动 |
|---|---|
| `backend/internal/integration/upsert.go` | `clause.AssignmentColumns` + 空列表 → `DoNothing`；注释明确「DB 列名」 |
| `backend/internal/integration/service.go` | NetBox：列名改对、更新列提为 `netboxUpdateCols`（去 `status`、加 `site_name`，F-7；不含 tags/custom_fields/rack_name）、批次内去重（F-5）、`time.Now().UTC()`（F-3）、删预查询/ID 回填；Zabbix/GLPI：去 `ON CONFLICT` |
| `backend/internal/models/asset.go` | `NetBoxID` 标签 → `uniqueIndex` |
| `backend/migrations/000015_asset_netbox_unique.{up,down}.sql` | 新增：drop 普通索引 → 建 UNIQUE（down 反向） |
| `backend/internal/service/asset_service.go` | `Update` 撞 23505 → `ErrAlreadyExists`（409，与 `Create` 一致；F-8） |
| `backend/internal/integration/upsert_test.go` | 新增：helper 渲染列名断言（引用 `netboxUpdateCols`）+ NetBox 真插入/冲突更新（含 status/tags/custom_fields/rack_name 不被覆盖）+ 混合批次 + 同批重复 id 去重 + 空列表/错误透传 + Zabbix 重复 trigger 不重复 + ack 边界钉住 + GLPI 两次同步不重复（只喂 1 张票，见 §6） |
| `backend/internal/service/asset_service_test.go` | 新增 `Update` 唯一冲突 → 409 用例（F-8） |
| `backend/tests/db_smoke_test.go` | 新增 `TestDBSmoke_NetBoxUpsert`（真 PG：索引存在 + upsert 三态 + 人工列不被覆盖）；`MigrateRunner` 断言「应用数 == embed 内迁移数」且 15 已记录（F-A）；**修** `TestDBSmoke_DownPreservesLegacyColumns`（补第三次 Down + 升级库前置，否则空转） |
| `scripts/db_smoke.sh` | 白名单补新用例 |
| `TODO.md` / `docs/TRAPS.md` | G-22 结案（G-25/G-26/G-27 划界）；T-30（`ON CONFLICT` 需要真 UNIQUE 仲裁器 + Go 字段名≠列名）、T-31（两类假绿：前置缺失走 Skip、测试另抄一份生产清单） |

**不动**：`alerts.trigger_id` / `tickets.external_id` 不加唯一索引（§1.2-3 语义）；GLPI 的覆盖策略；NetBox 字段映射。

## 4. 验证清单

### V-0 现状实测（本机，2026-09-09；gorm v1.30 DryRun + 真 PG 18-alpine 临时容器）

| 问题 | 实测结论 | 对设计的影响 |
|---|---|---|
| 冲突目标写错 | PG `42703`，HINT 指向 `net_box_id` | 必须改列名 |
| SET 用 Go 字段名 | PG `42703 column "Name" of relation "t" does not exist`；**sqlite 成功执行**（大小写不敏感） | 单测不能只靠 sqlite（F-5） |
| 空更新列 | PG `42702 column reference "id" is ambiguous` | 空列表必须 `DO NOTHING`（F-4） |
| 唯一索引 + 多 NULL | 3 行 `net_box_id IS NULL` 共存，无冲突 | 手工资产不受影响 |
| 同名 `DROP INDEX` + `CREATE UNIQUE INDEX` 同一事务 | 成功，`pg_indexes` 显示 UNIQUE | 迁移可在一个文件里完成 |
| `CREATE INDEX CONCURRENTLY` | `cannot run inside a transaction block`（迁移执行器把整文件放一个事务） | 只能用普通建索引；大表有**读写**阻塞窗口（见 R-5） |
| 存量重复值 | 裸 PG：`could not create unique index "…" / DETAIL: Key (net_box_id)=(2) is duplicated`；**应用日志只剩 `ERROR: could not create unique index … (SQLSTATE 23505)`** —— 驱动 `*pgconn.PgError.Error()` 只拼 Severity/Message/SQLSTATE，Detail 字段不在其中（实测 pgx v5.5.1；本仓库未开 gorm `TranslateError`） | 迁移失败信息**不带重复值定位** → 必须靠升级前自检 SQL（R-1） |
| gorm 零值 uuid 主键 | 省略该列，DB 生成，三行 id 互不相同 | 删预查询后新增路径安全 |
| 混合批次 upsert（2 冲突 + 1 新增） | 行数正确、冲突行被更新、**id 未被改写** | 删「预查询 + 回填 ID」安全 |
| DDL 与版本记录 | `execInTx(db, upSQL, tailSQL)` 同一事务（internal/migrate/migrate.go:298-326） | 失败即整体回滚，无半应用状态（R-1 缓解） |

### V-1..V-11 用例

| 编号 | 验证项 | 手段 | 反证（变异） |
|---|---|---|---|
| V-1 | helper 渲染 `SET "col"="excluded"."col"`；空列表 → `DO NOTHING`；更新列**等于生产那份清单**（引用 `netboxUpdateCols`，不另抄）且不含 `status`/`tags`/`custom_fields`/`rack_name` | `upsert_test.go`（sqlite + DryRun 断言 SQL 字符串） | 退回手拼 `EXCLUDED.` → 断言红；给 `netboxUpdateCols` 加回 `tags` → `require.Equal` 红（N2 实测） |
| V-2 | 真 `SyncFromNetBox`：首次插入、二次同 `net_box_id` 改名字/机房 → 行数仍 1、字段被更新、本地 `status='retired'`/`tags`/`custom_fields`/`rack_name` 不被覆盖 | `upsert_test.go`（sqlite + httptest NetBox，`database.DB` 换成内存库并 `t.Cleanup` 还原） | 冲突目标退回 `netbox_id` → sqlite `no such column` 红；更新列加回 `status`/`tags` → 对应断言红 |
| V-3 | 调用点传给 helper 的是 **DB 列名**（`name`/`asset_type`/…），不是 Go 字段名；单测断言的**就是**调用点用的 `netboxUpdateCols` | V-1 的渲染断言 + 真 PG dbsmoke | 改回 `"Name"` → PG 42703 红（sqlite 抓不到，故必须两处都有） |
| V-4 | Zabbix 同步两次不产生重复、不报错；同 trigger 的历史行保留；**已知边界**：本地已 ack 的告警会被重复插入（TODO G-27，断言把当前行为钉住） | `upsert_test.go` | 加回 `ON CONFLICT ("trigger_id")` → 首次同步报「no PRIMARY KEY or UNIQUE constraint」红（见下） |
| V-7 | GLPI 同步两次不产生重复、不报错 | `upsert_test.go`（**刻意只喂 1 张票**，见 §6 G-25） | 加回 `ON CONFLICT` → 首次同步红 |
| V-8 | 混合批次：同批 1 冲突 + 1 新增 → 行数正确、冲突行 id 保留、新行 id 由 DB 生成 | `upsert_test.go` | 删掉冲突分支/新增分支 → 行数或 id 断言红 |
| V-9 | 同批重复 `net_box_id` 被去重（不去重时 PG 报 21000、整批回滚） | `upsert_test.go` | 删掉 `seen` 去重 → 断言 `n==1` 红（N5 实测） |
| V-10 | `SyncFromNetBox` 空列表 → `(0, nil)`；NetBox 4xx → 错误**透传**（不静默当空列表） | `upsert_test.go` | 把错误吞成 `return 0, nil` → 红 |
| V-5 | 真 PG：000015 后 `assets(net_box_id)` 是 UNIQUE（`pg_indexes.indexdef`）；重复 `net_box_id` 被拒；多 NULL 共存；真 `SyncFromNetBox` 插入/冲突更新/保留 id/不覆盖本地 status+tags+custom_fields+rack_name；**脏数据负循环**：删版本 15 → Up 必须失败且不记版本、索引仍非唯一 → 清重复 → Up 成功 | `TestDBSmoke_NetBoxUpsert` | 删 000015 → 42P10 红；把 000015 的 `CREATE UNIQUE` 改回普通 `CREATE` → 索引断言红 |
| V-6 | 无回归：`go vet` 干净、`go test ./...` 全绿、`db_smoke.sh` 两条路径绿、`TestDBSmoke_DownPreservesLegacyColumns` 仍真的回滚到 000013 | 本地 | 退回「两次 Down」的旧版本 → 断言错位、用例红（实测）；`000013.down.sql` 加回 `DROP COLUMN ticket_type` → 末尾断言红（M11 实测）；**注意反向变异**：若改成「只删最后一次 Down」，用例会**静默空转**（末尾断言恒真）—— 这正是必须显式补第三次 Down 的理由 |
| V-11 | 迁移缺失必须**红**不能跳过：`TestDBSmoke_MigrateRunner` 断言 `applied == embed 内 *.up.sql 数` 且 version 15 已记录；`NetBoxUpsert`/`DownPreservesLegacyColumns` 的前置从 `Skipf` 改成 `Fatalf` | 真 PG（`db_smoke.sh`） | 删掉 `000015` 两个文件 → 三个用例红、脚本 `EXIT=1`（N1b 实测）；只删 `.up.sql` → `migration 15 has no .up.sql` 红（N1 实测）。**修复前**同样变异是「全 SKIP + EXIT=0」的假绿（审计 F-A） |

**V-4 的反证（实测成立）**：把 `buildUpsertClause("trigger_id", …)` 加回去 → **首次**同步的 INSERT 立刻报 `ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint`（sqlite 实测；真 PG 同源 42P10），用例红。用例**预置一行 `status='resolved'` 的历史行**正是为了逼首次同步走到 INSERT —— 否则预过滤会拦住第二次同步、断言恒真（空转）。同理 V-7（GLPI）加回 `ON CONFLICT` 也即红。

### V-12 变异反证汇总（本机实测，每次变异后恢复原文件）

| 编号 | 变异 | 期望 | 实测 |
|---|---|---|---|
| M1 | 手拼 `EXCLUDED.<col>` | 红 | ✅ 红 |
| M2 | SET 退回 Go 字段名 | 红 | ✅ 红 |
| M3 | 冲突目标退回 `netbox_id` | 红 | ✅ 红 |
| M4 | 测试库去掉唯一索引 | 红 | ✅ 红 |
| M5 | Zabbix 加回 `ON CONFLICT` | 红 | ✅ 红（`does not match any PRIMARY KEY or UNIQUE constraint`） |
| M6 | GLPI 加回 `ON CONFLICT` | 红 | ✅ 红 |
| M7 | 000015 建普通索引 | 红 | ✅ 红 |
| M8 | 更新列加回 `status` | 红 | ✅ 红 |
| M9 | 更新列去掉 `site_name` | 红 | ✅ 红 |
| M10 | 测试退回「两次 Down」（去掉 000015 的 Down） | 红 | ✅ 红（首个索引形态断言错位） |
| M11 | `000013.down.sql` 加回 `DROP COLUMN ticket_type` | 红 | ✅ 红（末尾断言，证明第三次 Down 不是空转） |
| N1 | 删 `000015.up.sql`（留 down） | 红 | ✅ 红（`migrate.Up` 报 `migration 15 has no .up.sql`） |
| N1b | 删 `000015` up+down 两个文件 | 红 | ✅ 红（`schema_migrations 里没有 version=15`，`db_smoke.sh EXIT=1`）；**修复前为全 SKIP + EXIT=0** |
| N2 | `netboxUpdateCols` 加回 `tags` | 红 | ✅ 红（`require.Equal` 报 len 7→8） |
| N5 | 删掉批次内 `seen` 去重 | 红 | ✅ 红（`n` 期望 1 实得 2） |

> 变异方法：`cp` 备份 → `sed`/`python` 改 → 跑对应用例 → `cp` 还原并 `grep` 复核（见每次实测输出）。
> **不做的变异**：`now := time.Now()` → 本地/UTC 无法用断言区分（无时区断言），只由注释与 review 保证（见 §5 R-6）。

## 5. Risk

- **R-1（数据完整性，高）**：000015 建 UNIQUE 索引时，若存量库已有重复 `net_box_id`（多行指向同一 NetBox 设备），迁移**失败**、服务启动中断。
  **缓解**：① 失败是**期望行为**（宁可挡住也不能悄悄删数据）；② **实测** DDL 与版本记录在同一事务（`execInTx`），失败即整体回滚 —— 不会留下「索引建了一半 / 版本已记录」的半应用状态，修好数据后重跑即可（`TestDBSmoke_NetBoxUpsert` 的负循环守这条）；③ **注意**：应用日志里只有 `ERROR: could not create unique index … (SQLSTATE 23505)`，**没有 PG 的 DETAIL（哪个键重复）** —— 驱动 `*pgconn.PgError.Error()` 只拼 Severity/Message/SQLSTATE（实测 pgx v5.5.1），Detail 不在其中 → 定位重复值只能靠自检 SQL，别等日志；④ 迁移注释与本文都给出升级前自检 SQL（`select net_box_id, count(*) from assets where net_box_id is not null group by 1 having count(*)>1`）；自动去重被否决（静默改数据更危险）。
  **注意**：**升级前就该跑这条自检**，而不是等迁移失败 —— 因为它会挡住整个服务启动。
- **R-2（正确性，中）**：删掉「预查询 + 回填 ID」后，若某行 `net_box_id` 为 NULL（非 NetBox 来源），upsert 会插入新行而非更新。
  **缓解**：`ConvertToAsset()` 对 NetBox 设备**必设** `NetBoxID`；NULL 行本就不该被 NetBox 同步更新；唯一索引兜住「同 id 两行」。
- **R-3（并发，中）**：Zabbix 去掉 `ON CONFLICT` 后，两个并发同步可能同时判定「不存在」并各插一行 → 重复的未恢复告警。
  **缓解**：同步由 cron/手动单点触发（非高并发路径）；重复行是「多一条历史」，非数据损坏；后续若需要强约束，可加 `(trigger_id) WHERE status='problem'` 的**部分**唯一索引（本轮不做）。
- **R-4（迁移幂等，低）**：`idx_assets_net_box_id` 同名索引先 drop 再建，若迁移重放或与 AutoMigrate 交叉，可能出现「索引已存在但非唯一」。
  **缓解**：迁移内用 `DROP INDEX IF EXISTS` + `CREATE UNIQUE INDEX`（**刻意不写 `IF NOT EXISTS`**：DROP 已在前面，同名索引不可能还在；万一出现坏状态就让它大声失败，而不是被 `IF NOT EXISTS` 吞掉、留下非唯一索引继续 42P10）；重放时净效果等价（重建一次索引，数据不变）；`TestDBSmoke_NetBoxUpsert` 的负循环与 `TestDBSmoke_MigrationReapply` 覆盖重放。
- **R-5（可用性，中）**：`CREATE INDEX CONCURRENTLY` 在 PG 里**不能进事务**（实测），而迁移执行器把整个文件放一个事务 → 只能普通建索引，且 `DROP INDEX` 取到的 **ACCESS EXCLUSIVE 一直持到 COMMIT**（实测 `pg_locks`），把后面的建索引全程盖住 → `assets` 期间**读写都被阻塞**。
  **缓解**：`assets` 在本产品量级（自建 ITSM，量级 10³–10⁵ 行）建索引 <1s；迁移注释写明「大表请在维护窗口执行」；不引入 `CONCURRENTLY`（会要求改造迁移执行器支持非事务迁移，超出本轮范围）。
- **R-6（数据语义，低）**：`updated_at` 进更新列 → 语义从「最后修改」变成「最后同步」；且 `time.Now()` 是本地墙钟，而 gorm `NowFunc` 是 `time.Now().UTC()`（`database/database.go:49-52`）→ 非 UTC 部署下同一行 `created_at` 与 `updated_at` 差一个时区偏移。
  **缓解**：改为 `time.Now().UTC()` 对齐（本轮已改）；「最后同步」语义在列名上无法体现，属可接受折中（同步本来就该刷新它）。`SyncFromZabbix`/`SyncFromGLPI` 的 `time.Now()` 未动（既存行为，同一行两列同为本地时钟，自洽）。

## 6. 边界（不做的事）

- 不给 `alerts.trigger_id` / `tickets.external_id` 加唯一索引（§1.2-3）。
- 不改 `SyncAll` 的错误聚合语义、不改 GLPI 工单覆盖策略。
- 不引入 `TargetWhere`/部分唯一索引（NetBox 用整列唯一即可，NULL 天然不冲突）。
- 不动 `internal/api/testdata/migrations/` 的 sqlite 兼容 schema（非生产路径，与既有口径一致）。
- **GLPI 一次新增 ≥2 张工单仍会失败**（`Ticket.BeforeCreate` 的 `generateTicketNumber` 按「当天已建条数」算号，同批每行算出同一个号 → `ticket_number` 唯一索引整批拒绝）。这是**另一个缺陷**（TODO **G-25**），与本轮 upsert 修复无关：本轮只去掉那条注定报错的 `ON CONFLICT`。因此 `TestSyncFromGLPI_两次同步不重复` **刻意只喂 1 张票**，避免红在编号上、掩盖它真正要守的语义。
- **NetBox 状态映射不做**：`ConvertToAsset` 仍硬编码 `Status: "active"`（新增行用），本轮只把它从更新列里摘掉（F-7），不引入 NetBox status → 本地 status 的映射。
- **不修 000014 的 `SET lock_timeout` 泄漏**（TODO G-26：会话级 `SET` 会留在连接池的连接上）—— 与本轮无关。
- **`rack_name` 不同步**：`NetBoxDevice` 没有 rack 字段、`ConvertToAsset` 不设 `RackName`（插入恒空串），故更新列**不含** `rack_name`（含了等于每次同步清空人工填的机柜）。将来若补 rack 映射，插入/更新两侧要一起补。
- **Zabbix 本地已 ack 的告警会被重复插入**（TODO G-27）：预过滤只认 `status='problem'`，ack 行不算「已存在」→ 下次同步再插一行 `problem`。属**语义决策**（本地 ack 的进行中告警该不该再插？），本轮只把当前行为用断言钉住（`TestSyncFromZabbix_本地已确认的告警会重复插入`），不改语义。
- **`PATCH /assets/:id` 的唯一冲突映射**：本轮顺手补上（`asset_service.Update` 撞 23505 → `ErrAlreadyExists` 409，与 `Create` 一致）—— 唯一索引是 000015 新引入的失败面，不修的话客户端会看到 500（审计 F-8）。同一处「`updates` map 任意列可写（如直接 `{"status":"retired"}` 绕过 Retire 流程）」是**既有问题**，本轮不动。
