# 修复方案：`assets` 的 jsonb 零值写入失败（TODO G-20）

> 状态 **rev3（并入三视角审查）** · 日期 2026-09-09 · 来源 `TODO.md` G-20（compose 轮 smoke 实测）
> 分支 `main`（用户已授权直推主干）
> 前序：`docs/FIX-PLAN-COMPOSE-RUNTIME.md`（G-9/G-10/G-13 + D-I）——本缺陷正是那轮
> 在真 Postgres 上跑 CLI 回归时被抓住的
> 审查：**正确性 / 一致性 / 数据完整性** 三视角已完成，逐条处置见 §7
>
> **rev2 → rev3 的实质修正（含一次自我纠错）**：
> rev2 声称「`default:'[]'` 对 string 字段不生效、列照旧写 `''`」——**这是错的**，
> 依据是我一次读错的 DryRun 探针（只看了 INSERT 的列清单，没看 VARS；且探针里的
> 「空串」判定实际命中的是零值 UUID）。rev3 复测：`default:'[]'` 下
> `VARS=[<uuid空> x [] {}]` —— gorm 把字面量**替换**进参数（`schema/field.go:262-268`
> 的 `DefaultValueInterface` + `callbacks/create.go`），所以**方案 A 对 Create 确实有效**。
> 但它不覆盖 `Save()`（update 路径不吃 default 替换）。rev3 仍选**钩子 + 列默认值**
> （方案 C），理由从「A 不成立」改为「C 覆盖更宽且可被 sqlite 门禁钉住」，见 §2.1。

## 1. 问题（What / Why）

### 1.1 现象

| 路径 | 现象 | 证据 |
|---|---|---|
| `cmd/seed`（演示数据） | 真 PG 上**每一条资产都建不出来**，日志刷 `创建服务器失败/创建交换机失败`，进程仍 `exit 0` | compose 轮 smoke 日志；CI dbsmoke 的 seed 步只敢断言用户路径（`.github/workflows/ci.yml:141-143` 有注释） |
| `POST /api/assets`（不带 `custom_fields`） | 500（`apierr.Internal`），非 400/409 | `backend/internal/api/handlers/asset_handler.go:86-104` |
| NetBox 同步 | **不触发本缺陷**——它是唯一显式写 `"{}"` 的路径 | `backend/internal/integration/service.go:113-114`；注：该路径当时另有既存缺陷（upsert 用 Go 字段名当列名 → `42703`），与本缺陷无关，**已在 G-22 轮修复**（见 `docs/FIX-PLAN-NETBOX-UPSERT.md`） |

Postgres 原始报错：`invalid input syntax for type json (SQLSTATE 22P02)`。

### 1.2 根因

```go
// backend/internal/models/asset.go:48-49
Tags         string `json:"tags" gorm:"type:jsonb"`          // JSON
CustomFields string `json:"custom_fields" gorm:"type:jsonb"` // JSON
```

```sql
-- backend/migrations/000001_init.up.sql:264-265（可空、无 DEFAULT）
tags            JSONB,
custom_fields   JSONB,
```

Go 的 `string` 零值是 `""`，gorm 照原样放进 INSERT（该字段没有 `default` tag，
gorm 不会跳过零值）→ `''` 不是合法 JSON → PG 拒收整行。

### 1.3 影响面

1. **演示/首次部署**：`make deploy` = `install + docker-up + db-seed`，seed 的资产全失败，
   用户看到的是「机房、机柜、告警都在，资产列表空」——半种子状态且无报错（退出码 0）。
2. **资产 API**：客户端不传 `custom_fields` 即 500（`Tags` 同理）。
3. **不是**单字段问题：`Tags`/`CustomFields` 是同一类（`string` + `type:jsonb` + 无默认）。
   全仓扫描 `type:jsonb` 只有三处，第三处 `ticket.Tags` 带 `default:'[]'`
   （`backend/internal/models/ticket.go:25`），**按 §2.1 复测的机制它是有效的**，
   且 `ticket_service.go:104-105` 另有 `""→"[]"` 归一，插入路径不落 NULL。

### 1.4 为什么单测抓不到

sqlite 是动态类型、**不校验 JSON**，且各测试的 `CREATE TABLE assets` 把这两列声明成 `TEXT`
（`handlers/topology_handler_test.go:30`、`handlers/diagnostic_handler_test.go:49`、
`integration/metric_sync_test.go:43`、`service/topology_service_test.go:30`、
`service/diagnostic_service_test.go:63`、`cmd/seed/main_test.go:132`）。
`go test ./...` 26 包全绿与本缺陷共存，只有**真 Postgres** 能暴露它——所以保证必须落在
dbsmoke（真 PG）+ smoke-compose 上。**rev3 的额外收益**：钩子在驱动之前改值，
因此这条不变量在 sqlite 默认门禁里同样可断言（V-1）。

### 1.5 消费方现状（决定方案边界）

- Go 侧**没有**任何代码读这两个字段（除 NetBox 同步的写入）；前端 `frontend/src` 无引用；
  迁移/查询里**没有** JSONB 运算符（`->>`/`@>`）——即两列的「JSONB 类型」目前只提供
  写入期校验，没有任何查询依赖。
- `openapi.yaml` 未定义这两个字段（不进生成类型）。

## 2. 方案（How）

### 2.1 候选与实测结论（rev3 复测口径）

| 方案 | 做法 | 实测/推理结论 |
|---|---|---|
| A 只加 `default:'[]'`/`default:'{}'` tag | 模型加字面默认值 | **对 Create 有效**（复测 `VARS=[… x [] {}]`，gorm 显式替换参数）。**但不覆盖 `Save()`/`Updates`**（update 路径不做 default 替换）→ 仍有 22P02；且值由 Go 侧写死，列默认值形同虚设 |
| B 加 `default:(-)` tag + 列默认值 | 让 gorm 省略零值列 | 可用（DryRun 实测两列被省略并进 `RETURNING`）。代价：保证**依赖迁移已应用**——dev `AutoMigrate` / 未迁移库下静默落 NULL（`(-)` 不给 DB 默认，`schema/field.go:264-268`） |
| **C（选定）** 模型钩子归一 + 列默认值兜底 | `BeforeSave` 把空值写成合法 JSON；迁移 `SET DEFAULT` + 回填 NULL | 显式、sqlite 可测、覆盖 Create/Save/结构体 Updates；列默认值兜住非 gorm 写入方与受限 `Select` 插入。**不依赖迁移**（应用层保证） |
| D 列改 TEXT（照 `000013` 对其它表的做法） | `ALTER COLUMN TYPE TEXT` | 丢掉写入期 JSON 校验；与 000013 的动因不同（那里模型写的是 TEXT、列是 jsonb） |

**A vs C 的取舍**：A 是 2 行，但 `Save()`（`db.Save(&asset)` 写全字段）会拿零值 `''` 打 PG，
且显式值掩盖列默认值。C 多一个 ~10 行钩子，换来「写入路径全覆盖 + 默认门禁可测」。
当前代码里**没有** `Save(asset)` 调用点，所以 A 也能让 CI 转绿——选 C 是为了不再留一个
同类地雷（本缺陷的教训正是「零值悄悄变成非法 JSON」）。

### 2.2 选定 C，具体设计

```go
// backend/internal/models/asset.go —— 新增钩子（Asset 此前没有钩子）
// BeforeSave 把两个 jsonb 列的空值归一为合法 JSON 字面量（TODO G-20）。
// 为什么不用 gorm 的 default tag：字面默认值只作用于 Create 的参数替换（Save/Updates 不吃），
// 而 default:(-) 把保证寄托在「迁移一定跑过」上；钩子是应用层显式保证，sqlite 门禁可测。
func (a *Asset) BeforeSave(tx *gorm.DB) error {
	if strings.TrimSpace(a.Tags) == "" {
		a.Tags = "[]"
	}
	if strings.TrimSpace(a.CustomFields) == "" {
		a.CustomFields = "{}"
	}
	return nil
}
```

```sql
-- backend/migrations/000014_asset_jsonb_defaults.up.sql（幂等、可重入）
-- 应用侧的功能保证在 Asset.BeforeSave；这里是 schema 层兜底：
-- 非 gorm 写入方（psql / 手工 SQL / 受限 Select 插入 / 未来的同步服务）省略这两列时拿到合法 JSON。
--
-- 语句顺序很关键：migrate 的 execInTx 把整个文件放在一个事务里（internal/migrate/migrate.go:309-325），
-- ALTER TABLE ... SET DEFAULT 取 ACCESS EXCLUSIVE 且**持有到 COMMIT**（PG 11+ 虽是元数据操作、不重写表）。
-- 因此先回填（RowExclusive，不阻塞读写）再改默认值（短锁窗口），否则回填期间 assets 全表不可读写。
-- 实测（300k 行 / 14MB）：UPDATE 在前时并发 SELECT/UPDATE 正常，ACCESS EXCLUSIVE 仅出现在两次 ALTER 的 ~3.4ms 内。
SET lock_timeout = '5s';   -- 抢不到锁就失败退出（事务回滚、api 启动报错），不要无限期挂住启动

-- 存量回填：历史行可能是 NULL（模型此前写 '' 会失败，故 NULL 只可能来自非 gorm 写入）。
-- 含 retired 行（assets 无软删除，退役行仍在表内）；NULL 与 []/{} 对全部消费方等价（§1.5），语义无损。
UPDATE assets SET tags          = '[]'::jsonb WHERE tags IS NULL;
UPDATE assets SET custom_fields = '{}'::jsonb WHERE custom_fields IS NULL;

ALTER TABLE assets ALTER COLUMN tags          SET DEFAULT '[]'::jsonb;
ALTER TABLE assets ALTER COLUMN custom_fields SET DEFAULT '{}'::jsonb;
```

```sql
-- backend/migrations/000014_asset_jsonb_defaults.down.sql
-- 只撤 schema 默认值，不动数据：回填不可逆（无法知道哪些行原本是 NULL，且 NULL 与 []/{} 对消费方等价）。
-- 注意回滚后的组合状态：down 000014 + 新二进制时，裸插入/受限插入会静默落 NULL（不报错），
-- 排查时别误读成「修复没生效」——应用层钩子仍在，gorm 路径不受影响。
ALTER TABLE assets ALTER COLUMN tags          DROP DEFAULT;
ALTER TABLE assets ALTER COLUMN custom_fields DROP DEFAULT;
```

语义选择：`tags` 默认 `[]`（数组，与 seed 的 `["web","production"]` 一致），
`custom_fields` 默认 `{}`（对象，与 NetBox 同步写的 `"{}"` 一致）。
`backend/internal/integration/service.go:127` 的 `Tags: "{}"` 同时改为 `"[]"`（列语义统一，1 行）。

**为什么不加 `NOT NULL`**：加 NOT NULL 需全表校验 + 重写（`ACCESS EXCLUSIVE` 长锁），
对大 `assets` 表是迁移期停摆风险；钩子 + 列默认值已覆盖全部已知写入路径。
代价要写明：`PUT {"tags": null}` 仍能造出 NULL，回填后的「无 NULL」不是 DB 强制的不变量
（应用层拒 null 归 G-21）。

**为什么保留 jsonb 而不走方案 D**：客户端显式传的值仍需 PG 校验（`'{bad'` 被拒），
这是当前唯一实质存在的校验点；改成 TEXT 等于把非法 JSON 静默存进去。

### 2.3 各机制各自保证什么（避免「以为有兜底其实没有」）

| 写入方式 | 谁保证 | 结果 |
|---|---|---|
| `db.Create(&Asset{...})`（API / seed / 同步） | `BeforeSave` 钩子 | 空值 → `[]`/`{}` 显式写入 |
| `db.Save(&asset)` | `BeforeSave` 钩子 | 同上（`Save` 写全字段，钩子先归一） |
| `db.Model(&a).Updates(Asset{零值})`（结构体） | gorm 自身 | **零值被 gorm 跳过** → 不写、不报错（静默 no-op，不是 22P02） |
| `db.Select("tags").Updates(Asset{Tags: ""})` | **无人保证** | 写 `''` → 22P02 → 500（G-21；实测钩子不介入） |
| `Updates(map[string]any{"custom_fields": ""})` | **无人保证** | 显式 `''` → 22P02 → 500（G-21） |
| `Updates(map[string]any{"custom_fields": []string{...}})` | **无人保证** | gorm 渲染成 `('x')` → 22P02 → 500（G-21） |
| `Updates(map[string]any{"custom_fields": nil})` | **无人保证** | 写入 NULL，破坏回填后的不变量（G-21） |
| `db.Select("name").Create(&asset)`（受限插入） | 列默认值（000014） | DB 补 `[]`/`{}` |
| 裸 SQL / 非 gorm 写入方省略列 | 列默认值（000014） | DB 补 `[]`/`{}` |
| 裸 SQL 显式写 `''` | **无人保证** | PG 拒收（正确行为） |

> **上表是实测口径**（gorm v1.30 + sqlite 探针 + `models/hooks_test.go`），不是推理：
> 钩子只覆盖 `Create`/`Save`；`Updates` 一族全部绕过钩子（结构体零值是被 gorm 跳过而非报错，
> `Select`/`map` 则直接写出 `''`）。这三条统一登记 G-21。

> `Updates` 一族是**同一族**缺陷（入参不做规范化），统一登记 G-21；
> 本轮不扩大爆炸半径（`UpdateAsset` 的语义变更需要单独的需求 + 审查）。

## 3. Where（变更清单）

| 文件 | 改动 |
|---|---|
| `backend/internal/models/asset.go` | 新增 `BeforeSave` 钩子（+ `strings`/`gorm` import） |
| `backend/internal/models/hooks_test.go` | 新增：零值/纯空白/显式值/Save 路径四种输入的归一断言 + `Updates` 路径的实测特征化（sqlite，默认门禁） |
| `backend/migrations/000014_asset_jsonb_defaults.{up,down}.sql` | 新增：回填 → SET DEFAULT（含 `lock_timeout`）；down 只 DROP DEFAULT |
| `backend/internal/integration/service.go` | `Tags: "{}"` → `"[]"`（列语义统一） |
| `backend/cmd/seed/main.go` | 失败计数 → 非零退出（`seedData` 返回 error / `log.Fatalf`）；本模块并入（§7-D1）。**另修**：演示资产 `asset_tag` 加 `rack.Name`（见 §7-E1，退出码改动的连带发现） |
| `backend/cmd/seed/main_test.go` | 新增 `TestSeed_资产asset_tag站内唯一`（断言 `seedData == 0` 且 48 个资产）；测试 schema 补 `UNIQUE (asset_tag, site_id)` 镜像真库约束 |
| `backend/tests/db_smoke_test.go` | 新增 `TestDBSmoke_AssetJSONBDefaults`（fresh）+ `TestDBSmoke_AssetJSONBBackfill`（upgrade）；修 `TestDBSmoke_DownPreservesLegacyColumns` 空转 |
| `scripts/db_smoke.sh` | 升级库过滤按**版本号**（不再 `! -name '000013_*'`，否则 000014 被预应用 → 假绿）；预置 NULL / 非 NULL 两行存量资产；`-run` 白名单补新用例；de-number 注释 |
| `.github/workflows/ci.yml` | dbsmoke seed 步：断言资产数 > 0 且无 NULL jsonb；de-number 注释 |
| `scripts/smoke-compose.sh` | step 5b 补 seed 资产断言；新增 step 6b：经 nginx 登录（cookie jar）后 `POST /api/assets` 只给 name，期望 201 且响应含 `"custom_fields":"{}"`（登录用 `smokeadmin2` —— step 5 的 set-role 已把 `smokeadmin` 降为 auditor，无写权限） |
| `docs/TRAPS.md` | 新增 T-28（gorm `default` tag 对 string 字段是 Go 侧参数替换，非 DB 默认值）+ §五 索引 |
| `TESTING.md` / `08-部署运维.md` / `README`? | 去编号（「13 个迁移」「schema_migrations=13」→ 动态/泛指）；§8.3.4 补 000014 锁窗口与「大表先数 NULL 行」 |
| `TODO.md` | G-20 结案（含拒绝 NOT NULL 的理由）；登记 G-21/G-22/G-23 |
| **不动** | `backend/migrations/schema.sql:181-182` 与 `backend/internal/api/testdata/migrations/` 三份副本——前者是历史快照、后者是 sqlite 兼容 schema，均非生产路径（与 D1-D7 轮同一口径） |

## 4. 验证清单

| 编号 | 验证项 | 手段 | 反证（变异） |
|---|---|---|---|
| V-1 | 钩子把零值/纯空白归一为 `[]`/`{}`，显式值不动 | `models/hooks_test.go`（sqlite，默认门禁） | 删钩子 → 红 |
| V-2 | 真 PG：gorm 建零值资产成功，落库 `[]`/`{}` | dbsmoke `TestDBSmoke_AssetJSONBDefaults` | 钩子只归一一个字段 → 红 |
| V-3 | 真 PG：**受限/裸插入省略两列 → 列默认值生效**；且 `information_schema.columns.column_default` 确为 `'[]'::jsonb`/`'{}'::jsonb` | 同上用例 | 删 `SET DEFAULT` → 裸插入落 NULL → 红 |
| V-4 | 升级路径：存量 NULL 行被回填为 `[]`/`{}`，且**非 NULL 行未被改写** | dbsmoke `TestDBSmoke_AssetJSONBBackfill` + `db_smoke.sh` 预置两行 | 删回填 UPDATE → 红 |
| V-5 | `cmd/seed` 在真 PG 上真的建出资产，且**失败时退出码非 0** | CI dbsmoke seed 步：`count(*) > 0` + 无 NULL；退出码由 seed 自身保证 | 现有缺陷态 → 红（本轮前实测）；删失败计数 → 半失败仍绿 |
| V-6 | 完整栈：`POST /api/assets` 不带 `custom_fields` → 201 | `scripts/smoke-compose.sh` step 6b | 删钩子 → 500 |
| V-7 | 无回归：`go vet ./...` 干净、`go test ./...` 26 包绿、`db_smoke.sh` 两条路径绿、`smoke-compose.sh` 全绿 | 本地 | — |
| V-8 | 回滚安全：`Down` 000014 后列默认值消失但数据仍在；`Down` 000013 仍不丢旧列 | dbsmoke 改后的 `TestDBSmoke_DownPreservesLegacyColumns` | 删 000014 down 的 DROP DEFAULT → 红 |
| V-9 | seed 在**真 PG 全新库**上 exit 0 且建出全部 48 个演示资产（无静默半种子） | 本机真 PG 实验（空库 → seed，两次）+ CI dbsmoke seed 步 + `cmd/seed` 单测 | 退回旧 `asset_tag` → 真 PG exit 1 / 36 处 duplicate key；单测红 |

> **V-5 的口径修正（rev3）**：rev2 把「删迁移 `SET DEFAULT` → V-1 红」当作变异反证，
> 但 V-1 走的是 gorm（钩子/默认值都在应用侧生效），删 `SET DEFAULT` 不会让它变红 —— 那是假阴性。
> 列默认值的反证必须用**省略列的裸插入**（V-3）。两者已拆开。

## 5. Risk

| 编号 | 失败模式（具体） | 缓解 |
|---|---|---|
| R-1 | **钩子被绕过**：`Updates(map)`（`handlers/asset_handler.go:113-119` 正是这条路）显式传 `""`/`[]`/`null` → 仍 22P02 → 500 或写入 NULL。列默认值**帮不上**（列被显式赋值） | §2.3 把「谁保证什么」写进文档；V-6 覆盖 Create 路径；三条 `Updates(map)` 语义登记 G-21（本轮不做，避免扩大爆炸半径） |
| R-2 | **迁移在存量库上失败/不可重入**：语句顺序错（回填被长锁罩住）→ 迁移期 assets 全表停写；或回填触发全表重写 | §2.2 已把 UPDATE 放 ALTER 前并加 `lock_timeout`；000014 只做元数据 `SET DEFAULT` + `WHERE ... IS NULL` 定向回填；V-4 在升级路径库上真跑；down 只 `DROP DEFAULT`（不删列、不动数据） |
| R-3 | **保证只活在 CI**：将来有人删掉 dbsmoke 用例或钩子 → 缺陷静默复发 | V-1 在默认门禁（sqlite）里也钉住钩子；V-2..V-4 的变异反证写进 TODO 结案；CI 的 dbsmoke job 是门禁 |
| R-4 | **回填改变读侧语义**：把 NULL 变成 `{}`/`[]` 后，某个读侧把「空对象」当「有值」 | §1.5 已核实 Go/前端无读侧；`{}` 比 NULL→`""` 更合法、更易判断 |
| R-5 | **seed 改退出码后 CI/部署失败暴露**：`make deploy` 从此会在资产失败时中断（此前静默通过） | 这正是 V-5 的目的；失败信息带具体原因（`log.Fatalf` 前逐条 `log.Printf` 已存在），不会只给一个退出码 |

## 6. 不做（划界）+ 待登记

- **不做**：`CreateAsset`/`UpdateAsset` 的入参规范化（`null`/`""`/JSON 数组 → 400 或归一）。
  现状：`""`/`[]` → 22P02 500，`null` → 写入 NULL。登记 **G-21**（含 `Updates(map)` 对 jsonb 的语义定义）。
- **不做**：`ticket.Tags` 的表示一致性（`default:'[]'` 对 Create 有效、service 层另有归一，
  属「表示不一致」而非缺陷）。归 **G-23**。
- **不做**：给这两列加 `NOT NULL`（理由见 §2.2）。
- **登记**：**G-22** —— NetBox 同步 upsert 用 Go 字段名当列名
  （`integration/upsert.go:13-22` 生成 `ON CONFLICT ("netbox_id") DO UPDATE SET "Name"=EXCLUDED.Name`
  → PG `42703 column "Name" does not exist`，**首次插入即失败**）。既存缺陷，与本缺陷无关，
  本轮只修正 §1.1 的措辞（原文「正常」只对「不触发 22P02」成立）。

## 7. 审查处置（逐条）

> 三份审查报告（正确性 / 一致性 / 数据完整性）的发现与处置。
> 「采纳」= 并入本方案（rev3 已落）；「否决」= 不改，附理由。
> 注：审查期间工作区曾有一版探针改动（`asset.go` 的 `default:(-)` + 临时测试文件），
> 已全部回滚，当前工作区只有本方案文档为未跟踪文件。

### 7-A 阻断项（已全部并入 rev3）

| # | 视角 | 发现 | 处置 |
|---|---|---|---|
| A1 | 一致性/正确性/完整性 | `scripts/db_smoke.sh:131` 的 `find ... ! -name '000013_*'` 会把 **000014 也预应用**到「存量升级库」→ 升级路径的 000014 变成「在已有默认值的表上再设一次」→ 假绿 | **采纳**：过滤改成**按版本号比较**（`awk` 取文件名前 6 位数字 `< 13`），并把「预置 NULL 存量行」交给脚本（A2/V-4） |
| A2 | 完整性 | 回填正确性**零覆盖**：两条路径在 000014 执行时表里都没有资产，`WHERE tags IS NULL` 恒命中 0 行 | **采纳**：`db_smoke.sh` 在升级库预插两行（一行 `tags/custom_fields IS NULL`、一行 `'["x"]'::jsonb`/`'{"k":"v"}'::jsonb`），dbsmoke 断言 NULL→`[]`、非 NULL 未被改写 |
| A3 | 一致性/正确性/完整性 | 新增 000014 让 `TestDBSmoke_DownPreservesLegacyColumns` **静默空转**（`migrate.Down` 只回滚最新版本 → 回滚的是 000014 而非它要守的 000013） | **采纳**：改为先 `Down` 一次（000014）并断言列默认值消失，再 `Down` 一次（000013）并断言 `tickets.ticket_type` 仍在 |
| A4 | 正确性/完整性 | V-5 的变异「删迁移 `SET DEFAULT` → V-1 红」**是假阴性**（gorm 路径不经 DB 默认值） | **采纳**：拆成 V-3（裸插入省略列，删 `SET DEFAULT` 必红）+ V-1（钩子，删钩子必红）；并加 `information_schema.column_default` 断言 |
| A5 | 正确性/完整性 | 迁移语句顺序：`ALTER ... SET DEFAULT` 取 ACCESS EXCLUSIVE 且持有到 COMMIT → 回填期间 `assets` 全表不可读写；无 `lock_timeout` 时可能无限期挂住 api 启动 | **采纳**：两条 `UPDATE` 移到 `ALTER` 之前 + 文件头 `SET lock_timeout = '5s'`（实测 300k 行验证，见 §2.2 注释） |
| A6 | 正确性/一致性 | 新增 dbsmoke 用例不在 `scripts/db_smoke.sh` 的 `-run` 白名单里 → **根本不会跑** | **采纳**：白名单补 `TestDBSmoke_AssetJSONBDefaults`（fresh）与 `TestDBSmoke_AssetJSONBBackfill`（upgrade） |
| A7 | 完整性 | `cmd/seed` 失败仍 `exit 0`，本模块没修「失败没人知道」（§1.3 自己把它列为影响 #1） | **采纳**：并入本轮（失败计数 → 非零退出 + CI 断言资产数） |
| A8 | **自查（实现期实测）** | rev3 §2.3 原表把「结构体 `Updates`」也算作钩子保证 —— 实测**钩子不覆盖 Updates 一族**：结构体零值被 gorm 跳过（静默 no-op），`Select(...).Updates`/`Updates(map)` 直接写出 `''` | **采纳**：§2.3 改为实测口径；加特征化测试 `TestAsset_BeforeSave_Updates路径不在覆盖范围` 钉住边界（G-21 修好后该用例应改为断言归一） |

### 7-B 采纳（非阻断）

| # | 视角 | 发现 | 处置 |
|---|---|---|---|
| B1 | 正确性 | rev1 §2.3 的机制论证方向反了（gorm 是**替换参数**而非走 DB 默认值）；`Select("*")` 不退化、受限 `Select` 才依赖 DB 默认 | **采纳**：rev3 §2.1/§2.3 重写（并顺带纠正 rev2 对方案 A 的错误否定，见文首） |
| B2 | 正确性/完整性 | `Save()` 仍会 22P02，必须在文档里写明而非默认覆盖 | **采纳**：钩子覆盖 `Save`（§2.3 表格显式列出）；`Updates(map)` 三种入参登记 G-21 |
| B3 | 正确性 | `Updates(map)` 传合法 JSON 数组 `[]` 也会 500（gorm 渲染 `('a')`）；`null` 会写入 NULL 破坏回填不变量 | **采纳**：§2.3 逐条列出，G-21 描述改为「入参规范化」并显式列 `null`/`""`/数组 |
| B4 | 一致性/完整性 | `ticket.Tags` 的「落 NULL」表述错误 | **采纳**：§1.3 改为「`default:'[]'` 有效 + service 层归一，插入路径不落 NULL」 |
| B5 | 正确性/完整性 | `integration/service.go:127` 给 `Tags` 写 `"{}"`（对象）与列默认 `[]`（数组）语义分裂 | **采纳**：改为 `"[]"`；其余表示问题归 G-23 |
| B6 | 完整性 | 回填覆盖 retired 行，文档未写明 | **采纳**：§2.2 注释写明「含 retired 行，语义无损」 |
| B7 | 完整性 | `down` 回滚后的**组合状态**容易被误读成「修复没生效」 | **采纳**：写进 `000014_*.down.sql` 注释 |
| B8 | 一致性 | 陈旧编号：`08-部署运维.md:277`、`TESTING.md:17`、`ci.yml:64/125`、`db_smoke_test.go:62`、`db_smoke.sh:13` | **采纳**：全部去编号（改成「全部迁移」/动态取值） |
| B9 | 一致性 | `schema.sql` 与 testdata 三份副本应显式「不动」 | **采纳**：§3 表格末行注明 |
| B10 | 一致性 | 需登记 `docs/TRAPS.md`（有明确的添加约定 §六） | **采纳**：T-28 + §五 索引 |
| B11 | 一致性 | smoke step 6b 需要一个能拿会话的登录变体（当前 `login()` 丢弃 body） | **采纳**：用 cookie jar（`-c/-b`）复用同一账号，不引入 token 变体 |
| B12 | 一致性/完整性 | TODO 结案要说明**拒绝 NOT NULL** 的理由 | **采纳**：§2.2 已写；结案词同步 |
| B13 | 完整性 | 上线前三条检查：NULL 计数前后比对 / `column_default` 断言 / 部署文档补锁窗口 | **采纳**：V-3/V-4 覆盖前两条；第三条并入 §3 的 `08-部署运维.md` 改动 |
| B14 | 完整性 | 回填量级：`cmd/seed` 48 行资产；真实部署 NULL 行通常很少（NetBox 写非 NULL） | **采纳**：作为 §2.2 注释与结案说明的量化依据 |

### 7-C 否决（不改，附理由）

| # | 视角 | 主张 | 否决理由 |
|---|---|---|---|
| C1 | 完整性 | 「部分反对不做 G-21」——本轮就把 `null`/`""`/数组三种入参修掉 | 不改。`UpdateAsset` 是 `map[string]any` 直通 gorm（`handlers/asset_handler.go:113-119`），改语义要同时定「归一 vs 400」「部分更新里 null 的语义」，属独立需求 + 审查（用户工作流要求文档先行）。本轮范围已经含 A7（seed 退出码）这类扩围，再加会稀释验证深度 |
| C2 | 完整性 | 把 `tags`/`custom_fields` 改成 `NOT NULL` 以强制不变量 | 不改。全表校验 + 重写 = `ACCESS EXCLUSIVE` 长锁，对生产大表是停摆风险；且 `PUT {"tags": null}` 仍能绕过（应用层没拒），DB 约束换不来真不变量 |
| C3 | 完整性 | `ticket.Tags` 表示不一致本轮一起改 | 不改。它不是缺陷（Create 有效 + service 归一），改它要动 ticket 的写入路径，属另一模块 → G-23 |

### 7-D 审查期间新增的登记项

- **G-21** 资产 jsonb 入参规范化（`null`/`""`/JSON 数组 → 400 或归一；`Updates(map)` 语义）。
- **G-22** NetBox 同步 upsert 用 Go 字段名当列名 → `42703`（真调用路径上预查询先失败，见 F-6）。**已修**：`docs/FIX-PLAN-NETBOX-UPSERT.md`。
- **G-23** `ticket.Tags` 表示一致性（`default:'[]'` + service 归一的收敛）。

### 7-E 实施期间由退出码改动**连带暴露**的缺陷（新发现）

- **E1 · seed 演示资产 `asset_tag` 撞唯一约束（已修）**：`unique_asset` 在 000001 是
  `UNIQUE (asset_tag, idc_id)`，000013 把 `assets.idc_id` 改名为 `site_id`，约束随之变成
  **站内唯一**。而 seed 的 tag 只含 `site.Code`（`AST-DC-BJ-01-001`），同站点 4 个机柜生成同一个 tag
  → 每站点只有第一个机柜的 4 个资产能插入，其余 27 服务器 + 9 交换机全部 `23505 duplicate key`。
  G-20 之前这些失败只打日志、进程 exit 0，于是 **48 个演示资产实际只有 12 个**落库，CI 的
  `assets > 0` 断言照样通过。退出码改为非零后，本机真 PG 实验立刻复现：`❌ 初始数据有 36 处失败`。
  修复：tag 里加 `rack.Name`（与同表的 `SN`/`Name` 一致，早已含机柜名，只有 tag 是漏网）。
  验证：真 PG 全新库 → exit 0、48 资产、0 NULL jsonb、幂等重跑 exit 0；退回旧 tag → 单测红、真 PG exit 1。
- **E2 · 演示网络接口 IP 非法（登记，未修）**：`fmt.Sprintf("192.168.%s.10", rack.Row)` 里
  `rack.Row` 是字母（`A`/`B`），生成 `192.168.A.10`。000013 把 `asset_networks.ipv4_address`
  从 inet 改成 varchar 后不再报错，于是这类垃圾值静默入库（改名前它是硬失败）。
  影响面：演示数据的可信度（监控平台演示 IP 不可路由）；不阻塞 G-20，登记为 **G-24**。
