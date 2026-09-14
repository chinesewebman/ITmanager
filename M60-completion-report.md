# M60-completion-report — G-Utils-ValidatorsShared + G-BE-HttpsWhitelist（T-56 / T-71 结案）

**Shipped**: 2026-09-15（commits `314d47b` feat frontend validators / `a3867ff` refactor Settings import / `64c9623` feat backend 白名单 / `549ddb5` test backend；docs 本次）
**Scope**: frontend 3 files + backend 2 files
**摩擦**: M59 留下的两条债 —— (1) 前端规则住在 `pages/Settings.tsx`，跨页复用只能 import 一个 page 文件；
(2) 后端 `binding:"url"` 是形状检查不是 http(s) 白名单，绕开前端的调用路径能写进 `ftp://` 集成地址。
**Time**: ≤2h omp round

## 改动

| 文件 | 改动 |
|---|---|
| `frontend/src/utils/validators.ts`（新增） | `URL_PATTERN` / `EMAIL_PATTERN`（逐字照搬）+ `urlRules` / `emailRules` / `portRules` + `arrayOfPatternRules(pattern, label, requiredMessage?)` |
| `frontend/src/pages/Settings.tsx` | 删本地 6 个定义改 import；`toRules` 在页面内由 `arrayOfPatternRules(EMAIL_PATTERN, '邮箱', '请输入收件人')` 构造；10 处 Form.Item 引用与文案不变 |
| `frontend/src/pages/Settings.test.tsx` | pattern import `"./Settings"` → `"../utils/validators"`（1 行） |
| `backend/internal/api/handlers/integration_handler.go` | `isHTTPURL` + 3 个 handler 入口校验；`URL` 字段 `binding:"required,url"` → `binding:"required"` |
| `backend/internal/api/handlers/integration_handler_test.go` | 新增 `TestUpdateIntegrations_非HTTPScheme_返400`（3 × 3 表驱动）+ M59 两条用例注释更新 |

## Hard pass

| 维度 | 结果 |
|---|---|
| frontend `npx tsc --noEmit` | **0 error** ✓ |
| frontend `npx vitest run src/pages/Settings.test.tsx` | **58 tests PASS** ✓（无退化） |
| frontend 全量 `npx vitest run` | **42 files / 409 tests PASS** ✓（M59 同基线，零退化） |
| backend `go test -count=1 ./...` | **27 packages ok（0 fail）** ✓ |
| eslint（改动 3 文件，`--max-warnings 0`） | 干净 ✓ |
| `gofmt -l`（改动 2 文件） | 无输出 ✓ |
| mutation inversion（后端短路 3 处 scheme 闸 → 恒假） | **9/9 新 sub-case FAIL** + `TestUpdateIntegrations_非URL_返400` 3/3 FAIL；还原后全绿 ✓ |
| 双轨分析 | graphify 6843 nodes / 13936 edges / 445 communities，0 anomalies；codegraph 新符号入图（见 `M60-graph-analysis.md`）✓ |

## 关键设计决策（含理由）

### 1. 后端**替换**而非叠加校验（T-56 的修法）
`binding:"url"` 留着、再加一条 scheme 检查，会得到两条语义不同的规则并存 —— 一旦有人只看到
binding 那条，就会重现「以为后端已做 http(s) 白名单」的误判（T-52 同族：校验器接受的形态必须有
消费者按该形态消费）。故：`URL` 字段 binding 收敛为 `required`（只管空值），scheme 判定在 handler
入口显式一行。**顺序也要紧**：校验在写内存 cfg 之前 —— 新测试的 `assert.Empty(cfg.Integrations.X.URL)`
就是钉这一点（400 且配置不变，而不是「先写坏再报错」）。

### 2. `isHTTPURL` 用 `url.Parse` 而不是字符串前缀
`strings.HasPrefix(raw, "http://")` 会接受 `http://`（无 host）并拒绝 `HTTP://host`（scheme 大小写不敏感，
RFC 3986 §3.1）。`url.Parse` 顺带把 scheme 规范化成小写，host 判据也保留原 `url` tag 的语义。
只放行 `http` / `https` 两个字面量 —— `ws://` / `wss://` 在 brief 的 out-of-scope 里。

### 3. `arrayOfPatternRules` 的第三参不是「多余的灵活度」
brief 给的签名是 `(pattern, label)`，按字面实现会把收件人字段的必填文案改成「请输入邮箱」，
而 M6 用例断言的是「请输入收件人」（label 就是「收件人」，不是「邮箱」）。**这是 brief 的笔误**，
两条路：改断言（测试退化一档，且文案确实变差）或加可选参（保住原语义）。
选后者：默认值仍是 `请输入${label}`，只有收件人这一个字段传自定义文案，签名向后兼容。

### 4. 前端两个文件同 commit（refactor 拆不开）
`Settings.test.tsx` 从 `"./Settings"` import `URL_PATTERN` —— 只改页面会让 tsc 直接报错。
M59 retro 的「绿 commit 拆分需要先截断再加回」在这里没有中间态（新增模块的 commit 1 已是绿的，
搬家这一步天然原子），故 refactor + test 迁移合成一笔。

### 5. `URL_PATTERN` / `EMAIL_PATTERN` 字符零改动
只搬运不重写：现有 pattern 经 M59 实战测试（T-65 实证过正/负样本），改动即引入未测语义。
`Settings.test.tsx` 的 16 条 pattern 边界在搬家后**未改一字**且全绿 —— 这就是「搬迁而非重写」的证据。

## 学到 / Retro

- **「搬迁」类重构的验收标准是测试零改动**：本轮前端 58 条测试里只改了 1 行 import，
  其余断言逐字未动仍全绿 —— 比「测试也改过了所以绿」强得多的证据。
- **同名不同集是跨语言协作的默认状态**：gin 的 `url` 与前端 `^https?://` 都叫 URL 校验，集不同。
  修法应是**收敛成一条规则**，而不是「两边各写一条、尽量写得像」。
- **mutation 打在新建的闸上**：把 3 处 `if !isHTTPURL(...)` 短路成恒假，等价于回到 M59 的
  `url`-tag-only 行为 → 新 9 个 sub-case **和** M59 的 3 个 non-URL sub-case 一起红。
  两层用例红在同一道闸上说明它们覆盖的是同一条规则的不同输入 —— 是有意的（一条新用例集
  + 一条既有回退用例），不是重复劳动。
- **观测（未复现，如实记录）**：重构后第一次跑 `Settings.test.tsx` 时出现 1 failed / 57 passed，
  该次运行与 eslint + 两次 `go test` 编译**并发**（N97 4 核，套件本身要 60s+）。
  随后单独重跑 58/58 PASS、verbose 全 ✓、全量 409/409 PASS。未能复现，未定位，登记为
  资源竞争下的偶发；如需结论需专门在压测下复跑（本轮未做）。

## Follow-up（留 future round）

- **其他页面接入 `utils/validators`**：Oncall URL / Runbook webhook / AssetForm IP 本轮**刻意不碰**
  （brief 只搬 Settings 一处）；各页有自己的 M6 文案，接入需逐页评估文案变更面。
- **`ws://` / `wss://`**：若将来有 WebSocket 类集成目标，`isHTTPURL` 与 `URL_PATTERN` 需同步扩集
  （两侧必须同改，否则重现 T-56 的「两侧不同集」）。
- **实时 URL 探活校验**：保存时试探连通性（注意 SSRF 面：内网地址是常态，不能简单地拒内网）。
- **SMTP user 强 email 的 SASL 误伤**（M59 登记，仍未解）：SendGrid `apikey` 类用户名会被 `emailRules` 挡。
- **F-6 Settings Token 留空语义 / F-7「测试连接」/ F-9 保存按钮 loading**：M57 登记的同族摩擦，未在本轮 scope。
