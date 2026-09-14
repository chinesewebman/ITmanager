# M50 Completion Report — G-UI-Tickets 工单工作流产品化

## Delivered

工单页此前**只读**：详情弹窗 footer 只有一个 `[关闭]`，列表行只有一个 `[详情]`，
改派 / 改优先级 / 关单**没有任何入口**。本轮补齐（frontend-only，后端一字未改）：

- **`TicketDetailModal.tsx`** — footer 5 个操作，全部写在这一个弹窗里：
  `[+ 评论]`（TextArea + 提交，空文本 disabled）/ `[改派]`（人员下拉，懒加载）/
  `[改优先级]`（四档下拉）/ `[关单]` / `[已解决]`（后两者 `Popconfirm` 二次确认）。
  `[关闭]` 原样保留（收起弹窗，不是关单）。五种操作**共用一条** mutation：
  `onSuccess` → `message.success` + `invalidateQueries({queryKey: queryKeys.tickets.all})` + `onClose()`。
  错误不在这里处理（axios 拦截器已统一 toast，再加一层＝同句话弹两遍），弹窗内无 error alert。
- **`TicketTable.tsx`** — `更新时间` 列（`formatRelativeTime`，带 sorter）+ 行尾 `[更多操作]`
  `Dropdown`（click 触发）：查看详情 / 改派 / 改优先级 / 关单，四项都把**整行 record** 交给父回调。
- **`Tickets.tsx`** — 三个行回调 → `openTicket(record, panel)` 打开弹窗并预设展开哪块面板。
- **`api.ts`** — `userApi.list` 加可选 `page/page_size` + 收窄返回类型（派单候选池必须显式放大分页，
  后端默认 20 条会静默丢掉运维）。**`ticketApi` 一字未改。**

### 与 intent 假设不符、按实测改掉的三处

intent 的「Backend already has (no change)」三条里有两条与仓库实际不符，grep 后端后按下述实现：

| intent 假设 | 实测 | 本轮做法 |
|---|---|---|
| `POST /api/tickets/:id/comments` 存在 | **不存在**：全仓无 comment handler，openapi 也无该 path | 「加评论」= 追加成 `description` 新段落再 `PUT`（工单上唯一的自由文本列） |
| 改派写 `assignee`（PATCH `assignee`） | 库里没有 `assignee` 列，`PUT` 认**库列名** → 写它会 500（新 trap **T-62**） | 写 `assignee_name` |
| 候选按 `role=operator` 筛 | `operator` 是设计期别名，出站已折叠成 `ops_user` → 按它筛**恒 0 行**（新 trap **T-63**） | 判据用 `ops_user`/`ops_admin`（＝`capRoles[CapWrite]` 的运维角色） |

另外：intent 说 `TicketTable.test.tsx` **已存在**（「现有 + 新加列」）——实际不存在，本轮新建。

### 与 brief 的两处设计偏离（有意，附理由）

1. **mutation 不放两份**。brief §1 说「弹窗内用 useMutation + onSuccess invalidate + 关 modal」，
   §3 又说 `Tickets.tsx` 要有「3 个 mutation handler」。后端只有一个写入口
   （`PUT /tickets/:id`），两处各写一份 mutation + invalidate 就是双写路径；按 §1 与 §2
   的「都打开 Modal，modal 内有同样的 mutation handlers」实现 —— **mutation 收在弹窗里**，
   `Tickets.tsx` 只做 wiring（3 个行回调 + `initialAction`）。brief §4 的断言
   （「点击评论 → expect api.put 被调用」「成功 → expect queryClient.invalidateQueries 被调」）
   也只有 mutation 在弹窗里才成立。
2. **改派/改优先级是「选 + 确认」两步**，不是选中即写。写操作前给一次确认，且失败时不会
   留下「下拉显示已改派、库里没改」的假状态。

### 测试

- `TicketDetailModal.test.tsx`（**新**，6 例）：5 个按钮都在（含 `[关闭]` 不动）/ 空评论不可提交 +
  提交后载荷是 `磁盘 90%\n\n已重启服务` / 关单 Popconfirm **确认前零请求**、确认后
  `{status:'closed'}` + invalidate + 收弹窗 / 失败：拦截器 toast **恰好一次**、弹窗不关、
  无 `.ant-alert-error`、不 invalidate / 改派候选只含 `ops_user`/`ops_admin` → `{assignee_name}` /
  改优先级四档 → `{priority}`。
  **不打桩 `services/api`**：只换 `api.defaults.adapter`，拦截器真跑，「不重复 toast」才有内容。
- `TicketTable.test.tsx`（**新**，2 例）：`更新时间` 在表头且渲染相对时间；`[更多操作]` 四项俱全
  且回调带整行 record（连原有的 `[详情]` 一并回归）。
- `Tickets.test.tsx`（既有 10 例**全保留**）：仅补基建 —— 弹窗现在需要 `QueryClient`（真环境由
  `main.tsx` 提供）→ `renderTickets()` 套 provider；mock 补 `queryKeys.tickets.all` 与 `users` 分支。

## Changed

| file | lines before → after | 说明 |
|---|---|---|
| `frontend/src/components/TicketDetailModal.tsx` | 96 → 329 | 5 个操作 + 3 块内联面板 + 共享 mutation + 懒加载候选 |
| `frontend/src/components/TicketDetailModal.test.tsx` | 新 → 180 | 6 例；真 axios + 换 adapter，拦截器真跑 |
| `frontend/src/components/TicketTable.tsx` | 143 → 188 | `更新时间` 列 + `[更多操作]` Dropdown + 3 个行回调 prop + `Ticket.description` |
| `frontend/src/components/TicketTable.test.tsx` | 新 → 66 | 2 例（列存在 + 下拉四项/回调带 record） |
| `frontend/src/pages/Tickets.tsx` | 195 → 214 | `openTicket(record, panel)` + 三回调接线 + `initialAction` |
| `frontend/src/pages/Tickets.test.tsx` | 207 → 236 | `renderTickets()` 套 QueryClientProvider + mock 补 users/all |
| `frontend/src/services/api.ts` | 327 → 332 | `userApi.list(params)` + 返回类型；`ticketApi` 未动 |
| `CHANGELOG.md` / `TODO.md` | — | `### M50` / G-UI-Tickets 结案 |
| `docs/TRAPS.md` | — | 新 trap **T-62**（`PUT` 认库列名）、**T-63**（`operator` 别名）+ §五 索引 |

## Validation

- `npx tsc --noEmit` → **0 error**（全量，非增量）。
- `npx vitest run src/components/TicketDetailModal.test.tsx src/components/TicketTable.test.tsx`
  → **8 PASS**（6 + 2），0 FAIL。
- `npx vitest run`（全 frontend）→ **42 files / 360 tests PASS，0 FAIL**
  （基线 40 files / 352 tests 全绿；本轮 +8）。
- `cd ../backend && go test -count=1 -timeout=600s ./...` → **27 packages ok**，0 FAIL
  （只有 `mattn/go-sqlite3` 的 cgo `-Wdiscarded-qualifiers` 编译告警，与本轮无关）。
- **Mutation inversion 实证 3 次**（每次都 revert 回绿，逐条记录）：
  1. `[关单]` 的 `onConfirm` 改成 `return`（空操作）→ `vitest run src/components/TicketDetailModal.test.tsx`
     = **2 failed | 4 passed**（关单 + 失败路径两例红）→ revert 后 6/6 PASS。
  2. `ASSIGNEE_ROLES` 换成 `['admin','auditor']` → **1 failed | 5 passed**（改派候选例红）→ revert 后 6/6 PASS。
  3. 注释掉 `queryClient.invalidateQueries(...)` 一行 → **1 failed | 5 passed**（关单例的 invalidate 断言红）
     → revert 后 6/6 PASS。

## Risk

残余（按严重度；**未登记进 TRAPS.md** —— 它们是本轮的取舍代价，不是可复用的坑，
可复用的那两条已登记为 T-62 / T-63）：

- **R-1「评论」实为追加 `description`**：两个人同时提交评论 = 后写的
  覆盖前写的那一份（前端读的是列表快照里的 `description`，没有乐观锁）。真评论表需要后端新端点
  （intent 明确禁止改后端），故本轮以「一次追加」为上限。同因由：`description` 会随评论单调变长，
  历史 diff 的 `new_value` 有截断标记，长描述在时间线里看不全。
- **R-2 `assignee_id` 仍是空的**：只写了 `assignee_name`（该 ID 列前后端零消费方，
  写它只会多出一行裸 UUID 的经手历史）。将来若有「按 assignee_id 推送/过滤」的需求，需要先回填。
- **R-3 非 admin 点 `[改派]` 会收到 403 toast**：`GET /users` 挂在 `canIdentity`
  （仅 admin）。本轮不复制能力矩阵、不加前端角色门禁（intent 明确要求），只做到**懒加载** ——
  不展开面板就不发请求，同页其它操作不受影响。
- **R-4 行内 `[关单]` 用 `onClose` 命名**：语义是「关闭工单」，与弹窗的 `onClose`
  （收起弹窗）同名不同义，仅靠注释区分。命名包袱来自 intent 的显式要求。

## Status

- 分支 `main`，全部 `git push origin main` 成功。
- AC 逐条：

| AC | 状态 |
|---|---|
| Modal 5 个操作按钮（评论/改派/改优先级/关单/已解决）+ `[关闭]` 不动 | ✓ |
| 评论内嵌 TextArea，空提交被挡，提交后刷经手历史（`tickets.all` 前缀） | ✓ |
| 改派用 `userApi.list` 筛运维角色（不硬编码名单） | ✓ |
| 改优先级四档 → 写 `priority` | ✓ |
| 关单 / 已解决 `Popconfirm` 二次确认 → 写 `status` | ✓ |
| mutation 用 react-query 封装（`useApiMutation`）+ `onSuccess` invalidate + 收弹窗 | ✓ |
| error 不写进 modal state（拦截器 toast，不加第二层） | ✓ |
| `TicketTable` 加 `更新时间` 列（`formatRelativeTime`，`创建时间` 之后，sorter） | ✓ |
| 行内 `[更多操作]` Dropdown 四项（查看详情/改派/改优先级/关单），无新依赖 | ✓ |
| `Tickets.tsx` 三个回调（`onAssign`/`onChangePriority`/`onClose`）都打开 Modal | ✓ |
| 后端零改动（27 packages ok） | ✓ |
| 不 spawn subagent / 不加 npm 依赖 | ✓ |
