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

**M16（rev4 新增）→ 2026-09-10 已收口，见 `docs/FIX-PLAN-M16-PRIORITY.md`**：工单**优先级域跨层不一致**。契约 `openapi.yaml:2448-2450` 的 priority enum 是 `[critical, high, normal, low]`，前端表单默认值/筛选/标签都按 `normal`；但 `cmd/seed/main.go:280` 与 `integration/glpi.go:161` 写入的是 `medium`（`models/ticket.go:17` 注释也是 medium）。后果：GLPI/seed 工单用「普通」筛选筛不到。当时只加了显示兜底（`medium: '普通'`），**未改域**。

**收口内容（选项 (a)：以契约为准）**：迁移 `000023` 把存量 `medium` 归一为 `normal`；三个写入方（glpi 映射 / `priorityFromSeverity` / seed 字面量）同批改 `normal`；`models/ticket.go` 注释同步；`Create` 补 `priority=''` 的兜底（`POST /tickets` 不传 priority 原会落空串，是活路径）。两处前端同义词字典按设计**保留**为未迁移行的安全网（注释已写明保留原因与删除条件）。回归网：两个映射函数的「输出 ∈ 契约词表」断言 + dbsmoke 升级路径的存量行归一/对照行不动/全表无词表外值三条断言 + `Load()` 撞号报错。同一类问题还有 `TicketStatsCards` 的档位（已在 W1 按契约修正为 5 档）。

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
| rev42 | 2026-09-10 | **批 2 第 13 步 M2（表格排序）第 3 处 AssetTable 完成**：`AssetTable` 给名称/类型/机房/机柜/状态（`localeCompare`，机房机柜空值 `?? ''`）加前端本地排序；IP 地址走 `ipCompare` 八位组数值序（字典序会把 `192.168.1.10` 排在 `192.168.1.2` 前），空值排最后、非 IPv4 回落 `localeCompare`。1 用例绿（点「IP 地址」表头后第一行由 `192.168.1.10` 的 web-server-01 变 `192.168.1.2` 的 db-server-01 数值升序），变异（IP sorter 改字典序）红在 `toContain('db-server-01')` 断言。**剩 Runbook/Oncall/MetricSnapshot 等表格** |
| rev43 | 2026-09-10 | **批 2 第 14 步 M2（表格排序）第 4 处 Runbook 完成**：`Runbook` 给标题/类型（`localeCompare`）/严重度（`severity` 数值）/启用（`Number(enabled)` 布尔权重）加前端本地排序；标签是多值逗号串、排序无意义不加（同 AlertTable message）。1 用例绿（点「严重度」表头后第一行由 severity 5 的「接入交换机端口 down」变 severity 4 的「主库复制延迟排查」数值升序），变异（去 severity sorter）红在 `toContain('主库复制延迟排查')` 断言。**剩 Oncall/MetricSnapshot 两表** |
| rev44 | 2026-09-10 | **批 2 第 15 步 M2（表格排序）第 5 处 Oncall 值班组表完成**：值班组表给名称/时区/启用/说明加前端本地排序（名称/说明 `localeCompare`，时区空值默认 `Asia/Shanghai`，启用布尔权重）。**副产物**：sorter 让表头 `<th>` 带 `aria-label=title`，与既有 M4 测试 `getByLabelText('名称')`（定位表单输入）冲突 → 改 `getByRole('textbox', { name: '名称' })` 精确定位。1 用例绿（点「名称」表头后 ops-team 变 dev-team 字母升序），变异（去 name sorter）红在 `toContain('dev-team')` 断言。**剩 Oncall 升级策略表/MetricSnapshot** |
| rev45 | 2026-09-10 | **批 2 第 16 步 M2（表格排序）第 6 处 Oncall 升级策略表完成（Oncall 全收口）**：升级策略表给名称（`localeCompare`）/层级数（`levels?.length` 数值）/启用（布尔权重）加前端本地排序；层级详情是多值 Tag 列表不加。1 用例绿（点「层级数」表头后 2 级的 critical 变 1 级的 info 数值升序），变异（去 levels sorter）红在 `toContain('info')` 断言。**剩 MetricSnapshot 一表** |
| rev46 | 2026-09-10 | **批 2 第 17 步 M2（表格排序）第 7 处 MetricSnapshot 完成（M2 全站收口）**：`MetricSnapshot` 给时间（`new Date(...).getTime()`，RFC3339 字典序会错序）/Asset ID/Key（`localeCompare`）/Value（数值）加前端本地排序。1 用例绿（点「Value」表头后 60.30 变 45.20 数值升序），变异（去 value sorter）红在 `toContain('45.20')` 断言。**M2 全站收口：AlertTable/TicketTable/AssetTable/Runbook/Oncall 两表/MetricSnapshot 七个表格** |
| rev47 | 2026-09-10 | **批 2 第 18 步 M3/P5（服务端分页）第 1 处资产页完成**：列表无服务端分页——`assetApi.list()` 不带参数默认 `page=1/page_size=20`，`total` 被丢弃，antd 分页是假分页（对已截断的 20 条再分页）。改三处：① `openapi.yaml` `/assets` GET 补 `keyword` 参数（后端 `asset_handler.go:47` 已支持但契约未声明 → 重新 `gen:api`）；② `Assets.tsx` 加 `page/pageSize` state，fetcher 传 `page/page_size/keyword/type`，解包 `items+total`，删前端 `filtered`（原只对已拉取的 20 条过滤、静默漏页），筛选变化重置 `page=1`；③ `AssetTable` 加 `total/page/pageSize/onPageChange` 受控分页。2 用例绿（副标题「共 100 台资产」用服务端 total；翻页/筛选更新 queryKey 的 page/keyword），两条变异（subtitle 改 `items.length` / 删 `onChange`）均红在断言。**待确认**：keyword 语义=`name/asset_tag/sn`（不含 IP，因 assets 表无 IP 列），与 M9 placeholder「名称/IP」有口径差，见 §8 阻塞 |
| rev48 | 2026-09-10 | **批 2 第 19 步 M3/P5（服务端分页）第 2 处工单页完成**：工单页 `ticketApi.list()` 不带 `page` 参数默认 `page=1/page_size=20`，`total` 被丢弃，antd 分页是假分页（对已截断的 20 条再分页）。改四处：① `openapi.yaml` `/tickets` GET 补 `page`/`page_size` 参数（后端 `ticket_handler.go:27-28` 已支持但契约未声明 → `gen:api`）；② `apiClient.ts` 加 `TicketListParams`（`typePath<'/tickets','get','parameters'>`，与 AssetListParams 对称）+ `api.ts` `ticketApi.list` 手写内联参数改 `TicketListParams`；③ `Tickets.tsx` 加 `page/pageSize` state，`fetchTickets` 返回 `{items,total}`，下沉 `page/page_size/status/priority`，删前端过滤（筛选本就下沉 queryKey，无本地 filtered），筛选变化重置 `page=1`，副标题用服务端 `total`，统计卡 `fetchTickets` 改读 `.items`；④ `TicketTable` 加 `total/page/pageSize/onPageChange` 受控分页。10 用例绿（含新增 M3/P5 翻页/筛选更新 queryKey 的 page/status + 副标题「共 100 个工单」用服务端 total），两条变异（删 `onChange` / 副标题改 `list.length`）均红在断言。**剩告警页** |
| rev49 | 2026-09-10 | **批 2 第 20 步 P4（表格 memo 化）第 1 处 AssetTable 完成**：`AssetTable` 未 memo + `columns` 每渲染重建 → 父组件无关 state 变更整表重渲染（审计实测 100 行 12–34×）。改三处：① `AssetTable` 包 `React.memo` + `columns useMemo` + `handleDelete useCallback`；② `Assets.tsx` 7 个回调 useCallback 化（`handleEdit/handleDiagnose/handlePostmortem/handleRetire/handleRestore/handlePageChange/handleChanged`），否则每渲染新建箭头函数让 memo 白搭；③ 新增 `AssetTable.memo.test.tsx`。验证策略：不用 `<Profiler>`（React 18 下 memo bailout 时 onRender 仍触发 update，无法区分 memo/non-memo——已实验证实），改 mock antd Table 在函数体内计数——memo 生效时父组件重渲染 bail out AssetTable 函数体、Table 不被再次调用。1 用例绿，变异（去 memo 包裹）红在「Table 调用计数不变」断言（received 2 vs 1）。**剩 AlertTable 一处** |
| rev50 | 2026-09-10 | **批 2 第 21 步 P4（表格 memo 化）第 2 处 AlertTable 完成（P4 全收口）**：`AlertTable` 未 memo + `columns` 每渲染重建 → 父组件无关 state 变更整表重渲染。改三处：① `AlertTable` 包 `React.memo` + `columns useMemo`（依赖 onAck/onResolve/onMarkFP）；② `Alerts.tsx` 回调 useCallback 化——解构出 `mutate`（React Query 的 `.mutate` 引用稳定）再依赖，绕开 exhaustive-deps 误判（ackMut 对象每渲染新建但 `.mutate` 稳定）；③ 新增 `AlertTable.memo.test.tsx`（与 AssetTable 同套 mock antd Table 计数方案）。1 用例绿，变异（去 memo 包裹）红在「Table 调用计数不变」断言。**P4 全收口（AssetTable rev49 + AlertTable rev50）** |
| rev51 | 2026-09-10 | **批 2 第 22 步 P7（MobileCardList key 稳定）完成**：`MobileCardList` 用 `key={idx}` → 过滤/新增时 DOM 复用错位。改 `useResponsiveTable.tsx:59` `key={item.id ?? idx}`（id 缺失时回落 idx）。测试直接测 `MobileCardList` 组件（绕过 jsdom 断点判断，P7 原注「jsdom 测不到」指经 Assets 页在 xs 断点测不到），用非受控 input + 顺序反转验证 key 语义：`key={item.id}` 时编辑内容跟随 id（DOM 复用正确），`key={idx}` 时错位。1 用例绿，变异（key 改回 idx）红在 `toHaveValue('edited')`（received 'b'）。 |
| rev52 | 2026-09-10 | **批 2 第 23 步 M13（移动端）「AlertTable 双渲染」完成**：告警页缺移动端适配，`Alerts.tsx` 直接 `<AlertTable scroll={{x:1000}}>`，xs 断点横向溢出（Assets 页已有 `isMobile ? MobileCardList : AssetTable`，告警页没跟上）。改三处：① `AlertTable.tsx` 抽 `getAlertActions` 纯函数（操作按钮决策：problem→确认+解决 / acknowledged→解决 / 未误报→标记误报 / 已误报→取消误报），桌面端操作列与移动端卡片共用，避免 status/is_false_positive 分支漂移（H9 教训）；② 新建 `AlertCard.tsx` 移动卡片（host/message/SeverityTag/StatusTag/时间 + 操作按钮）；③ `Alerts.tsx` 加 `useResponsiveTable` + `isMobile ? MobileCardList(renderCard=AlertCard) : AlertTable` 双渲染。6 用例绿（字段渲染 / problem 确认+解决 / acknowledged 只解决 / 标记误报 onMarkFP(id,true) / 取消误报 onMarkFP(id,false) / 无 onMarkFP 不渲染），两条变异（onAck 传错 id / 反转 is_false_positive 条件）均红在断言。**Sider breakpoint 豁免**（响应式视觉 jsdom 测不到、无法变异反证，同 P7 原注）。**已知预存在失败**：`Assets.mobile.test.tsx`（rev47 改 `data:{items,total}` 后该文件 mock 未同步仍返回数组，基线 f48850f 已红）与本轮无关，下轮修。 |
| rev53 | 2026-09-10 | **测试卫生：修复 `Assets.mobile.test.tsx` 预存在失败（rev47 遗留）**：rev47 服务端分页把 Assets 的 `data` 结构改为 `{items,total}`，但 `Assets.mobile.test.tsx` 的 `useApiQuery` mock 仍返回 `data: h.assets`（裸数组），`data.items`=undefined → 列表空 → 「维护」卡不渲染，基线 f48850f 起 CI 红。改一处：mock `data` 改 `{ items: h.assets, total: h.assets.length }`（对齐 `Assets.test.tsx:27` 的 `{items,total}` 结构）。1 用例绿（maintenance 显示「维护」而非「离线」），两条变异（items 改 `[]` / status 改 `offline`）均红在 `findByText('维护')` 断言。 |
| rev54 | 2026-09-10 | **批 2 P6（进度节流）豁免**：`runBulk` 每条 `setBulkProgress` 触发全页重渲染。P4 memo 化（rev49/50）已消解「整表重渲染」（AlertTable memo bailout，data/loading/onAck 等 props 不变），剩余「全页轻量组件」（PageHeader/AlertStatsCards/Select/Progress Modal）重渲染 CPU ~几 ms/次、分散在 100 条串行网络请求（每条 await ack/resolve ~100ms）的 10s+ 耗时里，用户无感知。节流需 ref 累计 + throttle + 强制最后刷新（否则进度停 98%），复杂度 > 收益且进度显示跳变。文档 P6 原注「P4 落地后基本消解；若仍做节流」条件句预示可选。按「不为将来可能的收益写代码」豁免，同 M6 rev22「死代码豁免」类。 |
| rev55 | 2026-09-10 | **批 2 第 24 步 M3/P5（服务端分页）第 3 处告警页完成（M3/P5 全收口）**：告警页 `alertApi.list()` 无 `page` 参数默认 `limit=100`、`total` 丢弃、antd 假分页（对 100 条再分页），且与资产/工单页分页口径不一致。改小步 2+3（契约 + 前端一起提交，契约单独不可独立交付，沿 rev47/48 先例）：① `openapi.yaml` `/alerts` GET 补 `page`/`page_size` 参数（默认 1/20，后端 `alert_handler.go` 已支持）+ `AlertList` 补 `total`（过滤后总数）→ `gen:api`；② `Alerts.tsx` 加 `page/pageSize` state，fetcher 下沉 `page/page_size/status/severity`，解包 `{items,stats,total}`（`total` 过滤后 vs `stats.total` 全表语义分离，§8 核心决策），筛选变化重置 `page=1`；③ `AlertTable` 加 `total/page/pageSize/onPageChange` 受控分页。11 用例绿（新增 2：分页器显示服务端 total「共 100 条」而非 items.length / 翻页更新 queryKey 的 page + 筛选重置回 page 1），两条变异（`total={list.length}` / 删 `onPageChange`）均红在断言。`tsc --noEmit` + `eslint` + 全量 vitest（37 文件 303 用例）全绿。**M3/P5 三页全收口（资产 rev47 + 工单 rev48 + 告警 rev55）** |
| rev56 | 2026-09-10 | **M17 `PUT /tickets/:id` 不可变字段收口**（承接 M16 登记的「`PUT` 是任意 map 直落 `Updates()`，mass-assignment 面更宽」）：handler 把请求体绑成 `map[string]interface{}` 直接交给 gorm `Updates(map)`，gorm 对每个键走 `Schema.LookUpField(k)`（先列名再 **Go 字段名**，均大小写敏感），故 `{"id": …}` 改写主键、`{"TicketNumber": …}` 改写工单号、`{"created_at": …}` 失真审计时间线。主键被改写后 D-3 的 `alerts.ticket_id` 指向不存在的工单（悬空）。修法沿用 `channel_service.go` 先例（T-37）但**用禁改集合而非可改白名单**：tickets 有 20 个业务列，绝大多数本就该可改（status/assignee/priority/resolution/due_date…），列白名单既维护不起，也会把外部对接要写的 `external_id`/`resolved_at` 静默丢掉，真正不能碰的只有 `id`/`ticket_number`/`created_at`/`updated_at` 这 4 个；键同收列名与 Go 字段名两种小写形态。归一化**原地两阶段**（先只读收集重命名，再落地）而非另建 norm map——既有用例钉住「调用方拿到的这张 map 里能看到注入的 `closed_at`」。顺带修掉一处既有不一致：`{"Status":"closed"}` 走 gorm 能写列但 `updates["status"]` 取不到 → `closed_at` 不写；小写化后两者一致。handler 补 `ErrInvalidInput`→**400**（漏了会变 500，把「调用方写错字段」报成服务端故障，运维只会重试同样的请求）。7 service 子用例（小写/Go 字段名两路 × 各列）+ 1 正控（Go 字段名写业务列照常更新）+ 1 handler 400 用例绿；3 条变异（V-1 守卫 `if false` / V-2 去掉小写归一 / V-3 handler 丢 400 映射）全红在业务断言，sha256 校验还原字节一致。`gofmt` + `go vet` + `go test ./...` + `tsc --noEmit` + `eslint` + 全量 vitest（37 文件 312 用例）全绿。**未做（登记）**：openapi 未文档化该不可变规则（避免 `gen:api` 漂移，独立小步再做） |
| rev57 | 2026-09-10 | **M18 `POST/PUT /tickets` 枚举列取值收口（priority + status）**（补记：本轮实际已提交为 `f831539`，§7 漏行，此次回填）：M17 封「哪些列能写」（键域），M18 封同一端点上正交的另一轴——「这些列收哪些值」（值域）。`tickets.priority`/`status` 是裸 `VARCHAR(20) NOT NULL`、全仓无 CHECK 约束。危害形态是**静默消失**：筛选器选不中、`TicketStatsCards` 的 `in` 判断不计数、`PRIORITY_WEIGHT[未知] ?? 0` 静默垫底、超长撞 `VARCHAR(20)` → 500（本该 400）。判据以 openapi enum 为准（priority `normal` 非 `medium`，同 M16）；**拒绝而非静默归一并大小写严格**（宽容会再造出「已关闭但无关闭时间」，正是 M17 刚修的）。`validateTicketEnumValues` 借道 `Create`/`Update` 同一函数，**fail-closed**（非字符串一律拒，`docs/TRAPS.md` T-36 复发点）。顺带修 handler 写死文案「工单标题不能为空」变假话。6 条变异全红在业务断言，含 V-4「fail-open 翻回 T-36」并核对红掉的恰是非字符串那几条。**未做（登记）**：`glpi.go` `ConvertToTicket` 空值缺口 / 存量脏数据诊断 / openapi 文档化。 |
| rev58 | 2026-09-11 | **M19 告警状态迁移收口（终态保护 + 幂等）**（承接 M17/M18 的「M17 是 tickets 端点、告警端点同病」）：告警的合法状态机**只活在前端** —— `getAlertActions`（`AlertTable.tsx:66`）给 problem 才出「确认」、problem/acknowledged 才出「解决」，而后端四条写路径（`Acknowledge`/`Resolve`/`BulkAcknowledge`/`BulkResolve`）**只按 id 更新**，`PUT /alerts/{id}/ack` 打在一个已 resolved 的告警上会把 `status` **回退**成 `acknowledged`。危害不只是状态错：该行掉出 `dashboard_service.go` 的 `ResolvedAlerts` 计数、重新落进待处理桶让值班再处理一遍、并多发一条「已确认」通知；重复 `Resolve` 把 `resolve_time` 推到 now → **MTTR 虚高**，重复 `Acknowledge` 推 `ack_time` → **MTTD 虚高**。前端那道守卫挡不住：列表 5s 轮询，两个值班同时点同一条时后者的页面仍是旧状态。修法：合法源状态写进 **UPDATE 的 WHERE**（`id = ? AND status IN (...)`）而非「读-判-写」——后者有 TOCTOU 窗口，正是要修的缺陷本身；`RowsAffected == 0` 走冷路径复读 `classifyAlertNoRows` 区分**幂等成功 / ErrInvalidState / ErrNotFound**。重复请求分两类：**已在目标态**（ack 一个 acknowledged）→ 幂等成功且**不重写时间戳**；**已在更后的终态**（ack 一个 resolved）→ 拒绝。新增 `ErrInvalidState`（与 `ErrInvalidInput` 分开：那是「参数写错了」400，这是「来晚了」409），HTTP 映射 **409**（落 500 会被当成服务端故障去重试、落 400 会被当成改参数，两者都指错方向，正确动作是刷新列表），gRPC 映射 `FailedPrecondition`。批量路径**不因个别 id 已终态而整批失败**（运维勾 20 条、其中 3 条同事已处理完，拒整批是最糟的选择），affected 如实报数。**测试**：service 8 用例（含「没有 UPDATE」用**不设期望**证明拒绝即没写库，而非数调用次数；含两条并发 0 行复读路径 + 复读时记录已删返 `ErrNotFound`）+ handler 2 用例（409 带真实原因）。**变异反证**：先跑出一次污染（脚本只在结尾还原一次，V-5 的变异残留进 V-6 的红色集合），改成每轮全量还原后重跑，6 条（V-1..V-6）红在断言且红色集合精确。**真库语义**：单测那条只正则匹配 SQL 文本、证不了 Postgres 真按条件筛行，故新增 `TestDBSmoke_AlertBulkTransitionGuards`（自建自清三行 problem/acknowledged/resolved，断言 affected 如实报数 + resolved 行状态与 `resolve_time`/`ack_user` 均不被改写），并**对真 PG 再跑两条变异**（S-1/S-2 把守卫从 WHERE 拿掉）确认真库用例会红——首次跑它时因 `scripts/db_smoke.sh` 用 `-run` 白名单、新用例不在名单里而**静默没跑**（假绿），已登记 `docs/TRAPS.md` T-42。 |
| rev59 | 2026-09-11 | **M20 `alerts.status` 库默认值漂移收口**（做 1.9 登记的 ①）：`000001_init.up.sql:646` 是 `DEFAULT 'firing'`、`models/alert.go` 是 `default:problem`，本仓用迁移建库不用 AutoMigrate，故库里那行是既成事实。`firing` 全仓只出现在 000001 的 DDL 与三份 testdata —— 不在前端 `getAlertActions` 的三个状态里、也不进 `dashboard_service.go` 计数分支，落库即**静默卡死**（列表里看得见、按钮一个不给、统计不计数）。迁移 `000024` 把库默认值对齐到 `problem`（契约里「未处理」的初始态）。**本轮最重要的产出是一次自我纠错**：变异实验（up 换成 `SELECT 1`）显示只有裸 SQL 那条断言红、GORM `db.Create` 那条照样绿 → GORM 对带 `default:` tag 的零值字段是**用它自己解析的 tag 值替代**，不省略该列、不吃库默认值；我在 1.9 ① 与迁移注释初版里写的「GORM 零值省略该列、于是落库默认值」**是错的**，已按实测改写迁移 up/down 与用例三处注释。据此重新定性：**这是潜伏缺陷不是活 bug** —— 显式赋值方不落 `firing`，漏赋值方被 tag 兜成 `problem`，grep 确认全仓非测试 Go 里无裸 `INSERT/UPDATE alerts`；本迁移的定位是**拆陷阱 + tag/库两侧对齐**，不为它编更吓人的理由。**测试**：`TestDBSmoke_AlertStatusDefault` 三条断言（information_schema 列默认值 / 裸 SQL 落 problem / GORM 落 problem），③ 已实测**不依赖** 000024 但留着钉「tag↔落库值↔契约初始态」不脱节；Down 链 10→**11** 次并加「第一次滚的确实是 000024」正向断言 + 默认值退回 `firing` 断言，顺带去掉注释里会静默过期的 Down 序号（000023 数据迁移刻意不可逆 vs 000024 可逆，不对称是有意的）。**变异** 3/3 红在断言（V-1 up 换 SELECT 1 / V-2 up 改回 firing / V-3 down 变 no-op），sha256 还原字节一致。门禁全绿（gofmt / vet / vet-dbsmoke / go test / 真 PG 双路径冒烟 / tsc / eslint）；**全量 vitest 未跑**（`git status` 确认前端 0 改动，vitest 覆盖不到本轮改动，如实记录）。 |
| rev60 | 2026-09-11 | **M21 gRPC 错误映射收口**（做 1.9 登记的 ②）：先确认真实暴露面 —— `cmd/server/main.go:75` **无条件**起 gRPC（50051），所以这不是纸面问题。缺陷两个：① `GetAlert` 判 `errors.Is(err, gorm.ErrRecordNotFound)`，而 `service.Get` 返的是 `service.ErrNotFound`（service 层 `alert_service.go:225` 已把 gorm 错误翻译掉）→ **判据永不命中**，「告警不存在」被报成 `codes.Internal`，客户端当服务端故障重试（**1.9 ② 里我写的「GetAlert 已有正确写法可对齐」是错的，已纠正**）；② 5 处 `status.Errorf(codes.Internal, "%v", err)` 把原始错误文本塞进**响应体**，与 `apierr.Respond` 的出口契约（G-28：原文只进日志且经 `redact.Text`）正相反。修法：新增 `serviceErrToStatus(method, err)` 一处集中翻译，**覆盖 service 包全部 5 支哨兵**（只覆盖今天可达的会重犯同病），非哨兵 → `redact.Text` 记日志 + 通用串 `"internal error"`，删掉假判据唯一用途的 `gorm.io/gorm` import。**取舍**：哨兵文案带真实原因（M19 成果，抹平等于推翻它），非哨兵才通用化；`ErrNotFound` 用 `"alert not found"` 而非哨兵的通用英文。**测试** 7 个新用例（含带 `password=sup3rs3cr3t` + 内网 IP 的错误断言响应三者皆无、含反面用例钉住「不许一并抹平」）；变异 5/5 红在业务断言、红色集合精确（V-5 删 `ErrTooManyItems` 支证明「覆盖全部哨兵」是被断言守住的而非口号）。机理层面的教训记成 `docs/TRAPS.md` **T-44**（`gorm.ErrRecordNotFound` 在 service 层对、带到上层就永不命中）。**扫出的两处相邻死代码已登记未动**（`apierr.TranslateDBError` 零生产调用方、`runbook_handler.go:184` 的 `var _ = gorm.ErrRecordNotFound`）。门禁全绿；**全量 vitest 未跑**（改动只有 2 个 grpcserver 文件，前端 0 改动）。 |
| rev61 | 2026-09-11 | **M22 告警路径 `:id` 校验 + ack/resolve 契约补全**（做 1.9 登记的 ③，并修掉侦察时新挖出的两个缺陷）：① **新缺陷** —— `alert_handler.go` 把 `c.Param("id")` **裸字符串**送进 gorm 与 **UUID 列**比较（`alerts.id`/`alert_rules.id` 均为 `UUID PRIMARY KEY DEFAULT gen_random_uuid()`），Postgres 报 `22P02`，既不是 `gorm.ErrRecordNotFound` 也不是哨兵 → 经 `apierr.Internal` 变 **500**，把「调用方 id 写错」报成「服务端故障」。**实测确认**（起 `postgres:18-alpine` 跑 `SELECT ... WHERE id='not-a-uuid'`），不靠推断。修法对齐既有先例 `oncall_handler.go:53`：新增 `alertPathID(c)` 应用到**全部六个**路径参数端点（只修登记的那一个会把同文件留成半修复态）。② **测试假信心** —— 测试路由把 ack/resolve 注册成 **POST** 而生产是 **PUT**（`routes.go:323-324`），三个用例全绿却测着一个**线上不存在的路由形状**；已对齐路由与三个请求方法并写明「改一处要同步改另一处」。③ **契约** —— openapi 给四个端点补 `400`、给 ack/resolve 补 `404`/`409`（M19 的状态冲突一直没进契约）；**只声明实现真会返的码**，`DeleteAlertRule` 刻意不写 `404`（`service.DeleteRule` 不判 `RowsAffected`，删不存在的行返 nil，写了是假承诺 → 登记）。`gen:api` 已重跑并提交产物（CI 有漂移闸门）。**测试**：表驱动 `TestAlertHandler_非法id一律400且不触达service` 覆盖六端点，每条同时钉「状态码 400」与「service 未被调用」（守卫若在查库之后就等于白修）。**变异** `/tmp/m22_mut.py` 5/5 红在断言且红色集合精确：V-1 去 `GetAlert` 守卫红 2；V-2 守卫恒放行红 6；V-3 落 409 红 6（钉的是具体码，不是「非 500」）；V-4 去 `DeleteAlertRule` 守卫只红该端点（证明六端点全覆盖）；V-5 测试路由退回 POST 红既有 ack 两用例。门禁全绿（gofmt/vet/go test/tsc/eslint/vitest Alerts 27 绿/openapi YAML）。**未做（登记）**：批量端点 body 内 id 同一缺口、`/alerts/rules*` 在 openapi 无条目、`DeleteRule` 缺 404 口径不对称、1.9 剩的两处死代码。 |

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
| 批 2（M1/M2/M3+P4/P5/P6/P7/M11/M13/M14/M15） | 🔄 进行中 | M14、M7、M11、M1(五页全收口)、M2(七表全收口)、M3/P5 资产页+工单页+告警页服务端分页 已完成（rev30–55）；P4 表格 memo 全收口（rev49 AssetTable + rev50 AlertTable）+ P7 key 稳定（rev51）+ M13 移动端双渲染（rev52，Sider breakpoint 豁免）+ P6 进度节流豁免（rev54）；剩 M15（登记待决策） |
| 批 2 · M14 批量操作确认 | ✅ 完成 | Alerts「批量确认/解决」套 Popconfirm 二次确认（title 带已选数量）；1 用例绿；变异（去 Popconfirm）红在确认框缺失 |
| 批 2 · M7 升级策略 JSON 异常文案 | ✅ 完成 | Oncall 升级策略 Levels 非法 JSON 给友好中文（结构化编辑器登记不做）；1 用例绿；变异（去 SyntaxError 判断）红在 toHaveBeenCalledWith |
| 批 2 · M11 严重度配色统一（AssetTimeline + Runbook + AlertSuppressions） | ✅ 完成 | 三处自造配色（AssetTimeline 6 档错位 / Runbook 2 档 / AlertSuppressions 3 档）统一到 SEVERITY_META 权威 6 档；3 处各 1 用例绿 + 变异红 |
| 批 2 · M1 标题体系统一（五页全收口） | ✅ 完成 | Oncall「值班管理」、AlertSuppressions「告警抑制」+ 按钮入 extra、Topology「网络拓扑」、Settings h2 改「系统设置」、Runbook 手写 Space 改「故障 Runbook」+ 计数 Tag/按钮入 extra（h4）；各 1 用例绿；变异均红在 heading 断言。AssetTimeline 子页（带返回链接）+ MetricSnapshot（标题带 Tag）无对应 slot，登记待定 |
| 批 2 · M2 表格排序（全站零 sorter） | ✅ 完成 | AlertTable 四列（rev40）+ TicketTable 六列（rev41，优先级按 PRIORITY_WEIGHT 权重）+ AssetTable 六列（rev42，IP 走 ipCompare 八位组数值序）+ Runbook 四列（rev43）+ Oncall 值班组四列（rev44）+ Oncall 升级策略三列（rev45）+ MetricSnapshot 四列（rev46）共七表已加前端本地排序；各 1 用例绿；变异（去/改 sorter）红在行顺序断言 |
| 批 2 · M3/P5 服务端分页（资产页） | ✅ 完成 | 资产页 `assetApi.list()` 原不带参数默认截断 20 条、`total` 丢弃、antd 假分页 → openapi 补 `keyword` 参数 + Assets.tsx 加 page/pageSize 下沉 page/page_size/keyword/type + 删前端 filtered + AssetTable 受控分页（total/page/pageSize/onPageChange）；2 用例绿（副标题用服务端 total / 翻页筛选更新 queryKey）；两条变异（subtitle 改 items.length / 删 onChange）均红在断言。**剩告警页** |
| 批 2 · M3/P5 服务端分页（工单页） | ✅ 完成 | 工单页 `ticketApi.list()` 原不带 page 参数默认截断 20 条、`total` 丢弃、antd 假分页 → openapi 补 `page`/`page_size` 参数 + `apiClient.ts` 加 `TicketListParams` + `api.ts` `ticketApi.list` 改 `TicketListParams` + Tickets.tsx 加 page/pageSize 下沉 page/page_size/status/priority + `fetchTickets` 返回 `{items,total}` + TicketTable 受控分页（total/page/pageSize/onPageChange）；10 用例绿（含 M3/P5 翻页/筛选更新 queryKey + 副标题用服务端 total）；两条变异（删 onChange / 副标题改 list.length）均红在断言。**剩告警页** |
| 批 2 · M3/P5 服务端分页（告警页） | ✅ 完成 | 方案 B 三小步全收口：小步 1 后端 offset 分页（`AlertFilter` 加 `Page/PageSize`，`List` 三元→四元 `(items, stats, total, err)`，switch 三路径 cursor→offset→limit，offset 分支过滤后 Count + Offset/Limit，pageSize 20/500 对齐 asset/ticket；handler 解析 `page/page_size` 响应加 `total` 过滤后 + `cursorMode` 门控 `next_cursor`；gRPC 适配四元）；小步 2 openapi 契约（`/alerts` GET 补 `page/page_size` + `AlertList` 补 `total` → gen:api）；小步 3 前端受控分页（`Alerts.tsx` 加 `page/pageSize` + fetcher 下沉 + 筛选重置 page 1；`AlertTable` 加 `total/page/pageSize/onPageChange`）。语义分离钉住：`stats.Total` 全表 vs `total` 过滤后。后端单测 + 前端 11 用例绿（含 2 分页用例），变异全红在断言；`tsc` + `eslint` + 全量 vitest 303 用例全绿。**M3/P5 三页全收口** |
| **M16 工单优先级词表归一**（后端驱动，不在 W 系列内） | ✅ **完成** | 见下面「下一步」1.6 |
| **M17 PUT /tickets/:id 不可变字段收口**（后端驱动，不在 W 系列内） | ✅ **完成** | 见下面「下一步」1.7 |
| **M18 POST/PUT /tickets 枚举列取值收口（priority + status）**（后端驱动，不在 W 系列内） | ✅ **完成** | 见下面「下一步」1.8 |
| **M19 告警状态迁移收口（终态保护 + 幂等）**（后端驱动，不在 W 系列内） | ✅ **完成** | 见下面「下一步」1.9 |
| **M20 alerts.status 库默认值漂移收口**（后端驱动，不在 W 系列内） | ✅ **完成** | 见下面「下一步」1.10 |
| **M21 gRPC 错误映射收口**（后端驱动，不在 W 系列内） | ✅ **完成** | 见下面「下一步」1.11 |
| **M22 告警路径 `:id` 校验 + ack/resolve 契约补全**（后端驱动，不在 W 系列内） | ✅ **完成** | 见下面「下一步」1.12 |
| 批 2 · M13 移动端（告警页 AlertTable 双渲染） | ✅ 完成 | 抽 `getAlertActions` 纯函数（操作按钮决策桌面+移动共用，避免 status/is_false_positive 分支漂移）+ 新建 `AlertCard` 移动卡片 + `Alerts.tsx` `isMobile ? MobileCardList(renderCard=AlertCard) : AlertTable` 双渲染；6 用例绿 + 两条变异红（onAck 传错 id / 反转误报条件）。**Sider breakpoint 豁免**（jsdom 测不到响应式）。**预存在失败**：`Assets.mobile.test.tsx`（rev47 改 data 结构后 mock 未同步）已修 rev53 |

**下一步（按顺序）**：
1. ~~W1 逐页推进~~ → W1 全部 11 页已完成（Dashboard/Alerts/Assets/Tickets/Oncall/AlertSuppressions/MetricSnapshot/Racks/Topology/Runbook/AssetTimeline）。
1.5. **D-3 告警一键建单（2026-09-10 完成，出自 `docs/FIX-PLAN-ALERT-TICKET.md`，不在本表 W 系列内）**
   - **后端**：`POST /api/alerts/{id}/ticket` + `TicketService.CreateFromAlert`（认领优先 + 认领与插票同事务）；8 service 用例 + 4 handler 用例绿，3 条变异红在业务断言；openapi + `gen:api` 已同步；TODO.md D-3 收口、`05-运维工单.md` §5.4/§5.5 同步。
   - **前端**（本轮）：入口挂在 `getAlertActions`（`AlertTable.tsx` 纯函数，桌面表格与移动卡片共用，一处实现两界面生效）——**不是**原先假设的「告警详情抽屉」，实测该页没有抽屉，已纠正设计记录 §6/§7。`Alert` 补 `ticket_id` 字段；`ticket_id` 非空渲染 disabled 的「已建单」，否则可点的「建单」。提示分流抽成纯函数 `ticketResultMessage`（`Alerts.tsx` 具名导出）：`created=true`→`success`，`created=false`→**`info`**「该告警已建单」（幂等不是失败，写成 error 会让运维反复重试）。**可见性判断只用来省一次请求，正确性由后端幂等兜底**——让前端判断承担防重职责会在并发下建出两张票。
   - **测试**：`AlertCard.test.tsx` +3（建单/已建单 disabled/未传不渲染）、`Alerts.test.tsx` +5（点击带对 id、created 两个分支的用户可见文案、纯函数 3 条）；4 条变异全部红在业务断言（V-1 已建单判断置 false / V-2 created=false 走 error / V-3 onSuccess 忽略 created / V-4 丢 disabled 透传），还原字节一致。
   - **测试基建**：`Alerts.test.tsx` 的 `useApiMutation` mock 由「忽略参数、共用一个 mutate spy」改为「每次调用一个独立 spy（转发到共享 spy）+ 留档 opts」——原先 onSuccess 回调分支**没有触发入口**；新写法按「哪个 spy 被点了」反查对应 opts，不依赖调用顺序（新增 mutation 不会悄悄错位）。`antd` 的 `message` 已在 `src/test/setup.ts` 全局 mock，断言打在调用上（jsdom 下静态 message 不落 DOM）。
   - **未做（登记）**：`/tickets` 列表对 `source='alert'` 的筛选口径。（M16 优先级域已于 2026-09-10 收口，见 §4.1 与 `docs/FIX-PLAN-M16-PRIORITY.md`）
1.6. **M16 工单优先级词表归一（2026-09-10 完成，出自 §4.1/§8「已知阻塞」，设计见 `docs/FIX-PLAN-M16-PRIORITY.md`）**
   - **决策**：选项 (a) —— 以契约为准（`normal`），数据与写入方一起收敛。用户 2026-09-10 拍板。
   - **迁移**：`000023_ticket_priority_normalize`（`UPDATE tickets SET priority='normal' WHERE priority='medium'`，down 显式声明不可逆）。序号取 000023 而非 000022（后者被 P20 预占）。
   - **写入方 4 处**：`integration/glpi.go` 映射、`service/ticket_service.go` 的 `priorityFromSeverity`、`cmd/seed/main.go` 字面量、以及 `Create` 里 `priority=''` 的兜底（`POST /tickets` 不传 priority 原会落空串，属**活路径**，非历史脏数据）。
   - **测试**：两个映射函数各加「输出 ∈ 契约词表」+「不产出 medium」断言（GLPI 侧此前**零覆盖**）；`ticket_service_test.go` 补默认值/传值保留断言；dbsmoke 升级路径新增 `TestDBSmoke_TicketPriorityNormalize`（存量 medium 归一 + high 对照行不动 + 全表无词表外值三条断言，seed 行由 `scripts/db_smoke.sh` 预置）。
   - **回滚链**：`TestDBSmoke_DownPreservesLegacyColumns` 由九次 Down 改十次（23→21→…→13），并在**首尾各加一条正向断言**——链上全是 `assert.False(索引还在)`，多滚/少滚都不会被它发现，首钉「第一次 Down 滚的确实是 000023」、尾钉「000012 必须还在」。
   - **附带修复**：`migrate.Load()` 同版本号撞号由「静默覆盖」改为**报错**，up/down 两侧都守，判据用文件名而非 `SQL != ""`（0 字节文件会漏检）；配 `fstest.MapFS` 用例。登记 `docs/TRAPS.md` T-41。
   - **变异反证**：9 条（V-1..V-9）全红在业务断言上，含「up 去掉 WHERE」「up 删掉 UPDATE」「Down 多滚/少滚」「Load 撞号守卫失效（up / down 各一条）」。
   - **未做（登记）**：`POST/PUT /tickets` 的 priority **取值**校验（`PUT` 是任意 map 直落 `Updates()`，mass-assignment 面更宽，单封 priority 会造成「已封住」的错觉）；前端两处同义词字典按设计保留一个版本。
1.7. **M17 `PUT /tickets/:id` 不可变字段收口（2026-09-10 完成，出自 1.6「未做（登记）」的第一句）**
   - **缺陷**：`UpdateTicket` 收 `map[string]interface{}` 直落 gorm `Updates(map)`。gorm 对每个键调 `Schema.LookUpField(k)`——先列名、再 **Go 字段名**，两者均大小写敏感。故 `{"id": …}` 改主键、`{"TicketNumber": …}` 改工单号、`{"created_at"/"CreatedAt": …}` 失真审计时间线。端点可达（公开 `canWrite` 路径），主键被改写后 D-3 建立的 `alerts.ticket_id` 指向不存在的工单（**悬空**）。
   - **修法**：`immutableTicketUpdateFields` 禁改集合（`id` / `ticket_number`+`ticketnumber` / `created_at`+`createdat` / `updated_at`+`updatedat`，即列名与 Go 字段名两种小写形态）+ 键归一化到小写，保证「校验的键 == 落库的键」。handler 补 `ErrInvalidInput`→400。
   - **为什么是禁改集合而不是可改白名单**（与 `channel_service.go` 的取舍相反）：channels 只有 5 个可写列且全部要校验，白名单顺理成章；tickets 有 20 个业务列，绝大多数本就该可改，列白名单既维护不起（每加一列都要同步），也会把外部对接要写的 `external_id`/`resolved_at` **静默丢掉**（静默丢字段比拒绝坏字段更难发现）。真正不能碰的只有 4 个。
   - **为什么原地两阶段而不是另建 norm map**：既有用例 `TestTicketService_Update_关闭工单_写closed_at` 钉住「调用方拿到的这张 map 里能看到注入的 `closed_at`」（`Update` 原地改）。改成新 map 会让这条用例红——而「改一个正在绿的用例去迁就重构」是味道，故改实现保约定。阶段 1 只读不改（避免 `range` 中增删 map），阶段 2 才落地重命名。
   - **顺带修复**：`{"Status":"closed"}`（Go 字段名写法）走 gorm 能写 `status` 列，但 `updates["status"]` 取不到 → `closed_at` 不写，工单「已关闭但无关闭时间」。小写归一后两者一致。此为归一化的**副作用而非目的**，未单独立项。
   - **测试**：`ticket_service_test.go` 新增 `newTicketSQLiteDB`（**手写 DDL，不能用 AutoMigrate**——`models.Ticket.ID` 带 `default:gen_random_uuid()`，sqlite 无此函数；DDL 必须列全 24 列，漏一列 gorm INSERT 就报 "no such column"）+ 表驱动 `TestTicketService_Update_禁改列被拒`（7 个子用例：小写/Go 字段名两路 × id/工单号/审计列）+ 正控「Go 字段名写业务列照常更新」；`rack_ticket_handler_test.go` 新增 `TestTicketUpdate_不可变字段返回400`（mock 返 `ErrInvalidInput`）。后端共 8 用例。
   - **变异反证**：V-1 守卫 `if immutableTicketUpdateFields[lk] {`→`if false`；V-2 `lk := strings.ToLower(k)`→`lk := k`；V-3 handler 去掉 `ErrInvalidInput` 分支。3 条全红在**业务断言**（非编译错），跑完 sha256 校验还原字节一致。手工复核 V-2：恰好 4 个 Go 字段名子用例红、小写子用例绿——证明该守卫挡的正是 Go 字段名那条路。
   - **门禁**：`gofmt -l` 干净、`go vet ./...` 干净、`go test ./...` 全包 ok；`npx tsc --noEmit` 0、`npm run lint` 0、`npx vitest run` 37 文件 312 用例全绿。
   - **未做（登记）**：① openapi 未文档化该不可变规则（加描述会动 `gen:api` 产物，独立小步再做）；② ~~`POST/PUT /tickets` 的 priority **取值**校验仍未做~~ → 已由 1.8 收口（且一并覆盖 status）。
1.8. **M18 `POST/PUT /tickets` 枚举列取值收口（priority + status，2026-09-10 完成，出自 1.6 / 1.7 的同一句登记）**
   - **缺陷**：M17 封的是「**哪些列**能写」（键域），本轮是同一端点上**正交的另一轴**——「这些列**收哪些值**」（值域）。`tickets.priority` / `tickets.status` 在 DB 里是裸 `VARCHAR(20) NOT NULL`，**全仓 migrations/ 无任何 CHECK 约束**（已 grep 核实），而 `Create`/`Update` 此前只校验「标题非空」，取值随便写。
   - **危害形态是「静默消失」而不是报错**——这才是它值得单独立项的原因：词表外的值能落库、能返回，然后就**从用户视野里蒸发**。四条具体路径：① 工单页优先级/状态筛选器只有契约那几档（`Tickets.tsx` 的 Select），选不中这些票；② `TicketStatsCards` 用 `if (t.status in acc)` 累加，词表外状态**不计数**，统计与列表对不上；③ `TicketTable` 的 `PRIORITY_WEIGHT[未知] ?? 0` → 权重 0，排序静默垫底；④ 超过 20 字符撞 `VARCHAR(20)` → PG 报错 → **500**（本该是调用方的 400）。
   - **判据以契约为准**：`openapi.yaml` 的 `Ticket.priority` enum `[critical, high, normal, low]`、`Ticket.status` enum `[open, in_progress, pending, resolved, closed]`（`normal` 而非 `medium`，同 M16）。
   - **两个决策及理由**：
     - **拒绝而不是静默改写**。把它悄悄归一到 `normal` 会把调用方的 bug 藏起来（对方以为写进去了），而 400 + 契约词表是**可行动**的。
     - **大小写严格**，不收 `"Closed"` / `"HIGH"`。宽容会造出「值合法但下游只认小写」的新裂缝：`Update` 里 `closed_at` 的判定是精确比较 `status == "closed"`，收了 `"Closed"` 就又回到 M17 刚修掉的「已关闭但无关闭时间」。契约本身就是小写精确匹配。
   - **修法**：`ticket_service.go` 加 `ticketPriorityValues` / `ticketStatusValues` 两个词表 + `ticketEnumFields` 切片（**用切片不用 map**：map 遍历序随机，两字段同时越界时报错文案会跳变，用例钉不住）+ `validateTicketEnumValues`。`Create` 与 `Update` **借道同一个函数**（不在 `Create` 里另写两个 if）——判据只有一处，日后加枚举列不会漏。
   - **两个顺序约束（都不是随手放的）**：`Update` 里校验放在键归一化**之后**（`{"Status": …}` 此刻已折成小写键，与列名写法走同一判据）、`closed_at` 判定**之前**（先确认 status 在词表内，那条 `status == "closed"` 精确比较才对得上）；`Create` 里放在 `priority` 默认值**之后**（不传 priority 走 `normal` 兜底，不能被误判越界）。
   - **fail-closed**：值不是字符串（数字/对象/数组/null）一律拒绝。写成 `if s, ok := v.(string); ok { 校验 }` 会让这些形态静默放行（`docs/TRAPS.md` T-36），其中 `null` 还会撞 `tickets.status NOT NULL` → 500。报错只提**契约列名**（本方常量），不回显调用方的键或值——两者都是调用方可控字符串，会原样进 400 body（同 M17 的禁改列报错）。
   - **顺带修复**：`CreateTicket` handler 的 `ErrInvalidInput` 分支原写死文案「工单标题不能为空」，加了枚举校验后同一分支承载两种原因，写死那句**会变成假话**（枚举越界被报成标题问题，调用方照着改标题永远改不好）→ 改 `err.Error()`。核实该文案全仓仅 `ticket_handler.go:91` 一处、无测试钉住，`%w` 包装不影响 `assert.ErrorIs`。
   - **覆盖面边界（写进代码注释）**：GLPI 同步（`integration/service.go:270` 的 `CreateInBatches`）与 `cmd/seed` 直连不经过 `TicketService`，各自持有词表（`glpi.go` 的 `ConvertToTicket`），本轮**不覆盖**。它们的空值缺口另见「未做」①。
   - **测试**：service 3 个用例（`TestTicketEnumValues_与契约词表一致` 双向断言 map 与契约字面量**相等**，防日后加列时只改一边；`Create` 6 个越界子用例 + 1 个契约值通过；`Update` 10 个负例含 `{"Priority":"urgent"}` 混合写法、`nil`/数字/布尔/对象/数组 + 1 个正控「`closed_at` 照常写」）；handler 1 个（400 body 带**真实原因**且 `NotContains "标题"`）。dbsmoke 在既有 `TestDBSmoke_TicketPriorityNormalize` 上扩 status 一侧，守**存量**（种子/夹具/迁移写歪会现形），与 service 的**新增写入**防线合起来才完整。
   - **非空前提（自查补的）**：上述 dbsmoke 断言是 `count(*) ... WHERE status NOT IN (...)`，**空表上恒真**——正是本文件 `TestDBSmoke_AssetJSONBBackfill` 注释里点名的「把迁移删掉也是绿」那类假绿。补 `require.NotZero(total)` 钉住 tickets 非空，断言才真的在守东西。
   - **变异反证**：6 条全红在**业务断言**（非编译错），sha256 校验还原字节一致。V-1 掏空拒收分支 / V-2 `Update` 送 `nil` 进校验 / V-3 `Create` 送常量 `"normal"` 进校验（等于不校验调用方的值）/ V-4 fail-closed 翻 fail-open（T-36）/ V-5 handler 退回写死文案。**V-4 的额外价值**：手工核对红掉的子用例名，恰好是那 5 个非字符串用例，字符串用例与正控全绿——证明这条守卫挡的正是 T-36 那条路，不是碰巧把整组用例弄红。写变异时踩到一次：`if !isString || … {` 换成 `if false {` 会让 `s`/`isString` 变成未使用变量 → **红在编译错**，那是无效变异；改成掏空 `if` 体（合法、语义正是「不再拒收」）才红在断言上。
   - **门禁**：`gofmt -l` 干净、`go vet ./...` 干净、`go test ./...` 全包 ok；`npx tsc --noEmit` 0、`npm run lint` 0；**全量 vitest 本轮未跑**——改动是纯后端（5 个文件全在 `backend/`），前端零改动，vitest 不会覆盖到任何被改的代码，跑它只是仪式。真 PG 冒烟 `scripts/db_smoke.sh` 通过（全新 + 升级两条迁移路径 + 冒烟断言）。
   - **未做（登记）**：① `integration/glpi.go` 的 `ConvertToTicket` 有空值缺口——`statusMap[t.Status]` 对 GLPI status 6（待批准）取不到键 → 落 `""`，`priorityMap[0]` 同理；这是**同步侧的入参词表缺口**，与 M18 的「写入方校验」正交，且 `""` 会撞 `NOT NULL` 或成为新的静默消失源，值得独立一步；② 存量脏数据诊断——线上库里**现在**是否已有词表外的 `priority`/`status` 行（M16 文档 R-4「先诊断列出，再决定」，仍未做）；③ openapi 未文档化该取值约束（同 1.7 ①，动 `gen:api` 产物）。

1.9. **M19 告警状态迁移收口（终态保护 + 幂等，2026-09-11 完成，出自「继续找逻辑与操作流程上的问题」的一轮排查）**
   - **缺陷**：告警的合法状态机**只编码在前端**。`getAlertActions`（`frontend/src/components/AlertTable.tsx:66`）决定按钮：`problem` 才给「确认」，`problem`/`acknowledged` 才给「解决」，`resolved` 什么都不给。后端四条写路径（`Acknowledge` / `Resolve` / `BulkAcknowledge` / `BulkResolve`）**只按 id 更新**，对当前状态零检查。于是 `PUT /alerts/{id}/ack` 打在一条已 resolved 的告警上，会把 `status` **回退**成 `acknowledged`。
   - **为什么前端那道守卫不算防线**：列表 5s 轮询。两个值班看到同一条告警，A 先解决，B 的页面还是旧状态（按钮仍在）→ B 点「确认」→ 服务端照单全收。**可见性判断只配用来省一次请求，正确性必须由服务端兜底**——这句是 D-3 立的规矩（rev 见 1.5），本轮是它的反面。
   - **危害不止「状态错」三条具体后果**：① 该行**掉出** `dashboard_service.go` 的 `ResolvedAlerts`（`COUNT(*) FILTER (WHERE status='resolved' …)`）并**重新落进待处理桶**，值班得再处理一遍；② `writeNotificationTrigger` 再发一条「已确认」通知；③ 重复 `Resolve` 把 `resolve_time` 推到 now → **MTTR 虚高**（`AVG(resolve_time - problem_start)`），重复 `Acknowledge` 推 `ack_time` → **MTTD 虚高**。
   - **修法（关键取舍：守卫放 WHERE，不放 Go）**：合法源状态写进 UPDATE 的 WHERE（`id = ? AND status IN (...)`），结果与到达顺序无关；Go 层「读出来判断再写」有 TOCTOU 窗口，会**原样复现**要修的缺陷。`RowsAffected == 0` 走冷路径 `classifyAlertNoRows` 复读一次，区分**幂等成功 / `ErrInvalidState` / `ErrNotFound`**（记录已被删掉时报「状态不对」会让调用方去刷新一个不存在的对象）。
   - **重复请求分两类，不合并**：**已在目标态**（ack 一个 acknowledged、resolve 一个 resolved）→ **幂等成功，不写库**——重写会改掉认领人、并把 MTTD/MTTR 拉长；**已在更后的终态**（ack 一个 resolved）→ **拒绝**。静默 no-op 会让调用方以为生效，正是本仓反复出现的「静默」反模式。
   - **新增错误类型 `ErrInvalidState`**：与 `ErrInvalidInput` 分开。后者是「请求写错了」（400，改参数重试），前者是「来晚了、状态已经走了」（409，刷新列表别再试）。混成一个会让调用方**指错方向**。HTTP 映射 **409 Conflict**（`apierr.Conflict`，body 带 `err.Error()` 真实原因而非写死文案，同 M18 教训）；gRPC 映射 `codes.FailedPrecondition`。
   - **批量路径的取舍**：**不**因个别 id 状态不合法而整批失败——运维勾了 20 条、其中 3 条已被同事处理完，拒掉整批是最糟的选择。让它们自然落空，`affected` 如实报数（UI 直接拿它显示「成功 N 条」）。`BulkResolve` 里那次预读 `Find` 的 WHERE 与后面的 UPDATE **必须同条件**（同 M17「校验的键 == 落库的键」），否则预读给出一个偏大的印象。
   - **测试**：service 8 用例 —— 已解决被拒 / 已确认幂等 / 已解决幂等 / 已确认可解决 / 两条「并发下 0 行再复读」/ 0 行且记录已删返 `ErrNotFound` / 批量两条路都带源状态守卫。**「拒绝必须是没写库」的证法**：不去数调用次数，而是**不设 UPDATE 期望**——sqlmock 对未期望的调用返回错误，实现若偷偷写了库，`res.Err` 非 nil，断言立刻红。handler 2 用例（409 + body 含真实原因）。保持既有两条 sqlmock 用例（`First` → `UPDATE` 形状）**原样绿**，不为了迁就重构去改正在绿的用例。
   - **变异反证**：6 条（V-1 去掉 Acknowledge 的源状态守卫 / V-2 吞掉 `RowsAffected==0` / V-3 幂等判定恒假 / V-4 拿掉 Resolve 幂等早返回 / V-5 BulkAcknowledge 的 WHERE 去守卫 / V-6 handler 丢 409 映射）全红在业务断言，跑完 sha256 校验还原字节一致。**过程记录**：第一版脚本只在结尾还原一次，于是 V-5 的变异残留在文件里污染了 V-6 的红色集合（多红了 2 个 service 用例）——改成**每轮变异前全量还原**后重跑，红色集合恢复精确。这正是 T-36 那条「检查红掉的是哪几条」的用法。
   - **真库语义（单测证不了的部分）**：单测那条 `WHERE id IN (...) AND status IN (...)` 只是**正则匹配 SQL 文本**，它证明不了 Postgres 真按这个条件筛行。故新增 `TestDBSmoke_AlertBulkTransitionGuards`（自建自清三行 problem/acknowledged/resolved，断言 `affected` 如实报数 + resolved 行的 `status`/`resolve_time`/`ack_user` 一个都没被改写），并对**真 PG** 再跑两条变异（S-1/S-2 把守卫从 WHERE 拿掉）确认它会红。
   - **踩到的假绿（已登记 `docs/TRAPS.md` T-42）**：首次跑 `scripts/db_smoke.sh` 时新用例**根本没执行**——该脚本用 `-run` 白名单挑用例，新用例不在名单里，脚本照样打印「✅ 全部通过」。已把 `TestDBSmoke_AlertBulkTransitionGuards` 加进白名单，并重跑确认它出现在输出里。
   - **门禁**：`gofmt -l` 干净、`go vet ./...` 干净、`go vet -tags dbsmoke ./tests/` 干净、`go test ./...` 全包 ok；`npx tsc --noEmit` 0、`npm run lint` 0；**全量 vitest 本轮未跑**——改动是纯后端（4 个源文件 + 2 个测试文件全在 `backend/`），前端零改动，vitest 覆盖不到任何被改的代码。真 PG 冒烟 `scripts/db_smoke.sh` 通过（全新 + 升级两条迁移路径）。
   - **未做（登记，①已于 2026-09-11 由 M20 收口，见 1.10）**：① `alerts.status` 的**库默认值漂移**——`migrations/000001_init.up.sql:646` 写 `DEFAULT 'firing'`，`models/alert.go:30` 写 `default:problem`。当时写作「当前三个写入方都显式 `Status: "problem"`，故这个默认值是**死的**；但一旦有新的写入方不显式赋值（或走裸 SQL 插入），会落成一个前端不认识的 `firing`」——**这句后半段已被 M20 实测证伪**：走 GORM 的新写入方即使漏赋值也落 `problem`（tag 自己替代），只有绕开模型字段表的路（裸 SQL / `db.Exec` / 非 GORM 导入器）才会落 `firing`。M20 已在迁移注释里记下这次纠正。② gRPC `AckAlert`/`ResolveAlert` 把 `ErrNotFound` 映射成 `codes.Internal`（应为 `NotFound`），与本轮同源但**故意不顺手改**——gRPC 错误映射是「一刀切」的全文件问题，值得独立一步统一（同 `GetAlert` 已有正确写法可对齐）。③ openapi 未文档化 `/alerts/{id}/ack`、`/resolve` 的 409（两个端点当前只声明了 `'200'`），同 1.7 ① / 1.8 ③，动 `gen:api` 产物，独立小步再做。

1.10. **M20 `alerts.status` 库默认值漂移收口（2026-09-11 完成，出自 1.9 登记的 ①）**
   - **漂移**：`migrations/000001_init.up.sql:646` 声明 `status VARCHAR(20) DEFAULT 'firing'`，`internal/models/alert.go` 声明 `gorm:"...;default:problem"`。本仓**用迁移建库、不用 AutoMigrate**，所以库里那行是既成事实，不会因模型写了 `problem` 被改掉。`firing` 这个词在 000001 的 DDL 和三份 testdata 之外**全仓不出现**——不在前端 `getAlertActions` 认识的三个状态里（`problem`/`acknowledged`/`resolved`），也不进 `dashboard_service.go` 的计数分支。落库即**静默卡死**：告警在列表里看得见，按钮一个不给，统计里也不出现。
   - **机理（实测，不是查文档得来的）**：本轮做变异实验时发现——把 000024 换成 `SELECT 1;`（等于没改库默认值）后，**只有裸 SQL 那条断言红（`db_smoke_test.go:962`），GORM `db.Create` 那条照样绿**。结论：GORM 对带 `default:` tag 的零值字段，是 Create 时**用它自己解析的 tag 值替代**，既不省略该列（不吃库默认值）也不送空串。**我在 1.9 ① 和迁移注释初版里写的「GORM 零值省略该列、于是落库默认值」是错的**，迁移注释与用例注释均已按实测改写——理由写错比不写更坏，因为它会误导下一个改动的人。
   - **那这条缺陷当前是活的吗？不是，是潜伏的**，这一点刻意写明、不夸大：今天的写入方都不会落 `firing`——显式赋值的（`integration/service.go`、`integration/zabbix.go`、`cmd/seed/main.go`）不会，漏赋值的也被模型 tag 兜成 `problem`；grep 确认全仓非测试 Go 里**没有任何裸 `INSERT INTO alerts`/`UPDATE alerts`**，即没有绕开模型字段表的写入方。**所以本迁移的定位是拆陷阱 + 让 tag 与库两侧对齐，不是修一个正在发生的 bug。**
   - **为什么仍值得一个独立迁移**：库默认值是**绕开模型字段表那条路的最后一道**——裸 SQL、`db.Exec`、将来的非 GORM 导入器、以及有人直接 psql 手插一行，这些都是真实会发生的运维动作，而它们全都不经过 tag。以契约为准对齐到 `problem`（契约里「未处理」的初始态），同 M16 的思路：让声明/契约那侧当事实标准，而不是让一个没人维护的库默认值继续当。
   - **为什么不反过来把模型改成 `firing`**：`firing` 不在任何词表里，改模型等于把一个前端不认的状态**确认为正确**，是把 bug 固化成契约。
   - **为什么不加 `NOT NULL` 逼写入方显式给值**：加 `NOT NULL` 不会当场破坏现有写入方（它们都显式赋值），但将来一旦有写入方漏赋值，插入会**直接失败**——对告警系统而言「丢一条告警」（无声无息）比「落一条默认为 `problem` 的告警」（列表里看得见、能处理）严重得多。默认值在这里是安全网，不是缺陷。
   - **测试**：`TestDBSmoke_AlertStatusDefault` 三条断言，一条比一条靠近真实路径——① `information_schema` 里列默认值含 `problem` 且不含 `firing`（迁移真的改了它）；② **裸 SQL** 漏设 status 插入 → 落 `problem`（库默认值本身可达，这是本轮的靶心）；③ 走 GORM `db.Create` 漏设 `Status` → 落 `problem`（钉住 tag↔落库值↔契约初始态三者不脱节）。③ **不依赖 000024**（已实测），留着是因为未来 GORM 若改成送空串，红的会是它。自建自清，不扰动后面的索引 EXPLAIN 断言。
   - **回滚链**：`000024.down.sql` 把默认值退回 `firing`，`TestDBSmoke_DownPreservesLegacyColumns` 的 Down 链从 10 次扩到 **11 次**（24 → 23 → 21 → 20 → 19 → 18 → 17 → 16 → 15 → 14 → 13），第一次 Down 前**加正向断言**钉住「滚的确实是 000024」并断言默认值已退回 `firing`。顺带把该用例注释里漂移-prone 的 `// Down = 回滚 0000NN` 序号去掉——序号会在每次新增迁移时静默过期，而这正是该用例存在的理由。000023 是数据迁移（迁完分不清原始值）故刻意不可逆，000024 只改列默认值、无数据改写，故可逆，两者不对称是**有意的**。
   - **变异反证**：`/tmp/m20_mut.py` 3 条全红在断言，跑完 sha256 校验还原字节一致 —— V-1（up 换成 `SELECT 1`）→ `TestDBSmoke_AlertStatusDefault` FAIL；V-2（up 改回 `'firing'`）→ 同左 FAIL；V-3（down 变 no-op）→ `TestDBSmoke_DownPreservesLegacyColumns` FAIL。
   - **门禁**：`gofmt -l` 干净、`go vet ./...` 干净、`go vet -tags dbsmoke ./tests/` 干净、`go test ./...` 全包 ok；真 PG 冒烟 `scripts/db_smoke.sh` 通过（全新 + 升级两条迁移路径，新用例已在 `-run` 白名单内，确认出现在输出里——T-42 的教训）；`npx tsc --noEmit` 0、`npm run lint` 0；**全量 vitest 本轮未跑**——`git status` 确认前端 0 个改动文件，vitest 覆盖不到任何被改的代码（本轮只碰 2 个 `.sql` + 1 个测试文件 + 1 个脚本）。
   - **未做（登记）**：无新增。1.9 剩的 ②（gRPC 错误映射）与 ③（openapi 补 409）原样待做。

1.11. **M21 gRPC 错误映射收口（2026-09-11 完成，出自 1.9 登记的 ②）**
   - **前提：gRPC 不是死代码。** `cmd/server/main.go:75` **无条件** `go startGRPCServer(grpcPort, ...)`（默认 50051，`cmd/server/grpc.go:25` 注册 `AlertService`），所以下面这些缺陷是**线上可达**的，不是纸面问题。
   - **缺陷一（判据永不命中）**：`GetAlert` 判的是 `errors.Is(err, gorm.ErrRecordNotFound)`，而 `service.Get` 返回的是 `service.ErrNotFound` —— service 层在 `alert_service.go:225` 就把 gorm 的错误**翻译掉了**，上层根本见不到 gorm 类型。于是「告警不存在」被报成 `codes.Internal`，客户端当成服务端故障去重试。**注意 1.9 ② 里我写的「`GetAlert` 已有正确写法可对齐」是错的**：它和 `AckAlert`/`ResolveAlert` 一样坏，只是坏得更隐蔽（那两支至少没有假判据）。已按实测纠正。
   - **为什么这个错很容易被写出来（值得记成 trap）**：`gorm.ErrRecordNotFound` 在 **service 层是正确的**判据 —— 那里正是 gorm 调用的最近处，全仓 20+ 处在用且都对。错的是把它**带到了上层**；分层之后上层已经拿不到 gorm 的错误类型了。看起来「和别处一样」，语义完全不同。已登记 `docs/TRAPS.md` **T-44**（含检测手段：`grep gorm.ErrRecordNotFound` 凡落在 `internal/service/` 之外的一律复核）。
   - **缺陷二（外泄原始错误）**：5 处 `status.Errorf(codes.Internal, "%v", err)` 把原始错误文本塞进**响应体**。这与 `apierr.Respond` 的出口契约正相反 —— 后者只在 `status >= 500` 时记日志、且经 `redact.Text` 脱敏，**响应里永远只有通用文案**（G-28 轮立的规矩）。内部错误可能带 DSN / `*url.Error` 的完整 URL / userinfo。
   - **修法**：新增 `serviceErrToStatus(method string, err error)`，一处集中翻译。哨兵：`ErrNotFound`→`NotFound`、`ErrInvalidState`→`FailedPrecondition`、`ErrInvalidInput`→`InvalidArgument`、`ErrTooManyItems`→`InvalidArgument`、`ErrAlreadyExists`→`AlreadyExists`；其余 → 记 `logger.Errorf(..., redact.Text(err.Error()))` + 返回通用串 `"internal error"`。原来的 `gorm.io/gorm` import 随之删除（它是那个假判据唯一的用途）。
   - **为什么覆盖**全部**哨兵、而不是只覆盖今天可达的那两支**：翻译表存在的意义就是防「将来新增的那支没人管」，而漏掉的那支**必定**静默降级成 Internal —— 那正是本轮修的缺陷形态。多写四行 vs 再犯一次同样的病，取舍很清楚。（`ErrTooManyItems` 今天在 `alert_server.go` 里确实不可达：批量方法没暴露成 RPC；但它在 `service` 里是活的，`BulkAcknowledge`/`BulkResolve`/`BulkDelete` 都会返。）
   - **哪些文案可以外露（关键取舍，别一刀切脱敏）**：哨兵那几支的文案是我们自己写的、可安全外露 —— M19 刻意让状态冲突带上**真实原因**（`告警当前状态不允许该操作`）好让客户端区分「别再试了」和「改参数重试」；把它们一并抹平成通用文案，等于把 M19 的成果推翻。所以：**哨兵 → 带原因；非哨兵 → 通用串 + 日志**。`ErrNotFound` 用具体文案 `"alert not found"` 而非哨兵的通用英文文本（`resource not found`），因为 gRPC 侧目前只服务告警。
   - **测试（7 个新用例）**：`TestServiceErrToStatus_哨兵全覆盖`（5 支 + 2 个 `%w` 包装形态）、`TestServiceErrToStatus_内部错误不外泄原文`（带 `password=sup3rs3cr3t` 与内网 IP 的错误 → 断言响应里三者皆无）、`TestServiceErrToStatus_状态冲突保留真实原因`（反面：不许被一并抹平）、`TestGetAlert_NotFound_返NotFound`（**本轮核心回归**，原实现必红）、`TestAckAlert_InvalidState_返FailedPrecondition`、`TestResolveAlert_InvalidState_返FailedPrecondition`、`TestAckAlert_复读失败走映射`（ack 成功但复读时记录已被删 → NotFound）、`TestListAlerts_服务端错误走映射`。
   - **变异反证**：`/tmp/m21_mut.py` 5 条全红在业务断言且红色集合精确（T-38 纪律：字节备份 + 每轮变异前全量还原 + sha256 校验）—— V-1 删 `ErrNotFound` 支（回到假判据形态）红 5 个用例；V-2 `default` 支改回 `%v` 外泄 红 2 个；V-3 状态冲突抹平文案 红 2 个；V-4 状态冲突降级成 Internal（回到 M19 之前）红 6 个；V-5 删 `ErrTooManyItems` 支 红 2 个（证明「覆盖全部哨兵」这句是被断言守住的，不是写在注释里的口号）。
   - **门禁**：`gofmt -l` 干净、`go vet ./...` 干净、`go test ./...` 全包 ok；`npx tsc --noEmit` 0、`npm run lint` 0；**全量 vitest 本轮未跑**——`git status` 确认改动只有 2 个 `backend/internal/grpcserver/` 文件，前端 0 改动。
   - **未做（登记）**：① `apierr.TranslateDBError`（`internal/apierr/apierr.go:97`）**零生产调用方**——只被自己的 6 个测试养着（`apierr_test.go:128-190`），且它只认 `gorm.ErrRecordNotFound`，service 层返的是 `service.ErrNotFound` → 谁真把它接上，就会在 HTTP 侧原样复现本轮这个 bug。要么删（连测试一起），要么改成认哨兵；属独立决策，待拍板。② `internal/api/handlers/runbook_handler.go:184` 有一行 `var _ = gorm.ErrRecordNotFound`（撑着一个已无用途的 import）——死代码，删掉即可，独立小步。③ gRPC 侧只映射了 2 个 RPC 用到的错误，`BulkXxx`/`Stats`/规则 CRUD 等**尚未暴露成 RPC**，将来暴露时直接用 `serviceErrToStatus` 即可。

1.12. **M22 告警路径 `:id` 校验 + ack/resolve 契约补全（2026-09-11 完成，出自 1.9 登记的 ③ 与 M22 侦察）**
   - **缺陷（本轮新发现，M22 侦察时挖出）**：`alert_handler.go` 把 `c.Param("id")` **裸字符串**直接送进 gorm 的 `WHERE id = ?`，而 `alerts.id` / `alert_rules.id` 都是 `UUID PRIMARY KEY DEFAULT gen_random_uuid()`（`000001_init.up.sql:637` / `:617`）。Postgres 对非 UUID 字面量报 **22P02 invalid input syntax for type uuid**；该错误既不是 `gorm.ErrRecordNotFound` 也不是任何 service 哨兵，于是经 `apierr.Internal` 变成 **500** —— 把「调用方把 id 写错了」报成「服务端故障」，调用方只会照着原样重试（与 M19/21 修的是同一种指错方向）。
   - **实测而非推断**：起 `postgres:18-alpine` 跑 `SELECT count(*) FROM a WHERE id = 'not-a-uuid';` → `ERROR: invalid input syntax for type uuid: "not-a-uuid"`。没有靠「Postgres 大概会隐式转换」这类想当然下结论。
   - **既有正确先例**：`oncall_handler.go:53` 就是 `uuid.Parse(c.Param("id"))` → `apierr.BadRequest(c, "ID 格式错误")`。修法是**对齐既有模式**，不是发明新写法。
   - **覆盖面**：新增 `alertPathID(c) (string, bool)` 并应用到**全部六个**路径参数端点（`GetAlert` / `AcknowledgeAlert` / `ResolveAlert` / `UpdateAlertRule` / `DeleteAlertRule` / `MarkFalsePositive`）。只修已登记的那一个会把同一文件留成半修复态，下一个人还得重走一遍侦察。只校验不转换（service 签名收 `string`，不做无谓类型改写）。
   - **同轮发现并修掉的第二个缺陷（测试给了假信心）**：测试路由把 `ack`/`resolve` 注册成 **POST**，而生产是 **PUT**（`routes.go:323-324`）—— 三个用例一直全绿，测的却是一个**线上不存在的路由形状**。已把测试路由与三个请求方法一并对齐，并在 `newAlertTestRouter` 上写明「方法必须与生产一致，改一处要同步改另一处」。
   - **契约补全（1.9 ③）**：`openapi.yaml` 给 `/alerts/{id}`、`/alerts/{id}/ack`、`/alerts/{id}/resolve`、`/alerts/{id}/mark-fp` 补 `400`；`ack`/`resolve` 另补 `404` 与 `409`（M19 引入的状态冲突一直没进契约）。**只声明实现真会返的码**：`DeleteAlertRule` 那条刻意不写 `404` —— `service.DeleteRule` 直接 `Delete` 且不判 `RowsAffected`，删不存在的行返 nil，写了就是假承诺（登记见下）。`npm run gen:api` 已重跑，`api.types.ts` 差异随本轮提交（CI 有漂移闸门）。
   - **测试**：新增表驱动 `TestAlertHandler_非法id一律400且不触达service`，覆盖六个端点，每条同时断言**状态码是 400** 与**service 完全没被调用**（守卫若放在查库之后，等于白修 —— 500 照样出得来）。
   - **变异反证**：`/tmp/m22_mut.py` 5/5 红在业务断言、红色集合精确（T-38 纪律：字节备份 + **每轮变异前全量还原** + sha256 校验）：V-1 去掉 `GetAlert` 守卫 → 红 2 个（含子用例）；V-2 `alertPathID` 吞掉校验结果 → 六条子用例全红；V-3 守卫落 409 → 六条子用例全红（证明钉的是具体状态码，不只是「非 500」）；V-4 去掉 `DeleteAlertRule` 守卫 → 只红该端点（证明覆盖的是全部六个，不是只有 `GetAlert`）；V-5 测试路由把 `ack` 退回 POST → 红 `Acknowledge` 两个既有用例 + 非法 id 的 ack 子用例（证明「方法对齐」是被断言守住的）。
   - **门禁**：`gofmt -l` 干净、`go vet` 干净、`go test ./...` 全包 ok；`npx tsc --noEmit` 0、`npm run lint` 0、`vitest run src/pages/Alerts` 27 用例绿；openapi YAML 解析通过。
   - **未做（登记）**：① `BulkAcknowledge`/`BulkResolve`/`BulkDelete` 的 id 走**请求体**而非路径，**同一个 22P02→500 缺口存在**（body 里的非法 id 一样会撞 UUID 列）——本轮只收口路径参数，批量口径与「部分 id 非法时整批拒绝还是逐条跳过」是独立语义决策，不顺手改一半。② `/alerts/rules` 与 `/alerts/rules/{id}` **在 openapi 里根本没有条目**（不是本轮遗漏，是既存空白），补齐是独立小步。③ `service.DeleteRule` 不判 `RowsAffected`，「删不存在的规则」静默返 200，与 `UpdateRule` 的 404 不对称 —— 登记待拍板。④ 1.9 剩的 `apierr.TranslateDBError` 死代码与 `runbook_handler.go:184` 仍在队列。

2. ~~W2 剩余 `Settings:923`~~ → 已完成（rev8）。**W2 全部 7 个调用点收口**。
3. ~~W4-H6 cssVar 实测~~ → 已完成（rev9，方案①）。~~W4-H8~~ → 已完成（rev10）。~~W4-H9~~ → 已完成（rev11）。~~W4-H10~~ → 已完成（rev12）。~~W4-M4 AlertSuppressions~~ → 已完成（rev13）。~~W4-M4 Oncall~~ → 已完成（rev14）。~~W4-M4 Runbook~~ → 已完成（rev15）。~~W4-M4 Settings~~ → 已完成（rev16）。~~W4-M5 AlertSuppressions~~ → 已完成（rev17）。~~W4-M5 Runbook~~ → 已完成（rev18）。~~W4-M5 Oncall~~ → 已完成（rev19）。~~W4-M6 Settings~~ → 已完成（rev20）。~~W4-M6 Oncall~~ → 已完成（rev21）。~~W4-M6 TicketFormModal/AssetFormModal~~ → 豁免（rev22，死代码）。**W4 批 1 全部收口（H1/H6/H8/H9/H10/M4/M5/M6）**。
4. ~~W6 批 1 逐索引推进~~ → **W6 批 1 全部收口（P13–P19：迁移 000016–000021 六个索引 + ticket_service cursor Count）**。
5. ~~批 2 启动~~ → **M14（rev30）+ M7（rev31）+ M11 三处（rev32/33/34）+ M1 五页（rev35–39）+ M2 七表（rev40–46）+ M3/P5 资产页服务端分页（rev47）+ M3/P5 工单页服务端分页（rev48）+ P4 表格 memo（rev49 AssetTable + rev50 AlertTable）+ P7 key 稳定（rev51）+ M13 移动端双渲染（rev52）+ P6 进度节流豁免（rev54）+ M3/P5 告警页服务端分页（rev55）已完成（M3/P5 三页全收口）**；剩 M15 空态/加载态（登记待决策，见 §8 已知阻塞）。

**已知阻塞/待确认**：~~M16（工单优先级域 normal vs medium）~~ 已于 2026-09-10 收口（以契约为准，迁移 `000023` + 三个写入方归一，见 `docs/FIX-PLAN-M16-PRIORITY.md`）。M3/P5 keyword 口径（资产页）：后端 `keyword` 匹配 `name/asset_tag/sn`（assets 表无 IP 列，IP 在 asset_networks 一对多），与 M9 placeholder「搜索名称 / IP」的「IP」承诺有口径差——改前前端 `ip_address` 在真实列表本就 undefined（后端未返回该字段），故「搜 IP」实为死逻辑，下沉后仅由「死逻辑」变「明确不支持」，未造成可感知回退；是否补后端 join 搜 IP 属独立决策，登记待确认。**M3/P5 告警页分页**：方案 B 已拍板（详见 `docs/FIX-PLAN-ALERT-PAGINATION.md`），三小步全收口（后端 offset 分页 + openapi 契约 + 前端受控分页，rev55），M3/P5 三页全完成。**M15 空态/加载态统一**：空态已收口（EmptyState 12 处、无裸 Empty）；「加载态统一」指把 Topology/Racks/AssetTimeline 的裸 Skeleton/Spin 换成已有 LoadingSkeleton（现只 2 处使用），属「重构非修 bug」+ 收益低（代码一致性），是否做待拍板。

**测试盲区（W1 系列共有）**：本批单测 mock 掉 `useApiQuery`，故 `queryFn` 内的形状归一（`Array.isArray(items) ? items : []`）与 `catch` 删除不被单测覆盖。防回归靠两点：① `MOCK_*` 常量已从源码删除，重新引入无法通过编译；② `isError` 分支有断言。接口真实形状逐页核对 handler——`oncall_handler.go:35/124/135` 均返回 `{code,data:[...]}` 裸数组，`apiGet` 解包后即数组。
