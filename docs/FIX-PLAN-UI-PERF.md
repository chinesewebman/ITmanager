# 前端易用性 / 性能 修复需求文档（FIX-PLAN-UI-PERF）

- 状态：**rev5**（并入 UI/UX 审美审计 + 文档事实审查 + 前端性能审计 + 后端性能审计；rev5 补 W2 遗漏站点）
- 触发：主人 2026-09-09 指令「记得运用codegraph啊，继续修复问题，改善表现、提高ui审美和易用性」
- 关联：`TODO.md`、`docs/TRAPS.md`、`docs/FIX-PLAN-FRONTEND-TOKEN.md`
- 索引工具：`codegraph_explore`（projectPath `/root/work/itmanager`）

---

## 0. 结论速览与批次

| 批次 | 内容 | 项数 | 状态 |
|---|---|---|---|
| **批 1（本轮）** | W1 假数据 → 错误态；W2 时间统一；W3 趋势角标；W4 批 1（H1/H6/H8/H9/H10/M4/M5/M6/M9/M10）；**W5 性能 P1 gzip / P2 echarts-core / P3 命令面板按需** | ~17 | 实施中 |
| 批 2（下一轮） | M1 标题体系、M2 排序、M3 分页 + **P4 表格 memo 化（必须与 M3 同批）**、M11 严重度、M13 移动端、M14 批量操作、M15 空态/加载态 | 8 | 待排期 |
| 登记不做 | L5/L6/L7 死代码清理、M7 的结构化编辑器重构、资产 IP 数值排序、P8 manualChunks、P10–P12 微优化 | 见 §6 |

**批 1 的选取标准**：用户会真实踩到、修法明确且局部、可单测 + 变异钉住。批 2 多为「一致性重构」，需要一次小重构，放下一轮避免单次改动过大。

---

## W1 前端假数据兜底 → 显式错误态（HIGH）

### 1.1 现象（修正版，按兜底**站点**计约 34 处）

> rev1 误记「11 页 12 处」——12 是表格行数。审查复核后实际兜底站点约 34 处，且 `Racks.tsx` 用小写 `mockRacks/mockDevices`（`grep MOCK_` 抓不到）。

| 页面 | 位置 | 行为 |
|---|---|---|
| Dashboard | `pages/Dashboard.tsx:100` | 「最近告警」卡片**无条件**渲染 `MOCK_RECENT`（4 条虚构告警），与接口完全无关 |
| Dashboard | `:51` `:59` `:74-75` | stats/trends 空值回落 `MOCK_STATS`（156 资产 / 8 告警 / 23 工单）/ `MOCK_TRENDS` |
| Dashboard | `:82` | 趋势点 <6 时 delta 硬编码 `5`（虚构趋势，见 W3） |
| Alerts | `:94-95` `:203-204` | items/stats 空值回落 `MOCK_ALERTS` / `DEFAULT_STATS`（常量在 `:20` `:60`） |
| Assets | `:159` `:238` | 回落 `MOCK_DATA`；副标题按假数据计数 |
| Tickets | `:58` `:74` | 回落 `MOCK_TICKETS`；统计卡**无条件**用写死的 `DEFAULT_STATS`（`:20`）——性质比兜底更严重 |
| Oncall | `:51` `:75` `:119` + 解构兜底 `:53` `:76` `:120` | `queryFn` 内 `catch { return MOCK_* }` |
| AlertSuppressions | `:49` `:52` | 解构默认值 + queryFn catch，双重 |
| MetricSnapshot | `:40` | `.catch(() => MOCK_LATEST)` |
| Racks | `:48` `:52`（sites）+ **`:59` `mockRacks()`** + **`:70` `mockDevices()`** | 四处，后两处为小写命名 |
| Topology | `:84` `:85-86` `:93` | 回落 `MOCK_GRAPH` |
| Runbook | `:52` `:240` | 回落 `MOCK_RUNBOOKS` / `MOCK_RECOMMEND` |
| AssetTimeline | `:153` `:155`（渲染层 `:165` `:166`） | 回落 `MOCK_TIMELINE` / 字段级 `MOCK_SUMMARY` |

**不删的「0 值默认」**（审查确认非假数据）：`components/TicketStatsCards.tsx:21` 的 `DEFAULT_STATS`（标签/配色/占位 0，`:35` 无条件 map——但**数据来源**是 W1 要修的）、`hooks/useDocumentTitle.ts:15` 的 `DEFAULT_TITLE`。

### 1.2 为什么这是缺陷（不是「演示数据」）

1. **误导运维判断**：界面显示从未发生的告警、资产台数、值班人。
2. **把失败伪装成成功**：`queryFn` 内 `catch` → React Query `isError` 恒 `false`：错误态永不出现、`retry`（5xx 重试 2 次）形同虚设、假数据进缓存（30s / Dashboard 60s）。
3. **空态与错误态不可区分**：后端真返回 `[]` 与后端 500 渲染成同一屏假数据。

### 1.3 修法（含审查补出的数据源与守卫问题）

| 步骤 | 内容 |
|---|---|
| 1 | 新增 `components/ErrorState.tsx`：`{ title?, status?, onRetry?, compact? }`，antd `Result` + 「重试」按钮，按状态码分流文案（401/403/404/5xx/网络） |
| 2 | 删除全部 ~34 处兜底与 `MOCK_*`/`mock*` 常量（含 `Racks` 的 `mockRacks`/`mockDevices`、`AssetTimeline` 的渲染层与字段级兜底） |
| 3 | 各页补 `isLoading` / `isError` 解构（**当前 0 个页面解构 `isError`**；`Oncall` 三处、`AlertSuppressions`、`Racks` 的 sites/devices、`Runbook` 的 recommend、`Dashboard` 的 stats/trends 连 `isLoading` 都没有） |
| 4 | **错误态按「区块」而非整页**：Dashboard 的 KPI（独立 queryKey `['dashboard','kpis']`）与 Racks 的 sites 下拉不得被主列表错误态一起替换 |
| 5 | **补空态**（审查指出这 4 处当前没有 `EmptyState`）：Dashboard 最近告警、Oncall 的 schedules/policies、MetricSnapshot（已查询但 0 行）、Racks（`RackGrid` 空数组渲染空 `Row`，整屏空白） |
| 6 | **补 undefined 守卫**：删兜底后「200 + 空 data」会 TypeError 白屏，而不是走 isError。逐点加固：`Alerts.tsx:211` `stats.problem`、`:247` `AlertStatsCards` 的 `stats[c.key]`、`Topology.tsx:94` `graph.nodes.length`、`AssetTimeline.tsx:176` `asset.name` / `:184` `summary.window_days`、`Assets.tsx:238` `(data ?? MOCK_DATA).length` |
| 7 | **两个缺数据源的点**（审查发现，rev1 自相矛盾）：<br>① Dashboard「最近告警」——后端 `/dashboard/*` 只有 stats/trends/kpis（`routes.go:351-355`），**无 recent 接口**；复用既有 `GET /alerts?limit=5`（`routes.go:316`，服务端已按 `created_at DESC, id DESC` 排序，`alert_service.go:134`），字段映射 `RecentAlert{host,message,severity,time}` → `Alert{host,message,severity_name,created_at}`。<br>② Tickets 统计卡——后端**无 `/tickets/stats`**（`routes.go:335-340` 只有 list/get/create/update），`queryKeys.tickets.stats()`（`useApiQuery.ts:28`）定义了但无调用方；改为从**未筛选**的列表推导（`ticketApi.list({page_size:500})`），并把档位从「pending/inProgress/waiting/resolved」（`TicketStatsCards.tsx:21` 的写死 `DEFAULT_STATS`）改成契约状态域。**rev4 修正**：原定「四档 open/in_progress/resolved/closed」的依据是 `models/ticket.go:18` 注释，该注释漏了 `pending` —— 契约 `openapi.yaml:2371` 的 status enum 是 **5 档**（含 `pending`），且 `integration/glpi.go:157` 会把 GLPI 状态 3 映射成 `pending`。按四档做会**静默漏掉 GLPI 同步的待定工单**，故实际实现为 5 档。`TicketStatsCards.tsx` 的取值需加守卫（否则 undefined 直接崩） |

**不做**：不改后端接口；不加「演示模式」开关（演示用 `cmd/seed` 数据）。

### 1.4 决策记录：为什么不采纳「保留 mock + 演示数据横幅」

UI 审计（H2）建议保留兜底、加 `<DemoDataBanner />` 提示。**不采纳**，理由：

1. 运维看板上带横幅的假数字照样会被当成真数字读——故障处置时读错一个数比看到空白代价大得多；
2. 保留 `catch` 等于继续让 React Query 的 `isError`/`retry` 失效（§1.2 第 2 条），横幅只是把静默失败变成「有提示的静默失败」；
3. 演示需求已由 `cmd/seed` 的真实数据满足。

### 1.5 Risk（多文件改动，给 3 个失败模式 + 缓解）

| # | 失败模式 | 缓解 |
|---|---|---|
| R1 | 某页测试依赖 hook 返回值形态（`Dashboard.test.tsx:21-29` 对非 trends 的 key 恒返回 `mockStats` **对象**）；新增 recent-alerts query 后 antd `List` 拿到非数组 → 用例红 | 改造前先把各页 mock 改成可注入 `isError/error/refetch` 的形态；`test/mocks.ts:6` 已有 `mockUseApiQueryDefault()`（含 `isError`）但目前**全仓无人引用**，改造成可传参版本复用 |
| R2 | 删常量后遗留引用 / 死 import → 编译红 | `npx tsc --noEmit` + `npm run lint` + `grep -rn "MOCK_\|mockRacks\|mockDevices\|DEFAULT_STATS" frontend/src --include=*.tsx --include=*.ts`（排除 `*.test.*`）复核为 0 |
| R3 | 403/404 被渲染成「加载失败」并诱导重试 | `ErrorState` 按状态码分流；403 文案为「无权限查看该数据」；重试按钮只 `refetch()`，不自动轮询 |
| R4 | 「200 + 空 data」不走 isError → 白屏 | §1.3 步骤 6 的逐点守卫 + 用例覆盖（hook 返回 `{data: null, isError: false}`） |

### 1.6 验证

- 每页新增用例：hook 返回 `{ isError: true, error: { response: { status: 500 } } }` → 断言错误态标题与「重试」按钮存在，且**断言页面不含**该页原 mock 文案（如 `'web-server-01'`、`'共 156 台'`）。
- 每页再加一条「200 + 空 data 不白屏」用例。
- 变异反证（每条必须红在断言上）：删某页 `isError` 分支 / `ErrorState` 换成 `null` / 把 `MOCK_*` 加回并接入 / 去掉某处 undefined 守卫。
- 全局：`npx vitest run`、`npx tsc --noEmit`、`npm run lint` 全绿。
- **测试基数更正**：使用 `useApiQuery` 的页面测试是 **11 个**（`Login`/`ChangePassword`/`Settings` 不用该 hook，rev1 误记为 14）。

---

## W2 时间显示统一（MED）

### 2.1 现象（修正版）

| 位置 | 现状 |
|---|---|
| `components/AlertTable.tsx:63-68` | `created_at` **无 `render`、无 `sorter`**，原样渲染 RFC3339。**注**：rev1 说「列宽 160 会截断」不准确——该列未设 `ellipsis`，实际是换行 |
| `components/TicketTable.tsx:57` | 同款原样渲染 + `width: 160`（rev1 遗漏） |
| `components/TicketDetailModal.tsx:70` `:74` | `{ticket.created_at}` / `{ticket.updated_at}` 裸渲染（rev1 遗漏） |
| `pages/Settings.tsx:923` | `new Date(t).toLocaleString()`（无 locale） |
| `pages/AssetTimeline.tsx:122` | `toLocaleString('zh-CN', { hour12: false })` |
| `pages/Oncall.tsx:62` | `toLocaleString('zh-CN')`（默认 hour12） |
| `pages/MetricSnapshot.tsx:128` | `new Date(v).toLocaleString()`（无 locale）——**rev5 补登**，rev1/rev2 两轮盘点均遗漏 |
| `pages/Dashboard.tsx:37-42` | 虚构的「10分钟前」字符串（W1 一并改） |

`dayjs` 已在 `package.json:25`，全仓零使用。

### 2.2 修法

新增 `frontend/src/utils/time.ts`（唯一时间格式化出口）：

```ts
formatDateTime(iso?: string | null): string       // 'YYYY-MM-DD HH:mm:ss'，空/非法 → '—'
formatRelativeTime(iso?: string | null): string   // '3 分钟前'，空/非法 → '—'
```

- 依赖 dayjs（已在依赖内，不新增包）；`dayjs.locale('zh-cn')` 在模块内设置一次。
- 应用点：`AlertTable` / `TicketTable` / `TicketDetailModal` / `Settings:923` / `AssetTimeline:122` / `Oncall:62` / `MetricSnapshot:128`（rev5 补）/ Dashboard 最近告警。

### 2.3 时区确认（审查要求先确认）

后端 `created_at` 是 Go `time.Time`（`models/alert.go:54`、`models/ticket.go:34`），JSON 序列化为 RFC3339 带时区偏移 → dayjs 解析后转浏览器本地时间，**语义正确**，无需 `dayjs.utc()`。前端 mock 里的 `'2026-02-14 10:00:00'`（无时区）只是 mock，随 W1 一起删除。

### 2.4 Risk

| # | 失败模式 | 缓解 |
|---|---|---|
| R5 | 非法/空时间串导致 `Invalid Date` 渲染或抛错 | `utils/time.ts` 内 `dayjs(iso).isValid()` 校验，否则 `'—'`；用例覆盖 `''` / `null` / `'not-a-date'` |
| R6 | 测试对「当前时间」敏感（`fromNow` 漂移） | `vi.useFakeTimers()` + `vi.setSystemTime('2026-09-09T12:00:00+08:00')` 断言固定文案 |

---

## W3 趋势角标方向恒为下降 + 虚构 delta（MED）

### 3.1 现象

- `components/DashboardCards.tsx:51-57`：`ArrowDownOutlined` 写死（`:54`）、绿色 `#52c41a` 写死（`:53`）、数值取 `Math.abs()` → 告警**上升**时显示「绿色 ↓ 5」，语义与事实相反。
- `pages/Dashboard.tsx:82`：趋势点 <6 时 `delta = 5` 是**虚构值**——只修组件方向，假趋势仍在（审查必修点 10）。
- `components/DashboardCards.test.tsx` **不存在**（已核 `ls`），该分支从未被断言。

### 3.2 修法

- `delta > 0` → `ArrowUpOutlined` + `colorError`；`delta < 0` → `ArrowDownOutlined` + `colorSuccess`；`delta === 0` 或 `undefined` → 不渲染角标。
- `Dashboard.tsx:82` 的 `: 5` 改为 `undefined`（配合 `DashboardCards.tsx:52` 的 `!== undefined` 判空）。
- 颜色取 `theme.useToken()` 的 `colorError` / `colorSuccess`——**不用** `var(--ant-color-error)`：实测 `App.tsx:348-353` 的 `ConfigProvider` 未开 `cssVar`，且 `node_modules/antd/es/theme/useToken.js:92-103` 只在 `cssVar` 为真时才让 cssinjs 注入 `--ant-*` → 现有 13 处 `var(--ant-*)` 全部未定义（见 H6）。
- 新增 `DashboardCards.test.tsx`：正/负/零/undefined 四条。

### 3.3 Risk

| # | 失败模式 | 缓解 |
|---|---|---|
| R7 | 用硬编码红绿，暗色模式对比度不足 | 用 `theme.useToken()` 的 `colorError`/`colorSuccess`（暗色算法下自动适配） |
| R8 | `delta === 0` 渲染 `0` 造成噪声 | 零值/undefined 不渲染角标，用例钉住 |

---

## W4 UI 审美 / 易用性（UI 审计并入，分两批）

### 4.1 批 1（本轮）

| # | 级别 | 现象 | 位置 | 修法 |
|---|---|---|---|---|
| H1 | HIGH | 命令面板点资产跳 `/assets/:id`，路由不存在 → 404 | `components/CommandPalette/index.tsx:187`；路由只有 `/assets`、`/assets/:id/diagnostics`（`App.tsx:233,241`） | 改指 `/assets/${id}/diagnostics`；用例断言 navigate 参数 |
| H6 | HIGH | 13 处 `var(--ant-*)` 全部未定义 → 次要文字色/背景/边框失效 | `EmptyState.tsx:96,100`、`Assets.tsx:303`、`CommandPalette/index.tsx:108`、`StatusPage.tsx:35`、`Alerts.tsx:304` 等 | 二选一：① `ConfigProvider theme={{cssVar:true}}`（一行，但改 antd 类名，需全量回归）；② 13 处改 `theme.useToken()`。**先做①的实测**，回归不过再退② |
| H8 | HIGH | 删除通知渠道一键生效，无二次确认 | `Settings.tsx:496` | 套 `AssetTable.tsx:163-168` 的 Popconfirm 四件套 + `onConfirm` 返回 Promise 自带 loading |
| H9 | HIGH | 移动端资产卡把「维护」标成红色「离线」 | `Assets.tsx:261-263`（桌面端 `AssetTable.tsx:92` 正确） | 抽 `statusLabel(status)` 两处共用；`StatusTag.COLOR_MAP` 补 `maintenance: 'orange'` |
| H10 | HIGH | Ping/Traceroute 失败时诊断弹窗全白 | `Assets.tsx:352-370`（catch 空实现 `:102-104`） | 加 `diagError` 状态（放 `Assets` 组件层，不放 `destroyOnHidden` 的 Modal 内）+ 弹窗内 `Alert` + 重试 |
| M4 | MED | 提交按钮无 loading → 连点重复创建 | `AlertSuppressions.tsx:160,194`、`Oncall.tsx:103,156`、`Runbook.tsx:180`、`Settings.tsx:741` | 加 `submitting` state + `confirmLoading`（范本 `AssetFormModal.tsx:64`）；`finally` 复位 |
| M5 | MED | 危险操作确认质量不一致 | `AlertSuppressions.tsx:145`、`Runbook.tsx:167`、`Oncall.tsx:101,154` | 统一 `title` 带对象名 + `okText="删除"` + `okButtonProps={{danger:true}}` |
| M6 | MED | 14 处 `required` 无 `message`，提示语气不一致 | `Settings.tsx:772,775,794,797,802,808,811,820,833,840`、`Oncall.tsx:105,158`、`TicketFormModal.tsx:56`、`AssetFormModal.tsx:100` | 统一句式「请输入/请选择 XXX」。**实现时发现 2 处豁免**：`TicketFormModal.tsx:56` priority、`AssetFormModal.tsx:100` status 均默认值预填（`'normal'`/`'active'`）且 Select 无 `allowClear`，`required` 校验永不触发（死代码），加 message 属「为不可能状态写代码」且变异反证无法做 → 不改，M6 实际收口 12 处 |
| M9 | MED | 搜索占位符承诺搜「资产标签/SN」，实际只搜 name/ip | `AssetFilterBar.tsx:25` / `Assets.tsx:162-165` | ✅ 已改「搜索名称 / IP」（`Asset` 类型里没有 `asset_tag`/`sn`，补搜索属扩功能） |
| M10 | MED | 副标题计数用未过滤总数，与表格行数不符 | `Assets.tsx:238`、`Tickets.tsx:64` | ✅ Assets 已改 `filtered.length` +「（已筛选）」；`Tickets.tsx:64` 待随 Tickets 页处理 |

### 4.2 批 2（下一轮，不在本轮范围）

M1 标题体系统一（`PageHeader` 只覆盖 5/12 页）、M2 表格排序（全站零 `sorter`）、M3 分页口径、M7 升级策略 JSON textarea（先改异常文案，结构化编辑器属新功能）、M11 严重度配色两套、M12 已并入 W2、M13 移动端（Sider `breakpoint` + `AlertTable` 双渲染）、M14 批量操作确认与取消、M15 空态/加载态统一。

**M16（rev4 新增，待决策，不在本轮范围）**：工单**优先级域跨层不一致**。契约 `openapi.yaml:2368` 的 priority enum 是 `[critical, high, normal, low]`，前端表单默认值/筛选/标签都按 `normal`；但 `cmd/seed/main.go:264` 与 `integration/glpi.go:158` 写入的是 `medium`（`models/ticket.go:20` 注释也是 medium）。后果：GLPI/seed 工单在列表里显示英文原值、用「普通」筛选筛不到。本轮只加了显示兜底（`medium: '普通'`），**未改域** —— 改哪边涉及存量数据与后端校验，需先定契约。同一类问题还有 `TicketStatsCards` 的档位（已在 W1 按契约修正为 5 档）。

---

## W5 前端性能（前端性能审计并入，rev3）

审计方法：codegraph 定位 + `command grep` 全仓复核（避开 `.gitignore` 静默漏检）+ 两个 `/tmp` 沙箱实测（vitest 渲染计时、vite build 体积），数字均为实测。

**基线**（`frontend/dist/assets/`，2026-09-09 02:09 构建）：入口 `index` 1,043,593 raw / 331,829 gzip；默认路由 `Dashboard` 1,059,595 / 351,021；全部 JS 2,447,856 / 803,772。

### W5.1 批 1（本轮）

| # | 级别 | 现象 | 位置 | 修法 |
|---|---|---|---|---|
| P1 | HIGH | nginx 未开 gzip（默认 `gzip_types` 也不含 js/css）→ 首屏 JS 传 2.10MB 而非 0.68MB（3.08×） | `frontend/nginx.conf:1-38`、`nginx-tls.conf.example:70-78` | `gzip on; gzip_vary on; gzip_comp_level 5; gzip_min_length 1024; gzip_types text/css application/javascript application/json image/svg+xml;`。**`gzip_vary on` 必须加**，否则中间代理可能把 gzip 喂给不支持的客户端 |
| P3 | HIGH | `CommandPalette` 全局挂载即发 3 个列表请求（面板未打开），且 queryKey 与页面不同无法去重 | `App.tsx:356`、`components/CommandPalette/index.tsx:157-174` | 三个 `useApiQuery` 加 `enabled: open`（hook 已支持）。体感回退用 hover 预取缓解 |
| P2 | HIGH | `echarts-for-react` 默认入口全量引入 echarts，占 Dashboard chunk 98% | `components/AlertTrendChart.tsx:2,35` | 改 `echarts/core` + `LineChart` + `Grid/Tooltip/Title` + `CanvasRenderer` 显式注册（沙箱实测 −555.75KB raw / −182.17KB gzip，Dashboard chunk 约 −48%） |

**P2 的关键风险**：漏注册组件时图表**静默空白、不报错**。必须配一条「渲染出 canvas」的单测，并在文件头注释写清「新增图表类型须在此 `echarts.use`」。

### W5.2 批 2（下一轮）

| # | 级别 | 现象 | 位置 | 备注 |
|---|---|---|---|---|
| P4 | HIGH | `AssetTable`/`AlertTable` 未 memo + `columns` 每渲染重建 → 无关 state 变更整表重渲染（100 行实测 12–34×） | `AssetTable.tsx:60,186-197`、`AlertTable.tsx:44,125`、`Alerts.tsx:278-286` | **必须与 M3 分页同批**：分页修好前绝对收益仅 20 行量级，修好后立即变成可感知卡顿 |
| P5 | MED | 列表无服务端分页：资产/工单静默截断 20 条、告警 100 条，`total` 被丢弃，antd 分页是「假分页」 | `Assets.tsx:47-53`、`Tickets.tsx:32-47`、`Alerts.tsx:81-98` | 与 M3 合并；资产页后端契约最完整，先做 |
| P6 | MED | `runBulk` 每条 `setBulkProgress` → 每条一次全页 + 整表重渲染 | `Alerts.tsx:126-148`（`:141`） | P4 落地后基本消解；若仍做节流，**最后一条必须强制刷新**，否则进度停在 98% |
| P7 | MED | `MobileCardList` 用 `key={idx}` → 过滤/新增时 DOM 复用错位（仅 xs 断点，jsdom 测不到） | `hooks/useResponsiveTable.tsx:59` | 改 `key={item.id ?? idx}` |

### W5.3 登记不做（本轮）

| # | 项 | 理由 |
|---|---|---|
| P8 | vite `manualChunks` | 收益只在重复访问，拆错会造巨型 vendor chunk 抵消懒加载；等 P2 落地后重看 chunk 分布再定 |
| P9–P12 | `useGlobalHotkey` 重订阅、zustand selector 新对象、sites 挂错 `queryKeys.racks.all`、`AppLayout` 菜单数组重建 | 绝对成本可忽略，顺手时改；P11 属键名隐患，可在 M3 一并修 |

**经核查不存在、明确不提**：请求取消/卸载竞态（`useEffect` 均已 cleanup，数据层收敛在 React Query）、轮询（全仓 `grep setInterval` = 0）、`rowKey` 缺失、`@ant-design/icons` 全量引入、`useApiQuery` 内联 `filters` 对象（React Query v5 `hashKey` 排序后序列化，内容相同即命中）。

---

## W6 后端性能（后端性能审计并入，rev4）

审计方法：临时 PG 18 容器（数据目录挂 `/dev/shm`，只读挂载真实迁移文件），灌入合成数据后 `EXPLAIN (ANALYZE, BUFFERS)` 实测。规模：assets 200k / alerts 500k / tickets 100k / audit_logs 1M / asset_networks 300k / notification_logs 200k。迁移 000001–000015 全部应用成功（64 张表）。

### W6.1 批 1（低风险、高收益、可独立验证）

| # | 级别 | 现象 | 位置 | 实测 |
|---|---|---|---|---|
| P13 | HIGH | 通知 worker 每 5s 轮询 `status='pending'`，但唯一索引是 `WHERE status='failed'` 的部分索引 → 每次全表扫 + Sort | `notification/worker.go:198-206`；`migrations/000009_notification_logs.up.sql:23-25` | 286.6 ms → 加 `idx_notification_logs_pending (sent_at) WHERE status='pending'` 后 **0.346 ms** |
| P14 | HIGH | `/dashboard/kpis` 5 条串行聚合，`alerts.problem_start` 无索引 → 4 条扫全表，且**无缓存** | `service/dashboard_service.go:156-207` | 合计 ~1.1 s → 加 `idx_alerts_problem_start` 后密度 360.6→12.1 ms |
| P15 | HIGH | Zabbix 同步预查 `alerts WHERE trigger_id IN (...)`，`trigger_id` 无索引 | `integration/service.go:166-170`；`000013:238-260` | 294.8 ms → **1.26 ms** |
| P16 | MED | GLPI 同步预查 `tickets WHERE external_id IN (...)` 无索引 | `integration/service.go:224-230` | 152.2 ms → **1.08 ms** |
| P17 | MED | metric sync 每 5min 按 `assets.name IN (...)` 关联，`name` 无索引 | `integration/metric_sync.go:154-156` | 75.3 ms → **1.21 ms** |
| P18 | MED | 审计列表 `path LIKE 'x%'` 无可用索引 | `service/audit_service.go:44-53` | 罕见过滤 455 ms → `idx_audit_logs_path (path text_pattern_ops)` 后 **0.39 ms** |
| P19 | MED | 工单 cursor 模式仍跑一次无条件 `COUNT(*)`（注释说"不跑 Count"，代码与注释不符） | `service/ticket_service.go:50-53` vs `:68-75` | 把 Count 挪到 cursor 分支之后，3 行 |

**迁移约束（审计踩过的坑）**：执行器按事务跑整个文件，**不能用 `CREATE INDEX CONCURRENTLY`**（`000015_asset_netbox_unique.up.sql:39-41` 已记录）。大表（audit_logs）升级期间会阻塞读写，需维护窗口。全部 `CREATE INDEX IF NOT EXISTS`，可重入。

### W6.2 批 2（需确认）

| # | 项 | 待确认 |
|---|---|---|
| P20 | `pg_trgm` + `idx_assets_trgm`（#3 的 `ILIKE '%x%'` 三列搜索） | 收益最大（1.75s→~0.02s）但**写放大最明显**，且 `CREATE EXTENSION` 权限不足会让整个迁移失败 → 服务起不来。单独 000022 迁移（批 1 六索引占 000016–000021 后顺延），失败可回滚 |
| P21 | `statsInternal` 30s 缓存 + `KPIs` 缓存（#2/#4） | 接受 30s 滞后；且进程内缓存会跨测试用例串值，必须提供可注入 `nil` 缓存的入口 |
| P22 | 拓扑接口分页/缓存（#8） | 全量拉 assets + asset_networks，200k 资产响应几十 MB；加 LIMIT 会改产品语义，先与前端定 |

### W6.3 登记不做（本轮）

| # | 项 | 理由 |
|---|---|---|
| P23 | eventbus 并发发送 / 幂等重试 / `WithBus` 接线（#11） | 涉及通知重复与丢失语义；另发现 `main.go:70` 构造 `NewAlertService(db)` 后**从未调用 `WithBus`**，生产环境事件链路是死代码，通知全靠 5s 轮询——需单独确认是否有意为之 |
| P24 | `tickets.in_sla` 列不存在 → SLA 指标永远 nil（#12） | 补列需产品定义语义 |
| P25 | 审计改异步写、引入 Redis、`BulkResolve` 重构、API key tracker 批量 UPDATE、gRPC 深分页 | 破坏合规语义 / 新增依赖 / 收益低 |

---

## 5. 执行顺序与验证门

1. W1 分小步：先 `ErrorState` 组件 + Dashboard（含最近告警接 `GET /alerts?limit=5`）→ 逐页推进，每页一个小步。
2. W2（`utils/time.ts` + 7 个调用点）。
3. W3（`DashboardCards` + `Dashboard.tsx:82`）。
4. W4 批 1 逐项（H1 → H6 实测 → H8/H9/H10 → M4/M5/M6/M9/M10）。
5. W5 批 1：P1 nginx gzip（一行，先做先赢）→ P3 命令面板 `enabled: open` → P2 `echarts/core`（必须配「渲染出 canvas」单测）。
6. 全量：`npx vitest run`、`npx tsc --noEmit`、`npm run lint`；`go test ./...` 确认后端无回归。
7. 后端性能回执后并入 §5.1（升 rev4），再走「多角度代码审计 → 提交推送（SSH）→ CI 绿」。

### 5.1 审计回执状态

| 路 | 主题 | 状态 |
|---|---|---|
| B | 前端性能（渲染、网络与数据、包体、内存/副作用） | **已回执，并入 §W5（rev3）** |
| C | 后端性能（N+1、索引 vs 迁移、热点路径、资源与并发） | **已回执，并入 §W6（rev4）** |

---

## 6. 登记不做 / 后续

| 项 | 理由 |
|---|---|
| L5 命令面板 `runbook` 死类型、L6 面包屑不可达分支、L7 `EmptyState` preset CTA 未接线 | 只影响注释/死代码准确性，不影响用户；顺手时再清 |
| M7 的结构化编辑器重构 | 属新功能，非 UX 修补；本轮只把 JSON 解析异常文案改成人话 |
| `AssetTable` 的 IP 数值排序 | 自定义 `sorter` 在 IPv6 上易错，收益低 |
| 工单四档状态改造 | 需后端域确认（现只有 `open/in_progress/resolved/closed`），本轮先按真实域改前端展示 |

## 7. 变更记录

| rev | 日期 | 内容 |
|---|---|---|
| rev1 | 2026-09-09 | 自研发现 W1/W2/W3；「antd locale 缺失」经实测证伪（`App.tsx:349` 已挂 `zhCN`）已删 |
| rev2 | 2026-09-09 | 并入 UI 审美审计（H1–H10 / M1–M15 / L1–L7）与文档事实审查：修正 `AssetTimeline`/`DashboardCards` 行号、补 `Racks` 小写 mock 两处与 `AssetTimeline` 渲染层两处、兜底计数 12→~34、补两个缺数据源的方案（Dashboard 最近告警复用 `GET /alerts?limit=5`；Tickets 统计卡按真实四档推导）、补 undefined 守卫与「区块级错误态」、补 4 处缺空态、`cssVar` 实测结论、W2 补 3 个遗漏站点并撤掉「截断」误述、测试基数 14→11；新增 §1.4 决策记录（不采纳演示横幅）与批次划分 |
| rev3 | 2026-09-09 | 并入**前端性能审计**（沙箱实测）：新增 §W5（P1 nginx gzip / P2 echarts-core / P3 命令面板按需为批 1；P4 表格 memo 化**必须与 M3 分页同批**、P5 服务端分页、P6 进度节流、P7 `key={idx}` 为批 2；P8–P12 登记不做）；§0 批次表与 §5 执行顺序同步；§5.1 改为回执状态表 |
| rev4 | 2026-09-09 | 并入**后端性能审计**（§W6）；§1.3-7② 修正为契约 5 档（含 `pending`）；M16 登记待决策。**补记**：该 rev 此前只改了正文与状态行，变更记录漏登 |
| rev5 | 2026-09-09 | W2 补第 5 个遗漏站点 `pages/MetricSnapshot.tsx:128`（`new Date(v).toLocaleString()`）——rev2 的「W2 补 3 个遗漏站点」仍漏了它，随该页 W1 同轮收口 |
| rev6 | 2026-09-10 | 台账 W1 推进 2 页：Topology（W1 第 9 页）、Runbook（W1 第 10 页）。Topology 去 `?? MOCK_GRAPH` + `catch` 双重兜底，新增 `normalizeGraph()`；Runbook 去列表/推荐两处 `.catch(() => MOCK_*)` 兜底，列表/推荐各补区块级 ErrorState + `Array.isArray` 归一。修订：推荐面板形状异常测试先红，暴露 rev 实现初稿漏 `Array.isArray`（写成 `data ?? []`），补守卫后 4 变异全红 |
| rev7 | 2026-09-10 | **W1 全部 11 页完成**（末页 AssetTimeline = W1 第 11 页 + W2 `:122` 收口）：去三重兜底，新增 `normalizeTimeline()`；`summary: typeof MOCK_SUMMARY` 类型依赖 mock 的隐患随 mock 删除一并消除（独立 TimelineSummary 接口）；5 变异全红。§8 台账 W1 段整段标全完成 |
| rev8 | 2026-09-10 | **W2 全站时间格式统一收口**：`pages/Settings.tsx:923` API 密钥表「最后使用」列 `t ? new Date(t).toLocaleString() : '—'`（无 locale + 非法时间显示 "Invalid Date"）→ `formatDateTime(t)`（空/非法 → '—'），与其余 6 页口径一致。1 用例绿，变异回退 `toLocaleString` 红在 `2026-02-14 10:00:00` 断言 |
| rev9 | 2026-09-10 | **W4-H6 完成**：13 处 `var(--ant-*)` 全未定义（`ConfigProvider` 未开 `cssVar`）→ 方案①实测可行，`App.tsx` 提取 `buildTheme(themeMode)` 纯函数开 `cssVar:true`。实测结论：全量 251→253 用例不破坏；探针确认 `--ant-color-text-secondary`/`--ant-color-primary` 注入。新增 `App.theme.test.tsx` 2 用例（明暗两态 cssVar + 注入），变异删 `cssVar: true` 两用例均红在断言 |
| rev10 | 2026-09-10 | **W4-H8 完成**：`Settings.tsx:497` 删除通知渠道无二次确认 → 套 Popconfirm 四件套（`AssetTable:163-168` 范本），title 带渠道名、`okButtonProps danger`、`onConfirm` 返回 Promise 自带 loading。1 用例绿，变异（去 Popconfirm 改回直接 onClick）红在 `not.toHaveBeenCalled()` 断言 |
| rev11 | 2026-09-10 | **W4-H9 完成**：移动端资产卡 `status === 'active' ? '在线' : '离线'` 把 maintenance 误标红「离线」→ 抽 `statusLabel(status)` 单一出口（`StatusTag.tsx` 导出，值域对齐后端 asset.go:35 active/offline/maintenance/retired），桌面端/移动端两处共用；`COLOR_MAP` 补 `maintenance: 'orange'`。顺带发现前端类型漂移：`types/index.ts:31` Asset.status 写 `'inactive'`（应为 `'offline'`），未改（不在 H9 范围）。4 用例绿，两条变异（移动端改回硬编码 / 删 maintenance 分支）均红在断言 |
| rev12 | 2026-09-10 | **W4-H10 完成**：诊断（Ping/Traceroute）失败时弹窗全白——`handleDiagnose` catch 空实现，失败后 pingResult/traceResult 保持 null，Modal 三条渲染分支（loading/ping/traceroute）全不命中。加 `diagError` 状态（Assets 组件层，规避 `destroyOnHidden` Modal 内 state 被清）+ 弹窗内 `Alert`（message「诊断失败」+ description）+ 重试按钮。1 用例绿，变异（去 `setDiagError`）红在「找不到『诊断失败』」断言 |
| rev13 | 2026-09-10 | **W4-M4（第 1 步 AlertSuppressions）完成**：提交按钮无 loading → 连点重复创建。`AlertSuppressions.tsx` 两个 Modal（新建/编辑 `onOk={onSubmit}`、模拟评估 `onOk={onPreview}`）加 `submitting`/`previewing` state + `confirmLoading`，`finally` 复位。1 用例绿（apiSend pending 时保存按钮 `ant-btn-loading` + loading 期间连点只调一次 apiSend），变异（去 `confirmLoading={submitting}`）红在 `toHaveClass('ant-btn-loading')` 断言。M4 剩余 Oncall/Runbook/Settings 待续 |
| rev14 | 2026-09-10 | **W4-M4（第 2 步 Oncall）完成**：`Oncall.tsx` 值班组（SchedulesTab）+ 升级策略（PoliciesTab）两个 Modal `onOk={onSubmit}` 加 `submitting` state + `confirmLoading`，`finally` 复位。2 用例绿（apiSend pending 时保存按钮 `ant-btn-loading` + 连点只调一次 apiSend），变异（去两处 `confirmLoading`）两用例均红在 `toHaveClass('ant-btn-loading')` 断言。M4 剩 Runbook/Settings 待续 |
| rev15 | 2026-09-10 | **W4-M4（第 3 步 Runbook）完成**：`Runbook.tsx` 新建/编辑 Modal `onOk={onSubmit}`（无 okText，默认 OK）加 `submitting` state + `confirmLoading`，`finally` 复位。1 用例绿（apiSend pending 时 OK 按钮 `ant-btn-loading` + 连点只调一次 apiSend），变异（去 `confirmLoading`）红在 `toHaveClass('ant-btn-loading')` 断言。M4 剩 Settings 1 处待续 |
| rev16 | 2026-09-10 | **W4-M4（第 4 步 Settings）完成，M4 全部 4 文件收口**：`Settings.tsx` 渠道 Modal `onOk={handleSaveChannel}` 加 `channelSaving` state + `confirmLoading`，`finally` 复位。1 用例绿（createChannel pending 时保存按钮 `ant-btn-loading` + 连点只调一次），变异（去 `confirmLoading`）红在 `toHaveClass('ant-btn-loading')` 断言。M4 全部完成（AlertSuppressions/Oncall/Runbook/Settings 共 4 文件 6 提交点） |
| rev17 | 2026-09-10 | **W4-M5（第 1 步 AlertSuppressions）完成**：删除规则 Popconfirm `title="确定删除？"`（无对象名、无 danger）→ 四件套（范本 `AssetTable:163-168`）`title={`确认删除规则「${record.name}」？`}` + `okText="删除"` + `cancelText="取消"` + `okButtonProps={{ danger: true }}`。1 用例绿（确认框 title 带对象名 + 确认按钮 `ant-btn-dangerous` + 确认后 DELETE `/alert-suppressions/r1`），变异（去 `okButtonProps`）红在 `toHaveClass('ant-btn-dangerous')` 断言。M5 剩 Runbook 1 处 + Oncall 2 处待续 |
| rev18 | 2026-09-10 | **W4-M5（第 2 步 Runbook）完成**：删除 Runbook Popconfirm `title="确定删除?"`（无对象名、无 danger）→ 四件套 `title={`确认删除 Runbook「${rb.title}」？`}` + `okText="删除"` + `cancelText="取消"` + `okButtonProps={{ danger: true }}`。1 用例绿（确认框 title 带对象名 + 确认按钮 `ant-btn-dangerous` + 确认后 DELETE `/runbooks/r1`），变异（去 `okButtonProps`）红在 `toHaveClass('ant-btn-dangerous')` 断言。M5 剩 Oncall 2 处待续 |
| rev19 | 2026-09-10 | **W4-M5（第 3 步 Oncall）完成，M5 全部 4 处收口**：值班组（`SchedulesTab`）+ 升级策略（`PoliciesTab`）两处 Popconfirm `title="删除？"`（无对象名、无 danger）→ 四件套 `title={`确认删除值班组/升级策略「${r.name}」？`}` + `okText="删除"` + `cancelText="取消"` + `okButtonProps={{ danger: true }}`。2 用例绿（两处确认框 title 带对象名 + `ant-btn-dangerous` + 确认后 DELETE `/oncall/schedules/s1`、`/oncall/policies/p1`），变异（去两处 `okButtonProps`）两用例均红在 `toHaveClass('ant-btn-dangerous')` 断言。**M5 全部 4 处完成（AlertSuppressions/Runbook/Oncall×2）** |
| rev20 | 2026-09-10 | **W4-M6（第 1 步 Settings）完成**：渠道 Modal 10 处 `required: true` 无 message（antd 默认英文「${label} is required」语气不一致）→ 统一句式：name「请输入渠道名称」、type「请选择渠道类型」、smtp_host/port/user/from「请输入SMTP服务器/端口/用户名/发件人」、to「请输入收件人」、dingtalk/wechat/webhook 的 url「请输入Webhook URL」。3 用例绿（顶层不填保存断言 name+type 中文提示；email 条件字段 5 处；dingtalk URL），变异（去 smtp_host message）红在「找不到『请输入SMTP服务器』」断言。M6 剩 Oncall 2 处 + TicketFormModal 1 处 + AssetFormModal 1 处待续 |
| rev21 | 2026-09-10 | **W4-M6（第 2 步 Oncall）完成**：值班组（SchedulesTab）+ 升级策略（PoliciesTab）两个「名称」`required: true` 无 message → `message: '请输入名称'`。2 用例绿（两处不填名称保存断言「请输入名称」），变异（去值班组名称 message）红在「找不到『请输入名称』」断言。M6 剩 TicketFormModal 1 处 + AssetFormModal 1 处待续 |
| rev22 | 2026-09-10 | **W4-M6 收口（12 处完成 + 2 处豁免）**：TicketFormModal.tsx:56 priority、AssetFormModal.tsx:100 status 两处 `required` 有默认值预填（`'normal'`/`'active'`）且 Select 无 `allowClear`，校验永不触发（死代码）→ 不加 message，豁免（违反「不为不可能状态写代码」且变异反证无法做）。§4.1 M6 描述与 §8 台账同步记录豁免理由。**M6 全部完成：Settings 10 + Oncall 2 已改，2 处豁免**。W4 批 1 全部收口（H1/H6/H8/H9/H10/M4/M5/M6） |
| rev23 | 2026-09-10 | **W6 批 1 第 1 步 P13 完成**：通知 worker 每 5s 轮询 `status='pending'` 全表扫（286.6ms）→ 迁移 000016 加 `idx_notification_logs_pending (sent_at) WHERE status='pending'` 部分索引（与 000009 的 failed 索引互补）。真 PG dbsmoke 两层断言：① 索引形态（pg_indexes.indexdef 含 sent_at/status/'pending'）；② EXPLAIN（`enable_seqscan=off` 强制走索引，验证谓词/列匹配）。`DownPreservesLegacyColumns` 三次→四次 Down（16→15→14→13）。变异（去 CREATE INDEX）红在 `NotEmpty`「索引不存在」断言。**决策：批 1 六索引拆 000016–000021 每索引一迁移**（失败隔离/独立回滚/小步可验证），P20 pg_trgm 顺延 000022 |
| rev24 | 2026-09-10 | **W6 批 1 第 2 步 P14 完成**：`/dashboard/kpis` 4 条聚合（MTTR/MTTD/密度/计数）都按 `problem_start >= ?` 过滤时间窗、无索引扫全表（合计 ~1.1s）→ 迁移 000017 加 `idx_alerts_problem_start (problem_start)`。dbsmoke 形态 + EXPLAIN（`enable_seqscan=off` 范围查询走索引）断言；Down 链四次→五次（17→16→15→14→13）；变异（去 CREATE INDEX）红在 `NotEmpty`「索引不存在」断言。下一 000018=P15 `alerts.trigger_id` |
| rev25 | 2026-09-10 | **W6 批 1 第 3 步 P15 完成**：Zabbix 同步预查 `alerts WHERE trigger_id IN (...)` 无索引扫全表（294.8ms）→ 迁移 000018 加 `idx_alerts_trigger_id (trigger_id)`。dbsmoke 形态 + EXPLAIN 断言；Down 链五次→六次（18→17→16→15→14→13）；变异（去 CREATE INDEX）红在 `NotEmpty`「索引不存在」断言。**EXPLAIN 断言细节**：真实查询带 `AND status='problem'` 时，空表上优化器会改选已存在的 `idx_alerts_status_created`（status 索引）——故 EXPLAIN 用纯 `trigger_id IN (...)` 条件验证索引可被命中，`status` 是附加过滤不影响索引存在性。下一 000019=P16 `tickets.external_id` |
| rev26 | 2026-09-10 | **W6 批 1 第 4 步 P16 完成**：GLPI 同步预查 `tickets WHERE external_id IN (...)` 无索引扫全表（152.2ms）→ 迁移 000019 加 `idx_tickets_external_id (external_id)`。dbsmoke 形态 + EXPLAIN 断言（真实查询单条件，无其它索引竞争，直接走该索引）；Down 链六次→七次（19→18→17→16→15→14→13）；变异（去 CREATE INDEX）红在 `NotEmpty`「索引不存在」断言。下一 000020=P17 `assets.name` |
| rev27 | 2026-09-10 | **W6 批 1 第 5 步 P17 完成**：metric sync 每 5min 按 `assets.name IN (...)` 关联无索引扫全表（75.3ms）→ 迁移 000020 加 `idx_assets_name (name)`。dbsmoke 形态 + EXPLAIN 断言（真实查询单条件，无其它索引竞争，直接走该索引）；Down 链七次→八次（20→19→18→17→16→15→14→13）；变异（去 CREATE INDEX）红在 `NotEmpty`「索引不存在」断言。下一 000021=P18 `audit_logs.path text_pattern_ops` |
| rev28 | 2026-09-10 | **W6 批 1 第 6 步 P18 完成**：审计列表 `path LIKE 'x%'` 前缀匹配无可用索引（罕见过滤 455ms）→ 迁移 000021 加 `idx_audit_logs_path (path text_pattern_ops)`。**验证**：`text_pattern_ops` 可作用于 `VARCHAR(500)` 列（真 PG `CREATE INDEX` 通过），EXPLAIN 显示 `Index Only Scan using idx_audit_logs_path`（`~>=~`/`~<~` pattern 操作符）。dbsmoke 形态（path + text_pattern_ops）+ EXPLAIN 断言；Down 链八次→九次（21→20→19→18→17→16→15→14→13）；变异（去 CREATE INDEX）红在 `NotEmpty`「索引不存在」断言。下一 P19 `ticket_service` cursor Count 3 行（无迁移） |
| rev29 | 2026-09-10 | **W6 批 1 第 7 步 P19 完成（批 1 全部收口）**：工单 cursor 模式仍跑一次无条件 `COUNT(*)`（注释说"不跑 Count"，代码与注释不符）→ 把 `q.Count(&total)` 从 cursor 判断之前挪到 offset 分支之后（`ticket_service.go:66-74`）。cursor 模式不再白跑全表计数（每页省一次 `COUNT(*)`），offset 模式保留 total。单测 `TestTicketService_List_cursor模式不跑Count`（sqlmock 只期望一条 Find，不期望 count）；变异（把 Count 挪回 cursor 前）红在 `could not match actual sql: SELECT count(*)`。**W6 批 1（P13–P19）全部完成** |
| rev30 | 2026-09-10 | **批 2 第 1 步 M14（批量操作确认）完成**：Alerts 页「批量确认」「批量解决」两按钮原本 `onClick` 直接对选中告警逐条生效（误点即执行，无二次确认）→ 套 Popconfirm 四件套（title 带已选数量「批量确认/解决已选的 N 条告警？」+ okText/cancelText，批量解决加 `okButtonProps danger`，范本 `Settings:503`）。1 用例绿（选中行 → 点批量确认 → 确认框出现且 mutate 未调用 → 点确认后 `mutate(['1'])`），变异（去 Popconfirm 改回直接 onClick）红在「找不到确认框文案」断言。**批 2 启动** |
| rev31 | 2026-09-10 | **批 2 第 2 步 M7（升级策略 JSON 异常文案）完成**：Oncall 升级策略 Levels 是 JSON textarea，`JSON.parse` 抛 `SyntaxError` 时原 catch 直接 `message.error(e?.message)` → 英文技术报错（"Unexpected token..."）。改为 `e instanceof SyntaxError` 时给友好中文「Levels JSON 格式错误，请检查后重试」。1 用例绿（新建 → 输非法 JSON → 保存 → 断言 message.error 友好文案 + apiSend 未调用），变异（去 SyntaxError 判断）红在 `toHaveBeenCalledWith` 断言。结构化编辑器属新功能，登记不做 |
| rev32 | 2026-09-10 | **批 2 第 3 步 M11（严重度配色统一）第 1 处 AssetTimeline 完成**：`severityColor()` 自造一套（5红/4橙/3黄/2蓝/1绿/0灰），与告警中心 `SeverityTag` 权威 6 档（P5紫红/P4红/P3橙/P2黄/P1蓝/P0灰）不一致，P3/P4/P5 颜色全错。改为复用 `SEVERITY_META[sev]?.color ?? 'default'`（删掉 6 行 if-else）。1 用例绿（severity=4 alert 事件 Tag 断言 `ant-tag-red` 且非 `ant-tag-orange`），变异（改回 `sev>=4?'orange'`）红在 `toHaveClass('ant-tag-red')` 断言。**剩 Runbook 两处 + AlertSuppressions 一处** |
| rev33 | 2026-09-10 | **批 2 第 4 步 M11（严重度配色统一）第 2 处 Runbook 完成**：Drawer 详情 + 推荐面板两处 `Tag color={severity>=4?'red':'orange'}>P{severity}` 两档，与列表列 `SeverityTag` 权威 6 档不一致 → 统一改 `<SeverityTag severity={...} />`（P5 紫红「灾难」/P4 红「严重」，与列表列一致）。2 用例绿（Drawer 打开后「P5 灾难」×2 且 `ant-tag-magenta`；推荐面板「P4 严重」且 `ant-tag-red`），变异（两处改回旧 Tag）均红在断言。**剩 AlertSuppressions 一处** |
| rev34 | 2026-09-10 | **批 2 第 5 步 M11（严重度配色统一）第 3 处 AlertSuppressions 完成（M11 全收口）**：`severity_max` 列 `Tag color={v>=4?'red':v>=3?'orange':'blue'}` 三档 → `SEVERITY_META[v]?.color ?? 'default'`（label 保持纯数字，因 severity_max 是「≤N」阈值非「P4 严重」标签）。1 用例绿（severity_max=2 断言 `ant-tag-gold` 且非 `ant-tag-blue`），变异（改回三档）红在 `toHaveClass('ant-tag-gold')` 断言。**M11 全部收口（AssetTimeline + Runbook + AlertSuppressions）** |
| rev35 | 2026-09-10 | **批 2 第 6 步 M1（标题体系）第 1 处 Oncall 完成**：Oncall 原 Tabs 页无可见标题（仅 `useDocumentTitle` 改浏览器标题），用户进来不知道是值班管理页 → 补 `<PageHeader title="值班管理" />`（h4，与 5 页已用 PageHeader 一致）。1 用例绿（`getByRole('heading', {level:4, name:'值班管理'})`），变异（去 PageHeader）红在「找不到 heading」断言。**剩 AlertSuppressions/Topology/Runbook/Settings（AssetTimeline 子页 + MetricSnapshot 带 Tag 待定）** |
| rev36 | 2026-09-10 | **批 2 第 7 步 M1（标题体系）第 2 处 AlertSuppressions 完成**：原无页面标题（仅 `useDocumentTitle` 改浏览器标题），「新建抑制规则」「模拟评估」两按钮悬空在页面顶部 → 补 `<PageHeader title="告警抑制" extra={按钮组} />`（h4，按钮从 `<Space>` 移入 extra，范本 Assets 页）。1 用例绿（`findByRole('heading', {level:4, name:'告警抑制'})`），变异（title 改占位符）红在「找不到 heading」断言。**剩 Topology/Runbook/Settings（AssetTimeline 子页 + MetricSnapshot 带 Tag 待定）** |
| rev37 | 2026-09-10 | **批 2 第 8 步 M1（标题体系）第 3 处 Topology 完成**：原无可见标题（仅 `useDocumentTitle` 改浏览器标题），「仅显示告警节点」筛选开关悬空在页面顶部 → 补 `<PageHeader title="网络拓扑" />`（h4，开关保留原位作筛选行）。1 用例绿（`getByRole('heading', {level:4, name:'网络拓扑'})`），变异（去 PageHeader）红在「找不到 heading」断言。**剩 Runbook/Settings（AssetTimeline 子页 + MetricSnapshot 带 Tag 待定）** |
| rev38 | 2026-09-10 | **批 2 第 9 步 M1（标题体系）第 4 处 Settings 完成**：原用原生 `<h2>系统设置</h2>`，与其它页 PageHeader h4 标题不一致 → 改 `<PageHeader title="系统设置" />`（h4）。1 用例绿（`findByRole('heading', {level:4, name:'系统设置'})`），变异（改回 h2）红在「找不到 level4 heading」断言。**剩 Runbook（AssetTimeline 子页 + MetricSnapshot 带 Tag 待定）** |
| rev39 | 2026-09-10 | **批 2 第 10 步 M1（标题体系）第 5 处 Runbook 完成（M1 全部收口）**：原手写 `<Space>` 标题（`Text strong`，非 heading，带 BookOutlined 图标 + 「N 条」计数 Tag + 新建按钮）→ 改 `<PageHeader title="故障 Runbook" extra={计数 Tag + 新建按钮} />`（h4，丢弃装饰性 BookOutlined 图标）。1 用例绿（`getByRole('heading', {level:4, name:'故障 Runbook'})`），变异（改回 Space）红在「找不到 heading」断言。**M1 五页全收口（Oncall/AlertSuppressions/Topology/Settings/Runbook）；AssetTimeline 子页 + MetricSnapshot 带 Tag 无对应 slot，登记待定** |
| rev40 | 2026-09-10 | **批 2 第 11 步 M2（表格排序）第 1 处 AlertTable 完成**：全站零 `sorter`，用户无法点表头排序 → `AlertTable` 给主机（`localeCompare`）/级别（`severity` 数值）/状态（`localeCompare`）/触发时间（`new Date(...).getTime()`，因 RFC3339 字符串字典序会因时区偏移错序）四列加前端本地排序；`message` 长文本不加。1 用例绿（点「触发时间」表头后第一行由 web-server-01 变 db-server-02 升序），变异（去 created_at sorter）红在 `toContain('db-server-02')` 断言。**剩 AssetTable/TicketTable/Runbook/Oncall/MetricSnapshot 等表格** |
| rev41 | 2026-09-10 | **批 2 第 12 步 M2（表格排序）第 2 处 TicketTable 完成**：`TicketTable` 给工单标题/请求人/处理人（`localeCompare`，处理人空值 `?? ''`）/状态（`localeCompare`）/创建时间（`new Date(...).getTime()`）加前端本地排序；优先级按严重度权重 `PRIORITY_WEIGHT`（critical 4/high 3/normal 2/medium 2/low 1，medium 与 normal 同权见 M16 域不一致）。1 用例绿（点「创建时间」表头后第一行由「服务器磁盘空间不足」02-14 变「网络延迟过高」02-13 升序），变异（去 created_at sorter）红在 `toContain('网络延迟过高')` 断言。**剩 AssetTable/Runbook/Oncall/MetricSnapshot 等表格** |

---

## 8. 执行进度（断点续跑台账）

> 自主循环每轮开工先读本节，再读 `git log -5` 确认落点。每完成一个可独立验证的小步，**在同一轮内**更新本节并提交。

| 项 | 状态 | 落点 |
|---|---|---|
| W2 `utils/time.ts` + 测试 | ✅ 完成 | 4 用例绿 |
| W1 `components/ErrorState.tsx` + 测试 | ✅ 完成 | 6 用例绿 |
| W3 `DashboardCards` 角标 + 测试 | ✅ 完成 | 3 用例绿 |
| W1+W2+W3 `pages/Dashboard.tsx` + 测试 | ✅ 完成 | 6 用例绿（含「失败不回落假数字」「最近告警接 `/alerts?limit=5`」） |
| W5-P1 nginx gzip（两个 conf） | ✅ 完成 | 容器实测：index 1,043,593→335,033、Dashboard 1,059,595→355,626 |
| W5-P3 命令面板 `enabled: open` | ✅ 完成 | 10 用例绿（含「未打开不发请求」「重复打开命中缓存」） |
| W5-P2 `echarts/core` + 测试 | ✅ 完成 | 构建实测 Dashboard chunk 1,059,595→514,130（gzip 351k→176k）；3 用例绿，变异（删 LineChart）红在断言 |
| W4-H1 命令面板资产路由 404 | ✅ 完成 | 断言 `navigate('/assets/a1/diagnostics')` |
| W1+W2 `pages/Alerts.tsx` + `AlertTable` | ✅ 完成 | 7 用例绿；两条变异（去 isError 分支 / 去 stats 守卫）均红在断言 |
| W1+M9+M10 `pages/Assets.tsx` + `AssetFilterBar` | ✅ 完成 | 11 用例绿；三条变异（去 isError 分支 / 副标题回落未过滤计数 / 去 ip_address 守卫）均红在断言 |
| W1+M10+W2 `pages/Tickets.tsx` + `TicketTable`/`TicketDetailModal`/`TicketStatsCards` | ✅ 完成 | 8 用例绿；四条变异（去列表错误分支 / 统计写死 / 去统计错误分支 / 列表时间原样）均红在断言。统计卡改 5 档真实推导（含 `pending`，见 §1.3-7② rev4 修正） |
| W1+W2 `pages/Oncall.tsx` | ✅ 完成 | 10 用例绿；四条变异（值班组去错误分支 / 去空态 / 当前值班时间回退原样渲染 / 升级策略去错误分支）均红在断言。三个 tab 的 `catch { return MOCK_* }` + `data ?? MOCK_*` 双重兜底已删除 |
| W1 `pages/AlertSuppressions.tsx` | ✅ 完成 | 6 用例绿；三条变异（去错误分支 / 去空态 / 空列表回落虚构规则）均红在断言。`catch { return MOCK_RULES }` + `data: rules = MOCK_RULES` 双重兜底已删除 |
| W1+W2 `pages/MetricSnapshot.tsx` | ✅ 完成 | 7 用例绿；四条变异（去错误分支 / 去空态 / 时间回退原样渲染 / 空结果回落虚构采样点）均红在断言。`catch { return MOCK_LATEST }` 已删除；W2 补第 5 个遗漏站点（rev5） |
| W1 `pages/Racks.tsx` | ✅ 完成 | 8 用例绿；五条变异（站点/机柜/设备去错误分支、去机柜空态、空列表回落虚构机柜）均红在断言。三处 `?? MOCK_*`（`MOCK_SITES`/`mockRacks`/`mockDevices`）已删除；新增「请先选择机房」引导空态与机柜/设备空态 |
| W1 `pages/Topology.tsx` | ✅ 完成 | 10 用例绿；五条变异（去错误分支 / 去空态分支 / 去 undefined 守卫 / 去 nodes 数组归一 / 去 stats 兜底）均红在断言。`?? MOCK_GRAPH` + `catch { return MOCK_GRAPH }` 双重兜底已删除；新增 `normalizeGraph()` 归一 nodes/edges/stats（§1.3-6 的 `Topology.tsx:94` 守卫）；过滤 switch 提到错误态之外，失败时仍可切换重查 |
| W1 `pages/Runbook.tsx` | ✅ 完成 | 9 用例绿；四条变异（列表去错误分支 / 推荐去错误分支 / 列表去形状归一 / 推荐去形状归一）均红在断言。列表 `.catch(() => MOCK_RUNBOOKS...)` + 推荐 `.catch(() => MOCK_RECOMMEND)` 两处兜底已删除；列表/推荐各补区块级 ErrorState + `Array.isArray` 形状归一（rev6：推荐面板原本漏 `Array.isArray`，形状异常测试先红后补守卫） |
| W1+W2 `pages/AssetTimeline.tsx` | ✅ 完成 | 9 用例绿；五条变异（去错误分支 / 去 asset 空态 / 去 events 数组归一 / 去 undefined 守卫 / 去 summary 兜底）均红在断言。`?? MOCK_TIMELINE` + `catch { return MOCK_TIMELINE}` + 渲染层 `data ?? MOCK_TIMELINE` / `tl.summary ?? MOCK_SUMMARY` 三重兜底已删除；新增 `normalizeTimeline()` 归一 asset/events/summary，asset 缺失走「资产不存在」空态；W2 `:122` 时间格式改 `formatDateTime`（rev7） |
| **W1 全部页（Dashboard/Alerts/Assets/Tickets/Oncall/AlertSuppressions/MetricSnapshot/Racks/Topology/Runbook/AssetTimeline = 11 页）** | ✅ **全完成** | 见上各行；全仓已无 `MOCK_*`/`mock*` 兜底常量与 `catch { return MOCK_* }` |
| W2 其余 1 个调用点 `pages/Settings.tsx:923` | ✅ 完成 | API 密钥表「最后使用」列 `toLocaleString()` → `formatDateTime`（空/非法 → '—'）；1 用例绿（断言 `2026-02-14 10:00:00`），变异（回退 `toLocaleString`）红在断言。**W2 全站时间格式统一收口** |
| W4-H6 cssVar 开启（13 处 `var(--ant-*)` 失效） | ✅ 完成 | 方案① `ConfigProvider theme={{cssVar:true}}` 实测：全量回归不破坏 + 探针确认 `--ant-*` 注入。提取 `buildTheme(themeMode)` 纯函数（`App.tsx`）使「必须开 cssVar」契约可单测；2 用例绿（明暗两态 cssVar + 注入 `--ant-color-text-secondary`），变异（删 `cssVar: true`）两用例均红在断言 |
| W4-H8 删除通知渠道二次确认 | ✅ 完成 | `Settings.tsx:497` 删除按钮套 Popconfirm 四件套（范本 `AssetTable:163-168`），title 带渠道名 + `okButtonProps danger` + `onConfirm` 返回 Promise；1 用例绿（点删除先弹确认框不调 deleteChannel → 确认后 `deleteChannel("c1")`），变异（去 Popconfirm 改回直接 onClick）红在 `not.toHaveBeenCalled()` 断言 |
| W4-H9 移动端资产卡「维护」误标红「离线」 | ✅ 完成 | 抽 `statusLabel(status)`（`StatusTag.tsx` 导出，后端 asset.go:35 值域 active/offline/maintenance/retired）桌面端 `AssetTable:92` 与移动端 `Assets.tsx` 卡片共用；`COLOR_MAP` 补 `maintenance: 'orange'`。4 用例绿（statusLabel 映射 + 未知值原样 + orange 色 + 移动端 maintenance 显示「维护」），两条变异（移动端改回硬编码 / 删 statusLabel maintenance 分支）均红在断言 |
| W4-H10 诊断失败弹窗全白 | ✅ 完成 | `Assets.tsx` 诊断 catch 原空实现 → 失败时 pingResult/traceResult 保持 null 弹窗全白。加 `diagError` 状态（放 Assets 组件层，不放 `destroyOnHidden` 的 Modal 内）+ 弹窗内 `Alert`（message「诊断失败」+ description 错误信息）+ 重试按钮（重调 handleDiagnose）。1 用例绿（Ping 失败显示 Alert + 重试二次调用），变异（去 `setDiagError` 恢复空 catch）红在「找不到『诊断失败』」断言 |
| W4-M4 提交按钮 loading · AlertSuppressions.tsx | ✅ 完成 | 两个 Modal（规则 `onOk={onSubmit}` + 预览 `onOk={onPreview}`）加 `submitting`/`previewing` state + `confirmLoading`，`finally` 复位（范本 `AssetFormModal`）。1 用例绿（apiSend pending 时保存按钮 `ant-btn-loading` + 连点只调一次 apiSend），变异（去 `confirmLoading={submitting}`）红在 `toHaveClass('ant-btn-loading')` 断言 |
| W4-M4 提交按钮 loading · Oncall.tsx | ✅ 完成 | 值班组（SchedulesTab）+ 升级策略（PoliciesTab）两个 Modal 加 `submitting` state + `confirmLoading`，`finally` 复位。2 用例绿（pending 时保存按钮 `ant-btn-loading` + 连点只调一次 apiSend），变异（去两处 `confirmLoading`）两用例均红在断言 |
| W4-M4 提交按钮 loading · Runbook.tsx | ✅ 完成 | 新建/编辑 Modal（无 okText，默认 OK）加 `submitting` state + `confirmLoading`，`finally` 复位。1 用例绿（pending 时 OK 按钮 `ant-btn-loading` + 连点只调一次 apiSend），变异（去 `confirmLoading`）红在断言 |
| W4-M4 提交按钮 loading · Settings.tsx | ✅ 完成 | 渠道 Modal（`onOk={handleSaveChannel}`）加 `channelSaving` state + `confirmLoading`，`finally` 复位。1 用例绿（createChannel pending 时保存按钮 `ant-btn-loading` + 连点只调一次），变异（去 `confirmLoading`）红在断言。**M4 全部 4 文件收口** |
| W4-M5 危险操作确认 · AlertSuppressions.tsx | ✅ 完成 | 删除规则 Popconfirm 四件套（title 带对象名 + `okButtonProps danger`）；1 用例绿，变异（去 `okButtonProps`）红在 `toHaveClass('ant-btn-dangerous')` 断言 |
| W4-M5 危险操作确认 · Runbook.tsx | ✅ 完成 | 删除 Runbook Popconfirm 四件套（title 带 `rb.title` + `okButtonProps danger`）；1 用例绿，变异（去 `okButtonProps`）红在 `toHaveClass('ant-btn-dangerous')` 断言 |
| W4-M5 危险操作确认 · Oncall.tsx（2 处） | ✅ 完成 | 值班组（SchedulesTab）+ 升级策略（PoliciesTab）两处 Popconfirm 四件套（title 带 `r.name` + `okButtonProps danger`）；2 用例绿，变异（去两处 `okButtonProps`）两用例均红在 `toHaveClass('ant-btn-dangerous')` 断言。**M5 全部 4 处收口** |
| W4-M6 required 无 message · Settings.tsx（10 处） | ✅ 完成 | 渠道 Modal name/type/smtp_host/smtp_port/smtp_user/from/to/dingtalk-url/wechat-url/webhook-url 统一「请输入/请选择 XXX」；3 用例绿，变异（去 smtp_host message）红在「请输入SMTP服务器」断言 |
| W4-M6 required 无 message · Oncall.tsx（2 处） | ✅ 完成 | 值班组 + 升级策略「名称」→ `message: '请输入名称'`；2 用例绿，变异（去值班组名称 message）红在「请输入名称」断言 |
| W4-M6 required 无 message · TicketFormModal/AssetFormModal（2 处） | ⏭️ 豁免 | priority/status 均默认值预填（`'normal'`/`'active'`）且 Select 无 `allowClear`，required 永不触发（死代码），加 message 违反「不为不可能状态写代码」且变异反证无法做。**M6 实际收口 12 处（Settings 10 + Oncall 2）** |
| W6 批 1 · P13 通知 pending 索引（迁移 000016） | ✅ 完成 | `idx_notification_logs_pending (sent_at) WHERE status='pending'`；dbsmoke 形态 + EXPLAIN 断言；Down 链 16→13；变异红在 `NotEmpty` 断言 |
| W6 批 1 · P14 alerts problem_start 索引（迁移 000017） | ✅ 完成 | `idx_alerts_problem_start (problem_start)`；dbsmoke 形态 + EXPLAIN 断言；Down 链 17→13；变异红在 `NotEmpty` 断言 |
| W6 批 1 · P15 alerts trigger_id 索引（迁移 000018） | ✅ 完成 | `idx_alerts_trigger_id (trigger_id)`；dbsmoke 形态 + EXPLAIN 断言（纯 trigger_id 条件，因 status 索引竞争）；Down 链 18→13；变异红在 `NotEmpty` 断言 |
| W6 批 1 · P16 tickets external_id 索引（迁移 000019） | ✅ 完成 | `idx_tickets_external_id (external_id)`；dbsmoke 形态 + EXPLAIN 断言（真实查询单条件）；Down 链 19→13；变异红在 `NotEmpty` 断言 |
| W6 批 1 · P17 assets name 索引（迁移 000020） | ✅ 完成 | `idx_assets_name (name)`；dbsmoke 形态 + EXPLAIN 断言（真实查询单条件）；Down 链 20→13；变异红在 `NotEmpty` 断言 |
| W6 批 1 · P18 audit_logs path 索引（迁移 000021） | ✅ 完成 | `idx_audit_logs_path (path text_pattern_ops)`；dbsmoke 形态（path + text_pattern_ops）+ EXPLAIN 断言；Down 链 21→13；变异红在 `NotEmpty` 断言 |
| W6 批 1 · P19 ticket_service cursor Count（3 行，无迁移） | ✅ 完成 | 把无条件 `COUNT(*)` 挪到 cursor 分支之后；cursor 模式不再跑 Count；单测 + 变异（挪回）红在 `SELECT count(*)` 未匹配 |
| 批 2（M1/M2/M3+P4/P5/P6/P7/M11/M13/M14/M15） | 🔄 进行中 | M14、M7、M11、M1(五页全收口)、M2(AlertTable+TicketTable) 已完成（rev30–41）；剩 M2(其他表)/M3+P4/P5/P6/P7/M13/M15 |
| 批 2 · M14 批量操作确认 | ✅ 完成 | Alerts「批量确认/解决」套 Popconfirm 二次确认（title 带已选数量）；1 用例绿；变异（去 Popconfirm）红在确认框缺失 |
| 批 2 · M7 升级策略 JSON 异常文案 | ✅ 完成 | Oncall 升级策略 Levels 非法 JSON 给友好中文（结构化编辑器登记不做）；1 用例绿；变异（去 SyntaxError 判断）红在 toHaveBeenCalledWith |
| 批 2 · M11 严重度配色统一（AssetTimeline + Runbook + AlertSuppressions） | ✅ 完成 | 三处自造配色（AssetTimeline 6 档错位 / Runbook 2 档 / AlertSuppressions 3 档）统一到 SEVERITY_META 权威 6 档；3 处各 1 用例绿 + 变异红 |
| 批 2 · M1 标题体系统一（五页全收口） | ✅ 完成 | Oncall「值班管理」、AlertSuppressions「告警抑制」+ 按钮入 extra、Topology「网络拓扑」、Settings h2 改「系统设置」、Runbook 手写 Space 改「故障 Runbook」+ 计数 Tag/按钮入 extra（h4）；各 1 用例绿；变异均红在 heading 断言。AssetTimeline 子页（带返回链接）+ MetricSnapshot（标题带 Tag）无对应 slot，登记待定 |
| 批 2 · M2 表格排序（全站零 sorter） | 🔄 进行中 | AlertTable 四列（rev40）+ TicketTable 六列（rev41，优先级按 PRIORITY_WEIGHT 权重）已加前端本地排序；各 1 用例绿；变异（去 created_at sorter）红在行顺序断言。**剩 AssetTable/Runbook/Oncall/MetricSnapshot 等表格** |

**下一步（按顺序）**：
1. ~~W1 逐页推进~~ → W1 全部 11 页已完成（Dashboard/Alerts/Assets/Tickets/Oncall/AlertSuppressions/MetricSnapshot/Racks/Topology/Runbook/AssetTimeline）。
2. ~~W2 剩余 `Settings:923`~~ → 已完成（rev8）。**W2 全部 7 个调用点收口**。
3. ~~W4-H6 cssVar 实测~~ → 已完成（rev9，方案①）。~~W4-H8~~ → 已完成（rev10）。~~W4-H9~~ → 已完成（rev11）。~~W4-H10~~ → 已完成（rev12）。~~W4-M4 AlertSuppressions~~ → 已完成（rev13）。~~W4-M4 Oncall~~ → 已完成（rev14）。~~W4-M4 Runbook~~ → 已完成（rev15）。~~W4-M4 Settings~~ → 已完成（rev16）。~~W4-M5 AlertSuppressions~~ → 已完成（rev17）。~~W4-M5 Runbook~~ → 已完成（rev18）。~~W4-M5 Oncall~~ → 已完成（rev19）。~~W4-M6 Settings~~ → 已完成（rev20）。~~W4-M6 Oncall~~ → 已完成（rev21）。~~W4-M6 TicketFormModal/AssetFormModal~~ → 豁免（rev22，死代码）。**W4 批 1 全部收口（H1/H6/H8/H9/H10/M4/M5/M6）**。
4. ~~W6 批 1 逐索引推进~~ → **W6 批 1 全部收口（P13–P19：迁移 000016–000021 六个索引 + ticket_service cursor Count）**。
5. ~~批 2 启动~~ → **M14（rev30）+ M7（rev31）+ M11 三处（rev32/33/34）+ M1 五页（rev35–39）+ M2 AlertTable（rev40）+ M2 TicketTable（rev41）已完成**；剩 M2 排序（AssetTable/Runbook/Oncall/MetricSnapshot）、M3 分页+P4 表格 memo（同批）+P5 服务端分页+P6 进度节流、M13 移动端、M15 空态/加载态。

**已知阻塞/待确认**：M16（工单优先级域 normal vs medium）待定契约后才能改，本轮只做显示兜底。

**测试盲区（W1 系列共有）**：本批单测 mock 掉 `useApiQuery`，故 `queryFn` 内的形状归一（`Array.isArray(items) ? items : []`）与 `catch` 删除不被单测覆盖。防回归靠两点：① `MOCK_*` 常量已从源码删除，重新引入无法通过编译；② `isError` 分支有断言。接口真实形状逐页核对 handler——`oncall_handler.go:35/124/135` 均返回 `{code,data:[...]}` 裸数组，`apiGet` 解包后即数组。
