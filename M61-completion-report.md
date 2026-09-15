# M61-completion-report — G-User-AdminManagement 用户管理（admin 端启用/禁用/改角色）

**Shipped**: 2026-09-15（commits `4d7092c` feat service / `1c85490` test service / `5012582` feat handlers+契约 / `4a7f407` test 路由集成 / `969db3e` feat frontend / `5249fd9` test frontend / **`dac8b05` fix 全站白屏**；docs 本次）
**Scope**: backend 5 files + frontend 8 files（含 `openapi.yaml`、生成物 `api.types.ts`、白屏回归用例）
**摩擦**: **G-4**（multi-angle 审查，AUTHZ-CLOSURE §2 D-A 登记）—— `/users` 只有 `GET`、`UserService` 只有 `List`/`Get`、`userApi` 只有 `list`/`get`、全站无用户管理页。admin 想让离职员工登不进来、想把某人提成 `ops_admin`，只能自己进库 `UPDATE`：无校验、无审计、无人知道谁改过。M40 把「禁用即失效」做进了鉴权侧，但**没有人能触发禁用** —— 功能缺的是那只手。
**Time**: ≤4h omp round

## 改动

| 文件 | 改动 |
|---|---|
| `backend/internal/service/user_service.go` | `UpdateUserInput`（三指针字段）+ `Update`/`UpdateStatus`/`UpdateRole`；`applyUserUpdate` 事务内核（`FOR UPDATE` 持锁读旧值 → 守卫 → UPDATE → 回读）；`checkUserUpdateGuards` 两道守卫（自我禁用/降级、最后一名**可登录**管理员）；`userStatusValues` 词表（只收 active/inactive） |
| `backend/internal/service/asset_service.go` | 错误块新增 `ErrForbidden`（403 = 策略拒绝，与 400「改参数重试有用」分开） |
| `backend/internal/api/handlers/user_handler.go` | `UpdateUser`/`UpdateUserStatus`/`UpdateUserRole` + `decodeUserBody`（严格解码）+ `userPathID`（UUID 校验）+ `writeUser`（错误映射收口） |
| `backend/internal/api/routes.go` | 三条写端点挂 `canIdentity` + `RejectAPIKeyAuth` |
| `backend/internal/middleware/audit.go` | 默认 `ActionFunc`：context 键 `audit_action` 优先、回落 HTTP method；`Action` 补 `sanitizeField(50)` |
| `backend/internal/api/openapi.yaml` | 3 条新 path + `UserUpdateRequest`；`User` 补 `status`/`last_login`；**修正** `UserList.data`（原声明裸数组，实际是信封） |
| `frontend/src/services/api.ts` | `userApi.update`/`updateStatus`/`updateRole` + `UserStatus`/`ASSIGNABLE_ROLES` |
| `frontend/src/pages/Users.tsx`（新增） | 用户管理页：Table + 角色 Select + 状态 Switch + Popconfirm、乐观更新 + 按字段回滚、强制改密按钮 |
| `frontend/src/App.tsx` | 菜单抽成 `buildMenuItems(hasIdentity)` 导出纯函数（`/users` 按 `capabilities` 含 `identity` 条件渲染）+ `/users` 路由 + `AppLayout` 导出 |
| `frontend/src/components/AppBreadcrumb.tsx` / `frontend/src/hooks/useApiQuery.ts` | 面包屑认 `/users`；`queryKeys.users` |
| `backend/internal/service/user_service_test.go` | +13 用例（真 sqlite 为主，2 条 sqlmock 钉 SQL 文本） |
| `backend/internal/api/routes_integration_test.go` | `gatedRoutes` +3 登记；+11 用例 / 28 sub-case |
| `frontend/src/pages/Users.test.tsx`（新增） | 14 用例 |
| `frontend/src/App.menu.test.tsx`（新增） | 5 用例（侧边栏能力门禁） |
| `frontend/src/App.render.test.tsx`（新增） | 2 用例（**全站白屏回归**，见「附带修复」） |

## Hard pass

| 维度 | 结果 |
|---|---|
| backend `go test -count=1 ./...` | **27 packages ok（0 fail）** ✓（M60 同基线） |
| backend `gofmt -l internal cmd` | ✓ 仅 3 个**本轮未触碰**的既存文件（`database/gorm_logger_redact.go`、`middleware/auth_status_cache.go`、`notification/sender.go`）；M61 改动的 5 个 Go 文件干净 |
| backend `go vet ./...` | 干净 ✓ |
| frontend `npx tsc --noEmit` | **0 error** ✓ |
| frontend `npm run lint`（全量，`--max-warnings 0`） | 干净 ✓ |
| frontend `npx vitest run src/pages/Users.test.tsx` | **14 tests PASS** ✓（M61 新增） |
| frontend `npx vitest run src/App.menu.test.tsx` | **5 tests PASS** ✓（M61 新增） |
| frontend 全量 `npx vitest run` | **47 files / 489 tests PASS** ✓（M60 基线 42/409；+5 文件 +80 测试，含 M62 并行落地的 2 文件，零退化） |
| frontend `src/App.render.test.tsx` | **2 tests PASS**（修复前 2 failed —— 复现同一条 invariant 异常）✓ |
| `npm run validate:api` + `gen:api` 漂移 | valid ✓；生成物差异仅新增 path/schema 与 `UserList.data` 形状修正（CI 门禁 `git diff --exit-code` 覆盖） |
| mutation inversion（5 处，全红在**断言**上，非编译） | ① 自我守卫短路 → service 3 条 + 集成 1 条 FAIL；② 守卫 count 去掉 `status='active'` → `_被禁用的管理员不算能自救` FAIL；③ 去掉 `CanonicalRole` 折叠 → 2 条 FAIL；④ 前端 bypass `statusMut.mutate` → `禁用账号…PATCH /users/:id/status` FAIL；⑤ 全部还原后复跑全绿 ✓ |
| 双轨分析 | graphify 6968 nodes / 14363 edges / 443 communities，**0 anomalies**；codegraph 新符号入图（`Update → applyUserUpdate → checkUserUpdateGuards` 调用链）✓ |

## 附带修复：全站白屏（P0，非本轮引入）

验证 M61 页面时用真浏览器打开 dist 产物 → **纯白页**。查下去是本轮之前就存在的缺陷。

**症状**：任意路由（含 `/login`）root 为空，仅一条未捕获异常
`Error: useNavigate() may be used only in the context of a <Router> component`
（`react-router` 的 `useNavigateUnstable` → `invariant`）。

**根因**：`frontend/src/App.tsx` 把 `<CommandPalette />` 挂在 `<BrowserRouter>` **外面**，
而 `CommandPalette`（`components/CommandPalette/index.tsx:132`）无条件调 `useNavigate()`
（选中搜索结果要 `navigate(to)` 跳详情）→ 渲染期抛错 → React 卸载整棵树。

**引入点与暴露面（实测，不是推断）**：
- `git log -S'<CommandPalette />' -- frontend/src/App.tsx` → `f7e98eb`
  （2026-06-17「小改进 #3 Cmd+K 全局搜索」），`git show f7e98eb:frontend/src/App.tsx:330-332`
  显示当时就是 `<CommandPalette />` 紧邻 `<BrowserRouter>` 之外。此后十几轮未变。
- **反证**：把 `/tmp` 下从 M60 状态（`34d40ec`）构建的同一份产物加载进同一个浏览器 →
  **同一条异常、同样空白**。故与 M61 改动无关。

**为什么此前无人发现**：**没有任何用例渲染过 `<App />` 整体**。
`App.theme.test.tsx` 只测 `buildTheme` 的产物；`App.menu.test.tsx` 与各页用例都渲染
`AppLayout`/页面本身并**自己包 Router**。全仓测试面里「应用能不能挂载」这一层是空的 ——
于是每轮「frontend 全量 vitest PASS」都是真话，而产品在浏览器里是白的。

**修法**（`dac8b05`）：`<CommandPalette />` 移进 `<BrowserRouter>`（一行位置变更 + 原位注释
写明约束，防止后人再挪出去）。它本就依赖 Router 上下文，移进去是纠正而不是迁就。

**回归钉子**：`frontend/src/App.render.test.tsx` —— 按**生产入口形状**（`QueryClientProvider`
+ `App`，同 `main.tsx` 的两层）渲染，断言未登录出登录页、已登录出侧边栏。
修复前这两条在 jsdom 里复现同一条 invariant 异常（2 failed）；修复后 2 passed。
（写用例时先跑出红、再改代码 ——「先复现后修」这条在本轮是字节级成立的。）

**影响面（如实说）**：这是**产品级**缺陷，影响所有页面、所有用户，且已存在约三个月。
M61 之前各轮报告里凡有「UI 已变更 / 界面已接上」类声明，其**浏览器级**验证依据都不存在；
那些轮次的结论需要按「单测已验证、真机未验证」重新理解。本仓的测试面缺口是
「入口形状未覆盖」，不是「用例写得不好」——修法是补一条覆盖入口形状的用例，而不是加断言密度。

## 真浏览器验证（M61 交付物的验证依据）

环境：本机**无 docker 权限、无 postgres**（`pg_isready` 无响应、`docker ps` permission denied），
故起不了真后端。改用 `vite build` 产物 + 手写契约桩（按 openapi 三条新 path 的请求/响应形状），
用真实 Chromium（Puppeteer，带 CDP）加载 —— 验证的是**真浏览器里的 React 行为**，
桩只替代服务端。桩与静态服务都在 `/tmp`，**不入仓库**。

| 场景 | 实测结果 |
|---|---|
| `/users` 首屏 | 侧边栏 12 项、`用户管理` 高亮；表格 3 行（admin/zhangsan/lisi），列 = 用户名（含昵称）/邮箱/角色/状态/最后登录/操作 |
| 角色列 | 下拉 5 词表（超级管理员/运维管理员/运维人员/审计员/只读用户，带词表值后缀）；`admin`→`ops_admin` 变更后行内显示更新 |
| 状态列 | Switch 受控：u2 启用 → 点开确认框（文案含被处置的用户名 + 「登录会话与 API Key 立即失效（最长 30s 生效）」） |
| 禁用普通账号 | 确认 → `PATCH /api/users/u2/status body={"status":"inactive"}` → Switch 变「禁用」+ toast「状态已更新」+ 列表重取（桩日志逐条可核对） |
| 改角色 | 选中即弹确认 → `PATCH /api/users/u2/role body={"role":"ops_admin"}` → toast「角色已更新」+ 列表重取 |
| **自我禁用（后端 403）** | Switch 翻动后**回滚**为「启用」，toast 带服务端原因 `forbidden: 不能禁用自己的账号`（不止是拦截器的通用「没有权限访问」） |
| 能力门禁 | `/__caps/read,write,manage`（无 `identity`）→ 侧边栏**没有**「用户管理」；`/auth/me` 返回的 `capabilities` 是唯一判据 |
| 侧边栏截图 | 目视确认（4 张截图之一交给视觉模型复核）：12 项菜单、6 列表格、角色值无重复渲染 |

**未能验证的部分（如实登记）**：真后端的守卫语义（403 的两种情形、`FOR UPDATE` 行锁、
30s 缓存滞后）只由 Go 测试覆盖；桩是**按契约**实现的，不构成对后端行为的证明。

## 关键设计决策（含理由）

### 1. 状态词表**只收** active / inactive —— 不收注释里那个 `locked`

`models.User` 的注释写 `status: active, inactive, locked`，但鉴权侧只有两个消费者判状态：
`middleware/auth.go` 的 JWT 路径与 API Key 路径、`auth_handler.go` 的登录分支 —— **判的都是
`status == "inactive"`**。把 `locked` 收进可写词表，管理员会得到一个「点了以后对方照样能登录」
的开关：无报错、无警告、看起来可控（**T-72**，与 T-71 的「antd 数组上的 `pattern` 规则永不触发」、
G-39 的「`NotifyChannels` 只写不读」同族）。

契约层相反地**收全三值**：库列是裸 `VARCHAR(20)`、无 CHECK，存量行可能有 `locked`，出站可能返回它。
不让契约比实现更宽（否则消费方按 `locked` 写分支）也不敢更窄（否则 Swagger 上写着一个不可能出现的
值域）。

### 2. 守卫只拦两种，不做「至少一个 admin」的过度保护

- **v-1 自我禁用/自我降级**：identity 路由只有 admin 进得来，admin 点错一次就把自己锁在门外，
  自助恢复路径**不存在**（只能进库改）。
- **v-2 最后一名可登录的管理员**：判据只数 `status='active'` 的 admin —— 被禁用的 admin
  **登不进来**，不算「能自救的那个人」。这个条件不是优化而是正确性（**T-74**，mutation ② 专钉它）。

**刻意不做**「禁用任意 admin 都要先确认还有 backup」这类更严的策略：两个 admin 正常情况下可以
互禁，那是运维的真实需要（其中一个人离职）；过度保护会让「换掉那个 admin」变成不可能。

### 3. 守卫与写入**同一事务 + 持锁读旧值**

两道守卫都是**读-判-写**：不加锁时两个并发请求可各自读到「还有另一个 admin」而双双降级 →
系统一个 admin 不剩。故旧值用 `clause.Locking{Strength: "UPDATE"}` 读，判定与写入同事务。
代价如实登记：**sqlite 基座不渲染 FOR UPDATE**（driver 明说不支持行级锁），单测只能验 SQL 文本
（sqlmock + postgres dialector），真并发场景未在真 PG 上实测 —— 见「学到 / Retro」。

### 4. `ErrForbidden`（403）与 `ErrInvalidInput`（400）分开

判据是**调用方该做什么**，不是服务端内部怎么实现：词表越界 → 改参数重试有用（400）；
自我禁用 / 降级最后一名 admin → 改什么参数都没用（403）。混成一个会让前端把「策略拒绝」
渲染成「你的请求写错了」，运维照着提示改半天。

### 5. 严格请求体（`DisallowUnknownFields` + 尾随内容检查）

`ShouldBindJSON` **静默忽略未知键**：调用方 `PUT {"email": "new@example.com"}` 会拿到 200 而邮箱
一字未改 —— 比报错更糟的失败模式（调用方以为改成功了）。严格解码把它变成显式 400，
与工单 M17「系统标识列不可写」同一口径。400 文案静态：json 的解码错误里带调用方可控的字段名，
回显进 body 就是反射面（G-33 M1 的教训），用例专钉「body 不含字段名」。

### 6. 不做 DELETE、不做创建账号、按钮不叫「重置密码」

- **不硬删**：`audit_logs.user_id` 取值来自 `users`，硬删后历史里的操作人再也查不到是谁。
  正确动作是 `status=inactive`（保留账号历史）。
- **不做创建账号**：后端没有 `POST /users`（账号由 `admin-bootstrap` / `seed` 建）——
  放一个点了没反应的按钮正是 B1-1 那类死表单缺陷。
- **不叫「重置密码」**：全仓没有「admin 给他人设新密码」的端点。能做到的是置
  `must_change_password=true`（让本人下次登录改），所以按钮就叫「强制改密」。按做不到的名字
  做按钮就是骗运维。

### 7. 前端按**能力**门禁侧边栏，不复制角色矩阵

`buildMenuItems(hasIdentity)` 的判据是 `/auth/me` 下发的 `capabilities` 是否含 `identity` ——
不是 `role === "admin"`（`roles.go` 的注释明确警告过复制矩阵会漂移，且字面量比较会绕过
`CanonicalRole` 的别名折叠）。抽成导出纯函数是为了可断言（同 `buildTheme` 的先例）：
条件渲染写错的表现是「入口该有却没有」或「不该有却出现」，人工都不易发现。
`/users` 路由**不做**前端门禁：非 admin 手输会看到 403 错误态，比静默跳回首页更让人理解发生了什么。

### 8. 审计动作名走 context 键，不给出三条路由各挂一个 `AuditLog`

`ActionFunc` 默认取 HTTP method，于是 `PATCH /users/:id/status` 与 `PATCH /users/:id/role`
在审计表里都是 `PATCH` —— 「谁把谁提成了 admin」与「谁禁用了谁」分不出来（**T-73**）。
修法是 handler `c.Set("audit_action", …)`（单个 context 写，零额外写入）。
**不给每条路由单独挂 `AuditLog` 实例**：组级实例也在跑，那样每个请求会**写两行**审计。
连带修掉 `Action` 字段此前不截断（列宽 varchar(50)）：覆写路径进来的字符串没有宽度约束 →
超宽即 22001 → **整行**审计丢失（G-44 同族）。

## 学到 / Retro

- **「缺的不是能力，是触发能力的手」**：M40 把禁用即失效做进了鉴权侧（30s TTL + 两条路径都查
  status），但整整一轮没有任何人能从产品界面触发它。功能存在 ≠ 功能可达 —— 这正是 G-4 登记的原话
  「只能改库」。审查里「链路完整」的判据必须包含**入口**，不然会把「已实现」误当成「已交付」。
- **守卫的失败方向要选对**：自我禁用/降级选 fail-closed（拒绝），因为自锁后**自助恢复路径不存在**；
  而不选「允许 + 事后提示」。凡是「拒绝的代价 = 一次人工操作，放行的代价 = 系统失去自救入口」，
  就该拒绝。
- **同一条规则的「隐含前提」是最容易漏的地方**：`role='admin'` 看起来就是「有管理员」，
  但真问题是「有**能登进来**的管理员」（`status='active'`）。测试如果只构造 role 不构造 status，
  这个洞永远测不出来 —— mutation ② 就是为它准备的。
- **契约漂移借「第一个真实消费者」暴露**：`UserList.data` 声明成裸数组已存在很久（无人按它写代码），
  M61 的页面是第一个消费者。**在写消费方之前先把契约改成如实**，否则就是在照错的契约写新代码。
- **antd 组件的测试判据要按它真实的 DOM/时序语义写**：本轮三次修正测试而非实现 ——
  ① 关闭的 Popconfirm 节点留在 DOM 里（`ant-popover-hidden` 要等 transitionend，jsdom 不触发过渡，
  停在 `*-leave` 上）；② `Select` 的 `onMouseDown` 挂在 `.ant-select-selector` 上而非根节点；
  ③ 选项节点不带 `title`。三条都写进了用例注释 —— 下次有人把断言「简化」回去就会红。
- **实测未复现的观察（如实记录）**：本轮多次在**同一台机器上并发**跑 vitest + 两次 `go test`
  （N97 4 核），个别次出现 `Test Files 1 failed` 但复跑即绿 —— 与 M60 记录的是同一现象
  （M60 retro 已登记为「资源竞争下的偶发，未定位」）。本轮未做压测定位。
  唯一一次具名红是并行 M62 落地期间 `AssetFormModal.test.tsx` 的 `IPv4 256.0.0.1` 用例
  （同机并发两个套件时），单独复跑 4/4 PASS。
- **「全量绿」不等于「产品能跑」**：本轮同一份代码库，`47 files / 489 tests PASS` 与
  「任意路由白屏」同时成立。差别只在**验证面**：测试渲染的是零件（页面、layout、纯函数），
  从没有人渲染过**入口形状**（`main.tsx` 那两层 + `<App />`）。
  这不是断言不够密，而是**被断言的对象错了粒度** —— 补一条 2 用例的入口级用例，
  就抓住了三个月里所有轮次都漏掉的东西。
- **真机验证的价值在本轮是具体的**：白屏只有真浏览器（或入口级 jsdom 用例）能看见；
  而「乐观更新失败回滚」「toast 带服务端原因」这两条只有真浏览器才证明得了
  （jsdom 用例断的是 mock 被调用，真实 DOM 上的 Switch 回滚是另一回事）。
  本机没有真后端，但**桩 + 真浏览器**依然足以覆盖前端行为 —— 缺的只是后端语义那一段，
  而那段本来就有 Go 测试。

## Follow-up（留 future round）

- **入口级用例应成为后续 round 的默认项**：白屏（T-75）暴露的不是「某条用例没写」，
  而是「入口形状无人守」。建议后续任何改 `App.tsx` 层级/Provider 顺序的 round 都跑
  `App.render.test.tsx`；若引入新的全局 Provider（如 i18n / 状态库），同一文件里补一条
  按生产层数渲染的用例比在页面用例里加断言更有效。
- **双重 toast（M61 引入的可观测副作用）**：403 时拦截器先弹通用「没有权限访问」，
  页面再弹服务端原因（`forbidden: 不能禁用自己的账号`）—— 两条同时出现。
  页面侧那条是行动指引（改什么参数都没用），拦截器那条是全站既有行为。
  收敛需要改 `services/api.ts` 的拦截器（影响每一页），属独立一轮。

- **创建账号**：后端缺 `POST /users`。需要密码策略、初始密码下发/强改密（M49 已有 `must_change_password`
  机制可复用）、审计与「谁能创建 admin」的判据。
- **admin 给他人设新密码**（真正的「重置密码」）：需要新端点 + 审计 + 「不泄露新密码」的传递方式
  （一次性展示？邮件？）。M49 的强改密流程已覆盖「让本人改」这条路径，优先级待定。
- **批量处置**：批量禁用/改角色需要部分成功语义（同 M58 的 `{succeeded, failed}`）+ 逐条审计
  （M58 的选择是「按请求落一行」）。当前单账号足够。
- **真并发下的守卫验证**：两道守卫的行锁行为只在 sqlmock 的 SQL 文本层面被钉住。
  需要两条真 PG 连接 + 精确定时的集成用例（同 `cmd/set-role` 的既有登记）。
- **`user_roles` 关联表**：HTTP 路径只改 `users.role`（该表运行时零读取，`FIX-PLAN-AUTHZ.md` §7 已核实）。
  一旦出现读取方，`cmd/set-role` 的 `syncUserRoles` 与 HTTP 路径必须收敛到同一个 helper。
- **PII 脱敏**：不硬删就需要「离职后邮箱/手机脱敏」的独立设计（TODO 已登记）。
- **`GET /auth/me` 无轮询**：角色被改后侧边栏入口最长等下次刷新才更新（后端判权是每请求实时的，
  不依赖这个缓存）。若将来有「热改权限」需求再说。
- **部门树管理**：`User.DepartmentID` 一直在，但 department API 不存在，属另一件事。
