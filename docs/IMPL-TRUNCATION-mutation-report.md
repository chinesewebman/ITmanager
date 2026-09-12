# M33 步骤 6 变异反证报告 — M1-M11

**日期**：2026-09-12
**基线 commit**：`b0117af`（main 分支头，已领先 `e9bb06b` 5 个 commit）
**测试入口**：
- 纯 Go（无 PG）：`cd backend && go test ./internal/redact/... ./internal/integration/... -count=1` — 83 PASS 基线
- 真 PG：`cd .. && DOCKER='sudo -n docker' scripts/db_smoke.sh` — 37 PASS 基线

**门禁**：每条变异执行 5 步：(1) 仅修改 §7 列出的文件；(2) `go build ./...` 必须编译通过；(3) 跑目标测试集；(4) 记录预期红点与实际红点；(5) `git checkout -- <file>` 还原。报告以最终 baseline 仍 PASS、`git status` 干净收尾。

## 11 行变异表

| # | 变异内容 | 预期红点（§7） | 实际红点（实测） | 结果 |
|---|---|---|---|---|
| M1 | `redact.TruncateRunes` 改 byte：`return s[:max], true` | U1 的 **rune 计数**（中文夹具）；emoji 夹具另抓 `utf8.ValidString` | `TestTruncateRunes_按字符截断且输出合法UTF8`（中文 256 / emoji 256 子用例既报 rune 数 85/66 又报非法 UTF-8）+ `TestTruncateRunes_非法UTF8原样返回不做归一化`（快路契约失效） | **PASS** |
| M2 | truncate 去剥；`text` 改 `return v`；`sanitizeText` 改 `return s, false`（`redact` 仍被 TruncateRunes 引用 → 无 unused import） | U2 + U6 的 NUL 样本 | `TestSanitizeText_剥控制字符保留可见内容`（NUL/DEL/LF/CR/HTAB 全红）+ `TestFieldCounter_只剥不截进明细不进计数`（明细空）+ `TestLogFieldSanitization_有截断或剥离才打日志/只剥不截也要打`（无日志）。**dbsmoke 端：`TestDBSmoke_ThirdPartyFieldTruncation`**（含 NUL 的 description 整批回滚）也红 | **PASS** |
| M3 | `max <= 0` 分支改 `return "", false` | U1 的 `max<=0` 分支 | `TestTruncateRunes_按字符截断且输出合法UTF8`（"max 为 0 原样返回"/"max 为负原样返回" 子用例 — 期望 `"abcdef"`，实际 `""`）+ `TestTruncateRunes_非法UTF8原样返回不做归一化`（期望 `\xff\xfe`，实际 `""`） | **PASS** |
| M4 | `truncate` 里去掉 `c.truncated[field]++` | U3/U6 | 纯 Go：`TestFieldCounter_超长按字符截断并计数` / `TestFieldCounter_多字段累加` / `TestFieldCounter_同字段既剥又截` / `TestLogFieldSanitization_有截断或剥离才打日志/有截断打明细`。<br>**dbsmoke 端：`TestDBSmoke_NetBoxFieldTruncation` + `TestDBSmoke_ZabbixFieldTruncation` + `TestDBSmoke_ThirdPartyFieldTruncation`**（`fieldsTruncated > 0` 断言全红） | **PASS** |
| M5 | `SyncAll` 失败分支也写 `*_field_truncations`（同步写 `netbox_field_truncations=0` / `zabbix_truncated=0` + `zabbix_field_truncations=0` / `glpi_field_truncations=0`） | U8（`SyncAll` 层） | **dbsmoke：`TestDBSmoke_SyncAllFailureOmitsKeys`**（`assert.NotContains(t, k, "_field_truncations", ...)` 报三个键 `"netbox_field_truncations"` / `"zabbix_field_truncations"` / `"glpi_field_truncations"` + 对称的 `zabbix_truncated`） | **PASS** |
| M6 | NetBox 少包一个字段（`Brand: ""`） | U6 | **dbsmoke：`TestDBSmoke_NetBoxFieldTruncation`**（`brand=100` 断言失败，实测 `""`）+ **`TestDBSmoke_ThirdPartyFieldTruncation`**（同步路径：`name=256/255 brand=0/100 ...` 日志中 `brand` 落 0/100） | **PASS** |
| M7 | `colAlertTriggerName = 600`（偏大） | U7a + U7b + U6（旧代码式 22001 复现） | `Test列宽常量与模型size_tag一致/colAlertTriggerName`（500 ≠ 600）。<br>**dbsmoke 端：`TestDBSmoke_ZabbixFieldTruncation`**（`trigger_name=500` 期望，实测 501 完整入库）+ **`TestDBSmoke_ThirdPartyFieldTruncation`**（`trigger=501/500`）。<br>**注**：§7 写的 U7b 不抓（M7b 同）—— `TestDBSmoke_ColumnWidthMatchesConstant` 的 `Expect` 是硬编码字面量 500，与 `truncate.go` 常量**不交叉比对**；它是「DDL vs 字面量 500」而非「DDL vs 常量」。U7b 实际只能抓到 §3.2 那种 DDL 漂移，抓不到纯 Go 常量漂移。 | **PASS**（U7a + U6 部分，U7b 抓不到） |
| M7b | `colAlertTriggerName = 400`（偏小） | U7a + U7b + **U6 的 `==` 精确值断言** | `Test列宽常量与模型size_tag一致/colAlertTriggerName`（500 ≠ 400）。<br>**dbsmoke 端**：`TestDBSmoke_ZabbixFieldTruncation`（`trigger_name=500` 期望，实测 400 行内）+ `TestDBSmoke_ThirdPartyFieldTruncation`（`trigger=400/500`）。U7b 同 M7：不抓。 | **PASS**（U7a + U6 部分，U7b 抓不到） |
| M8a | `truncate` 恒把 `hit` 当 true（`_` 丢弃 `hit`，无条件 `c.truncated[field]++`） | U5 | `TestFieldCounter_未超长不计入且值不变`（`fc.count() == 0` 期望，实测 1）+ `TestFieldCounter_正好等于上限不计入`（边界 255 中 == 上限 → 期望 0，实测 1） | **PASS** |
| M8b | `truncate` 恒把 `hit` 当 false | U3/U6 | 纯 Go：`TestFieldCounter_超长按字符截断并计数` / `TestFieldCounter_多字段累加` / `TestFieldCounter_同字段既剥又截`（全 `count() == 0` 期望，实测 0）。<br>**dbsmoke 端：`TestDBSmoke_NetBoxFieldTruncation` + `TestDBSmoke_ZabbixFieldTruncation` + `TestDBSmoke_ThirdPartyFieldTruncation`**（`fieldsTruncated > 0` 断言全红） | **PASS** |
| M9 | 顺序反转：`TruncateRunes` 之前不剥、剥在之后 | U2 夹具 `("\x00"×9+"abcdefg", 10)`：正确 `"abcdefg"`，反转 `"a"` | 纯 Go：`TestFieldCounter_同字段既剥又截`（`"\x00"+"中"×300` 走 `colAssetName=255`：期望 255 rune，实测 254 — `\x00` 被算进配额后被剥掉）。<br>**注**：§7 列出的 U2 夹具 `("\x00"×9+"abcdefg", 10)` 在仓库里**不存在**（实测 `grep` 仓库 0 命中）。结构等价的红点 `TestFieldCounter_同字段既剥又截` 在位并命中。**dbsmoke 端不抓**：U6 fixture description 是 `"lead\x00trail"`（9 字符远未超 255）/ title 是 `"T"×256`（无控制字符），顺序反转在两个用例都得到相同结果。 | **PASS**（pure-go 等价夹具命中；§7 指定的精确夹具未落地） |
| M10 | `fc.text(...)` 改成 `fc.truncate(..., 255)` | U6 的 `description` 长度断言 | **不红**：`scripts/db_smoke.sh` 全部 37 用例 PASS，包括 `TestDBSmoke_ThirdPartyFieldTruncation`（`assert.Equal(t, "leadtrail", tkDesc)` 通过 —— description 是 9 字符，先剥 `\x00` 再 truncate(255) 与先 truncate(255) 后剥 `\x00` 在此 fixture 上结果完全相同："leadtrail"）。`scripts/db_smoke.sh` 全绿，PG 上 M10 在 description 上**无可见行为差**。 | **PARTIAL**（mutation 可编译、dbsmoke 全过；U6 fixture 没覆盖 M10 描述的实际失败形态） |
| M11 | worker `:110` 改回 `if _, _, err := ...`（丢弃计数），并把 `if ft > 0` 守卫包到 `if false {}` | **可抓**：`metric_sync_test.go` 用 `log.SetOutput` 捕获 + 假 Zabbix，断言 tick 日志含 `field_truncations=1` | **不红**：仓库内**不存在**这个用例 —— `metric_sync_test.go:288-320` 的 worker 测试基座（`TestMetricSyncWorker_StartStop幂等` / `Stop未启动` / `CtxCancel`）只断言 DB 行数 > 0，**无任何日志断言**。`scripts/db_smoke.sh` 全过（U6 走直调 `SyncMetricsFromZabbix`，不触发 worker 路径）。§7 行明确写"可抓"但**测试未落地**（文档原话："**M11 代码已接住计数但无日志断言**，改回 `if _, _, err :=` 不会红"——IMPL-TRUNCATION.md §11 / §10 M9 综述亦同）。 | **PARTIAL**（mutation 可编译；现有测试**不覆盖**该路径；red-point 在仓库内**未实现**） |

## 汇总

- **M1-M9**：9 条变异全部按 §7 表红（部分超出预期覆盖：如 M4/M8b 同时红 U3+U4+U6 三条 dbsmoke）
- **M10**：mutation 可编译，但 §7 列的 `description` 长度断言在当前 U6 fixture 上**无法红**（description 9 字符远未触发 truncate 上限，M10 与正常路径结果一致）。dbsmoke 全绿
- **M11**：mutation 可编译，但 §7 列的测试用例**仓库内未实现**（worker 日志断言缺失），现有全部测试 PASS

**11 条红点收口 9/11。** M10/M11 是 §7/§11 文档自己点名的已知抓不到 / 未落地项。M10 是 fixture 覆盖问题（U6 用例的 description fixture 不触发 truncate 路径），需要给 description 加一个 ≥256 字符的超长 fixture 才能让 M10 浮上来；M11 是测试缺失问题，需要照 §7 末行（`metric_sync_test.go` 用 `log.SetOutput` + 假 Zabbix + 断言 `field_truncations=1`）补一条 worker tick-log 用例。

## 状态

- 文档：`docs/IMPL-TRUNCATION-mutation-report.md`（本次新增）
- 代码：所有 M1-M11 变异均已 `git checkout -- <file>` 还原。`git status` 工作树干净
- main 分支相对 `e9bb06b` 父 commit 仍为 5 commit ahead（M33 步骤 1-5 + 步骤 6 不变），本次新增的为本次 commit（report-only）
- 测试基线：`go test ./internal/redact/... ./internal/integration/... -count=1` 两包 `ok` / `scripts/db_smoke.sh` ✅ 37 PASS