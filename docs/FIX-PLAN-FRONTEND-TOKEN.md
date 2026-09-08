# 修复需求文档：前端 6 页 token 失效（恒 401 + 静默回退 mock）

> 状态: **已实现（2026-09-09）** — 见 §8 实现记录与偏差
> 日期: 2026-09-09
> 基线: `main` @ b5c46fd
> 来源: `docs/FIX-PLAN-AUTHZ.md` §7「明确不做」单列任务；`docs/adr/0005` 已知缺口
> 关联: [FIX-PLAN-AUTHZ](FIX-PLAN-AUTHZ.md)、[ADR-0005](adr/0005-角色词表与权限矩阵.md)、[12-优化建议](../12-优化建议.md)

> **v2 修订说明**（三路审查：正确性/安全/一致性，全部只读）：修正 §1.1 行号偏移、§1.3 计数与症状描述；
> 新增 §3.4（Runbook 写操作谎报成功）、§7（部署前提）；
> §3.2 由「4 份副本各改一遍」改为**收敛到 `api.ts` 单一 helper**（副本已漂移过一次，见 §1.3）；
> §3.3 由「新增 fs 扫描测试」改为 **ESLint `no-restricted-syntax`**（原方案自命中 + 缺 `@types/node`，见审查记录）。

---

## 0. 一句话

6 个页面用 `localStorage.getItem('token') ?? ''` 拼 `Authorization: Bearer`，而该键自 C-F5 改用 httpOnly cookie 后**无人写入** → 请求恒 401 → `if (!res.ok) return MOCK` 静默回退演示数据。**页面看起来能用，实际从未连上后端。**

## 1. 现状实测（2026-09-09，命令复核）

### 1.1 受影响文件与调用点

| 文件 | 助手/内联 行号 | 调用点数 | 路径 |
|------|----------------|----------|------|
| `frontend/src/pages/AlertSuppressions.tsx` | apiGet 38-44 / apiSend 46-60 | 1 + 4 | `/alert-suppressions`（+`/preview`） |
| `frontend/src/pages/Oncall.tsx` | apiGet 35-40 / apiSend 42-52 | 3 + 4 | `/oncall/{current,schedules,policies}` |
| `frontend/src/pages/Runbook.tsx` | apiGet 38-44 / apiSend 46-56 | 2 + 3 | `/runbooks*` |
| `frontend/src/pages/MetricSnapshot.tsx` | apiGet 27-33 | 1 | `/metric-snapshots/latest` |
| `frontend/src/pages/Topology.tsx` | 内联 fetch 78-86 | 1 | **`/api/v1/topology`** |
| `frontend/src/pages/AssetTimeline.tsx` | 内联 fetch 150-156 | 1 | **`/api/v1/diagnostics/assets/:id/timeline`** |

`grep -rnE "(^|[^a-zA-Z_.])fetch\(" frontend/src` 全仓库命中 **9 处**，全部在上表 6 个文件内；其余页面走 `src/services/api.ts`（axios + `withCredentials`）。4 份 `apiGet`/`apiSend` 签名逐字相同（**共 18 个调用点，可零改动替换**）。

### 1.2 三个叠加的缺陷

| # | 缺陷 | 证据 |
|---|------|------|
| F-1 | **token 源已死** | `localStorage.setItem('token'` 全仓库 0 命中；C-F5 改为 httpOnly cookie（`auth_handler.go:106-115`），前端由 `withCredentials` 自动携带（`api.ts:17`） |
| F-2 | **路径前缀错误** | Topology / AssetTimeline 请求 `/api/v1/...`；后端无 `/v1` 挂载（`routes.go:182` `r.Group("/api")`），vite 代理无 rewrite（`vite.config.ts:14-19`）→ 未命中 handler。**dev/未构建 dist 时 404；生产构建下 `NoRoute` 回退 `index.html` 返回 200 HTML**（`routes.go:384`）。**来源**：历史文档 `TODO.md:151,153,156` 把端点记作 `/api/v1/...`（后端从未如此挂载），前端照抄 → 文档漂移传染到代码 |
| F-3 | **失败被 mock 吞掉** | 6 页均为 `if (!res.ok) return MOCK` / `catch { return MOCK }` → 401/404 静默显示演示数据，缺陷长期不可见 |

**根因机制（比「token 为空」更硬）**：`middleware/auth.go:83-98` 只要 `Authorization` 头非空就**不回退** cookie；空 token 产生字面量 `Bearer `（`parts=["Bearer",""]`）→ 直接 401。即「恒 401」成立；去掉该头后 cookie 回退恢复。

### 1.3 连带缺陷 A：共享 client 对 204 / 非 JSON 响应误报

`src/services/api.ts:22-27` 的响应拦截器：

```ts
const res = response.data;
if (res.code !== 0) { message.error(...); return Promise.reject(...) }
```

| 响应形态 | 现状 | 影响 |
|----------|------|------|
| **204 空 body** | axios 返回 `''` → `''.code` 为 `undefined !== 0` → **成功当失败** | 生产 **5 处** `c.Status(http.StatusNoContent)` 全中招：`oncall_handler.go:62,105,176`、`alert_suppression_handler.go:150`、`runbook_handler.go:93` |
| **`responseType:'blob'`** | Blob 无 `code` 字段 → 同样 reject | **今天就是坏的**：`api.ts:110-113`（`Alerts.tsx:187` 误报导出）、`api.ts:263-269`（`Assets.tsx:112` 报告下载） |

> 结论：**不先修 1.3，就无法把 6 页切到共享 client** —— 切过去后每次删除都会弹错误。§3.1 顺带修好两处下载功能（附带修复，非本模块目标）。

**连带缺陷 B（Runbook 独有）**：`Runbook.tsx:46-56` 的 `apiSend` 缺 `if (res.status === 204)` 分支（另外 2 份有），且写操作把失败谎报成成功：

```ts
// Runbook.tsx:104-106
await apiSend('DELETE', `/runbooks/${id}`).catch(() => null)
message.success('已删除（mock）')   // ← 无论成败都提示成功
refetch()                          // ← 无条件执行，列表行仍在
```

`DELETE /api/runbooks/:id` 挂 `canManage`（`routes.go:370`）：无 manage 能力的角色点删除 → 403 → 界面呈现「已删除」。AlertSuppressions / Oncall 的写操作是正确的（`try { await …; message.success } catch { message.error }`），**仅 Runbook 需修**。

## 2. 目标

1. 6 个页面走共享 axios client（cookie 鉴权 + 统一 401 处理），删除全部 `localStorage.getItem('token')`。
2. 修正 Topology / AssetTimeline 的 `/api/v1` 前缀（含 `AssetTimeline.tsx:3` 注释）。
3. 共享 client 对 204 / 非 JSON 响应不再误报（顺带修复两处 blob 下载）。
4. Runbook 写操作失败不再谎报成功。
5. 加回归守卫（CI 强制），防止再出现「手拼 Authorization」与「错误路径前缀」。

## 3. 方案

> **本节是初版方案，保留供对照；最终实现以 §8 为准**（审计轮对 ESLint selector、测试断言、错误提示做了修订）。

### 3.1 `src/services/api.ts` — 拦截器容错（最小改动）

```ts
// before
const res = response.data;
if (res.code !== 0) { ... }

// after：只有「对象且自有 code 字段的 JSON 业务包」才校验 code
const res = response.data;
if (
  res && typeof res === "object" &&
  Object.prototype.hasOwnProperty.call(res, "code") &&
  res.code !== 0
) {
  message.error(res.message || "请求失败");
  return Promise.reject(new Error(res.message || "请求失败"));
}
return response;
```

- `Object.prototype.hasOwnProperty.call` 而非 `'code' in res`：后者沿原型链判定（`Object.create({code:1})` 命中）；也非 `Object.hasOwn`（ES2022，tsconfig `lib: ES2020` 无类型）。
- 空字符串时 `res &&` 短路，不触发 `hasOwnProperty`（已实测：无异常）。
- 判定条件收窄为「对象 + 自有 code」，业务包（`{code:0,data}`）行为不变；数组 / Blob / `''` / HTML 一律放行。

### 3.2 收敛为单一 helper（`api.ts` 导出，4 页 import）

4 份副本逐字相同，且**已经漂移过一次**（Runbook 缺 204 分支）。按「复用已有 helper，而不是新建一个」：在 `api.ts` 导出唯一一份，调用点零改动。

```ts
// src/services/api.ts 末尾新增（签名与原页面副本逐字一致，故调用点不变）
export async function apiGet<T>(path: string): Promise<T> {
  const res = await api.get(path);
  return res.data?.data as T;
}

export async function apiSend<T>(method: string, path: string, body?: any): Promise<T> {
  const res = await api.request({ method, url: path, data: body });
  return res.data?.data as T; // 204 → undefined（拦截器已放行）
}
```

4 页改动：删本地副本 → `import api, { apiGet, apiSend } from '../services/api'`（MetricSnapshot 只用 `apiGet`）。

2 个内联 `fetch` 的页面：

```ts
// Topology.tsx（`/api/v1/topology` → `/api/topology`，参数 only_with_alerts）
const res = await api.get('/topology', { params: onlyWithAlerts ? { only_with_alerts: true } : {} })
return res.data?.data ?? MOCK_GRAPH

// AssetTimeline.tsx（`/api/v1/...` → `/api/diagnostics/assets/:id/timeline`，参数 days）
const res = await api.get(`/diagnostics/assets/${id}/timeline`, { params: { days } })
return res.data?.data ?? MOCK_TIMELINE
```

**读路径 mock 回退保持不变**（既有产品决策）；但 401 现在会触发 `dispatchAuthLogout` 跳登录（`api.ts:33-38`），不再无声无息。

### 3.3 回归守卫：ESLint `no-restricted-syntax`（`.eslintrc.cjs`）

CI 已跑 `npm run lint`（`--max-warnings 0`，`.github/workflows/ci.yml:117-121`），无需新增文件/依赖：

```js
'no-restricted-syntax': ['error',
  { selector: "CallExpression[callee.object.name='localStorage'][callee.property.name='getItem'] > Literal[value='token']",
    message: '鉴权已改为 httpOnly cookie，禁止从 localStorage 取 token；请用 services/api 的共享 client。' },
  { selector: "Literal[value='Authorization']",
    message: '禁止手拼 Authorization 头（auth.go 见到该头就不回退 cookie）；请用 services/api 的共享 client。' },
  { selector: "Literal[value=/^\\/api\\/v1\\//]",
    message: '后端无 /v1 前缀（routes.go 只挂 /api）；请用 /api/... 路径。' },
  { selector: "TemplateElement[value.raw=/^\\/api\\/v1\\//]",
    message: '后端无 /v1 前缀（routes.go 只挂 /api）；请用 /api/... 路径。' },
]
```

> 选择器有效性用临时 fixture 实测（见 §4 FV-6），不靠推测。
> 定位说明：这是**防误写的回归守卫，不是安全边界**（绕过只需变量间接）；它挡的是「类别」（手拼鉴权头、错误前缀），不是单条字符串。

### 3.4 `Runbook.tsx` 写操作：失败不谎报成功

对齐 AlertSuppressions / Oncall 的既有写法：

```ts
// before
await apiSend('DELETE', `/runbooks/${id}`).catch(() => null)
message.success('已删除（mock）')
refetch()

// after
try {
  await apiSend('DELETE', `/runbooks/${id}`)
  message.success('已删除')
  refetch()
} catch (e: any) {
  message.error(e?.message ?? '删除失败')   // 拦截器已提示 403/500，此处兜底
}
```

`onSubmit` 的两处 `.catch(() => null)` + `message.success('已更新（mock）')` 同样处理。

## 4. 验收

| # | 验收项 | 判定 |
|---|--------|------|
| FV-1 | 6 页不再手拼 token | `grep -rn "localStorage.getItem('token')" frontend/src` → 0 命中 |
| FV-2 | 路径前缀正确 | `grep -rn "api/v1" frontend/src` → 0 命中（含注释） |
| FV-3 | 204 不误报 | `api.test.ts` 新增：`data: ''` → resolve，`message.error` 未调用 |
| FV-4 | 拦截器新条件分支全覆盖 | 新增 3 例：`data: {}`（对象无 code）→ resolve；`data: new Blob()` → resolve；`data: {code:1}`（自有 code）→ reject（既有用例） |
| FV-5 | 请求不再带 Authorization、URL 正确 | `api.test.ts` 新增 `apiGet`/`apiSend` 用例：mock adapter 断言收到 `url: '/alert-suppressions'` 且 `headers.has('Authorization') === false`（**必须用 `has()`**，见 §8 审计轮第 1 条） |
| FV-6 | 守卫真的会红 | 临时 fixture 逐类违规跑 eslint，确认报错后删除 fixture（反证，不留文件；实际覆盖 8 类，见 §8） |
| FV-7 | 既有测试全绿 | `npm test -- --run`、`npm run lint`、`npx tsc --noEmit` |

## 5. Risk

| # | 失败模式 | 缓解 |
|---|---------|------|
| R-1 | **切换到 axios 后错误语义变化**：原来 `!res.ok → MOCK` 不抛异常，现在 4xx/5xx reject + 全局 toast | 保留每页 catch/mock 结构；**注意 6 页既有测试全部 `vi.mock('../hooks/useApiQuery')`，恰好 mock 掉被改的请求层 → 它们改对改错都绿，不构成保护**；真实保护是 FV-3/4/5 的新用例（对旧实现会红） |
| R-2 | **拦截器放宽后，非标准响应（对象但无 code 字段）不再报错** | 判定收窄为「对象 + **自有** code」；`api.test.ts` 既有 `code != 0` 用例仍必须绿；副作用是两处 blob 下载恢复（期望方向） |
| R-3 | ~~5xx 重试次数变化~~ **不成立**（审计实测）：6 页读路径的 queryFn 全部 catch 后 **resolve**（返回 MOCK），从不 reject → `useApiQuery.ts:52-58` 的 5xx 重试不触发，请求次数与改前一致 | — |
| R-4 | **Topology / AssetTimeline 修正路径后，真实数据形状与 MOCK 不同** → 渲染报错 | 已核对：`AssetTimeline.tsx:168` 的 `tl.asset` 无 `?.`，但后端 `DiagnosticTimeline.Asset` 在 200 分支必然填充（`diagnostic_service.go:233-240`），资产不存在走 404 → 当前不会白屏；Topology 的 `nodes/stats` 由服务端 `make([]...,0)` 保证非 null（`topology_service.go:118,141`）。**属残留脆弱点**（若后端改为可空即崩），非本次回归 |
| R-5 | **静态守卫过严**，未来合法用法被挡 | 断言限定「localStorage 取 token」「字面量 Authorization」「/api/v1 前缀」三类；豁免方式是在 `.eslintrc.cjs` 显式改规则（走 code review） |
| R-6 | **dev 环境明文 HTTP 传 cookie**：`secure := mode == "release"`，本仓 `config.yaml` 是 `mode: debug` | 见 §7 部署前提；代码层不改（属独立任务，见 §6） |

## 6. 明确不做（记录，不静默）

| 不做 | 理由 |
|------|------|
| 去掉 mock 回退 / 加「演示数据」提示 | 既有产品决策，属独立 UX 任务 |
| 把 6 组端点补进 `api.ts` 的 `xxxApi` 命名空间 | 可选重构，超出本轮最小改动范围（现有 14 个 namespace 无一个覆盖这些端点） |
| 前端消费 `/auth/me` 的 `capabilities` 做按钮级隐藏 | 属 AUTHZ 前端后续任务（需先补 openapi spec） |
| 后端 `Logout` 硬编码 `secure=false`（`auth_handler.go:143`）与 Login 判定不一致 | HTTPS 下仍能清 cookie，无功能缺陷；记入 TODO |
| `secure` 改为按请求 scheme 判定 | 后端行为改动，超出本模块；见 §7 |
| `NoRoute` 对未知 `/api` 路径回退 `index.html`（生产 200 HTML） | 独立后端任务；本轮仅让拦截器不再把 HTML 当错误 |
| 登录 CSRF / 同站子域越权 | `SameSite=Strict` 已挡经典跨站 CSRF（跨站请求不带 cookie → `auth.go:101-107` 401）；同站子域与 login CSRF 取决于部署形态，**非本次改动引入**，记入 ADR 缺口 |

## 7. 部署前提（写进交付说明）

- **生产必须 HTTPS + `server.mode=release`**：否则 cookie 缺 `Secure`，且 JWT（24h、无黑名单，`auth_handler.go:139`）被嗅探后无法吊销。
- 若部署在同站子域下（`*.company.com`），`SameSite=Strict` 不防子域发起的请求，需靠网络层隔离或改 `SameSite=Lax` + CSRF token（独立评估）。

---

## 8. 实现记录（2026-09-09）

改动 12 个文件（含 3 处文档同步），`+163 / -104`；新增 `docs/FIX-PLAN-FRONTEND-TOKEN.md`：

| 文件 | 改动 |
|------|------|
| `frontend/src/services/api.ts` | 拦截器容错（§3.1）+ 导出 `apiGet`/`apiSend`（§3.2） |
| `frontend/src/services/api.test.ts` | +4 用例（204 / 对象无 code / blob / helper 断言 withCredentials 且无 Authorization 头） |
| `frontend/src/pages/{AlertSuppressions,Oncall,MetricSnapshot}.tsx` | 删本地副本 → import 共享 helper（调用点 18 处零改动）；前两页补「API 错误不重复 toast」 |
| `frontend/src/pages/{Topology,AssetTimeline}.tsx` | 内联 fetch → `api.get`，修 `/api/v1` 前缀（含 `AssetTimeline.tsx:3` 注释） |
| `frontend/src/pages/Runbook.tsx` | 写操作失败不谎报成功（§3.4）+ 表单校验失败不误报 |
| `frontend/.eslintrc.cjs` | `no-restricted-syntax` 守卫（§3.3） |
| `TODO.md` / `docs/FIX-PLAN-AUTHZ.md` / `docs/adr/0005-*.md` | 该缺陷状态同步为「已修复」 |

### 与文档的偏差（均已实测确认）

1. **Topology / AssetTimeline 加了 `try/catch`**：§3.2 片段只写了 `?? MOCK_GRAPH`，但 axios 对 404/500 是 reject 而非 `!res.ok`，不 catch 会让错误逃到 react-query 的 error 状态、mock 回退失效。已在实现中补上。
2. **`MOCK_EVENTS` 显式标注 `TimelineEvent[]`**：改用 `res.data?.data as TimelineResponse` 后，mock 的 `kind` 被推断为 `string` 不再兼容联合类型（原先靠 `any` 掩盖）。一行类型标注，未改数据。
3. **守卫 selector 用 `Identifier[name='Authorization'], Literal[value='Authorization']`**：初版只写 `Literal[...]`，实测**漏掉** `headers: { Authorization: ... }`（对象键是 Identifier 不是 Literal）——即漏掉本模块要防的那个原始写法。已修并实测。
4. **api.ts 注释不含 `localStorage.getItem('token')` 字面量**：否则 FV-1 的 grep 会被自己的注释命中（验收判据要求 0 命中）。

### 审计轮修复（2026-09-09，两路只读审计）

审计确认「行为等价性 / mock 回退无逃逸 / 拦截器形态完备 / 无漏改」全部成立，**无阻断项**；采纳 4 条：

1. **[重要] 新增的 Authorization 断言原本是空转**：初版写 `expect(config.headers?.authorization).toBeUndefined()`——实测 AxiosHeaders **只在 `has()`/`get()` 上大小写不敏感**，属性访问 `headers.authorization` 拿不到以 `Authorization` 存的头。即「有人给共享 client 手拼 Authorization」这条断言照样绿。改为 `expect(config.headers.has("auth" + "orization")).toBe(false)`（拼接是为避开本仓 ESLint 规则）。
2. **[次要] Runbook `onSubmit` 补 `if (e?.errorFields) return`**：否则表单校验失败会弹「提交失败」（见上「附带修复」）。
3. **[次要] Runbook 写失败双 toast**：拦截器已弹中文，页面 catch 再弹一条英文 axios 文案（`Request failed with status code 403`）。catch 改为 `if (e?.isAxiosError) return`（拦截器对所有 axios 错误都提示过）。
4. **[次要] ESLint 守卫两处漏网**（均实测）：`authorization` 小写写法不命中 → selector 加 `/i`；`'/api/v1'`（无尾斜杠）不命中 → 改为 `/^\/api\/v1(\/|$)/`。修后 7 类违规 fixture 全部报错、`authorized`/`AuthorizationService` 不误伤。

**记录不修**：`AssetTimeline.tsx:168` 的 `tl.asset` 缺 `?.`（见 §5 R-4，属残留脆弱点）；ESLint 守卫对「拼接字面量」（`'/api' + '/v1/x'`）与「变量键」（`getItem(KEY)`）不设防——它是防误写守卫，不是安全边界（§3.3）。

### 审计轮 2 修复（安全 / 可维护性方向）

1. **[重要] storage 守卫漏等价写法**：`callee.object.name='localStorage'` 只匹配裸标识量，实测 `window.localStorage.getItem("token")`、`sessionStorage.getItem('token')` 均不命中。去掉对象约束 → 三写法全中，且 `getItem('user')` 不误伤。
2. **[次要] 双 toast 的定性修正**：审计指出改前这两页走裸 fetch、**没有拦截器 toast**，所以「拦截器中文 + 页面英文 axios 文案」是**本次改动引入的**（我在上一版文档里误记为既有行为）。已在 AlertSuppressions（3 处）与 Oncall（4 处）的 catch 补 `if (e?.isAxiosError) return`，与 Runbook 对齐——API 错误只由拦截器提示一次。
3. **[次要] 守卫正则补 `/i`**：`'/API/V1/x'` 现在也命中。
4. **[次要] `AssetTimeline.tsx` 导入顺序**对齐「外部包在前」。

### 验证结果

| 项 | 结果 |
|----|------|
| FV-1 / FV-2 | `grep -rn "localStorage.getItem('token')\|api/v1" frontend/src` → 0 命中 ✅ |
| FV-3 / FV-4 / FV-5 | `npx vitest run src/services/api.test.ts` → 13 passed ✅ |
| FV-6 反证（变异） | 把拦截器回退为 `res.code !== 0` → 3 个新用例**变红**（204 / 对象无 code / blob）；ESLint 守卫用临时 fixture 实测 7 类违规（含小写 `authorization`、无尾斜杠 `/api/v1`）全部报错、`authorized`/`AuthorizationService` 不误伤，fixture 已删 ✅ |
| FV-7 | `npx tsc --noEmit` 0 error；`npm run lint` 0 error 0 warning；`npx vitest run` → **26 files / 160 tests passed** ✅ |

### 复现（本地）

```bash
cd frontend && npm run lint && npx tsc --noEmit && npx vitest run
```

### 附带修复

`Runbook.tsx` 的 `onSubmit` 缺少 `if (e?.errorFields) return`（AlertSuppressions / Oncall 都有），表单校验失败会弹「提交失败」。与本次改动无关的既有缺陷，但就在被改的同一个函数里、且属同一类「副本漂移」，顺手对齐（1 行）。

### 遗留（未修，见 §6）

无新增遗留；§6 记录的 7 项仍按独立任务跟踪。
