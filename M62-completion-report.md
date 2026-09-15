# M62-completion-report — G-UI-AssetIpValidator + utils/validators 落地（M60 follow-up）

**Shipped**: 2026-09-15（commits `1e460fc` feat validators ipRules / `f738bc7` test validators /
`c592512` feat AssetFormModal / `9be9576` test AssetFormModal；docs 本次）
**Scope**: frontend 4 files（backend 零改动；临时探针文件用后即删）
**摩擦**: `AssetFormModal.tsx:95` 的 `ip_address` 内联 `/^(\d{1,3}\.){3}\d{1,3}$/` 是**形状**检查
而非**地址**检查（`256.0.0.1` / `999.999.999.999` 放行），且 M60 把它列进「其他页接入
`utils/validators`」的 follow-up 里一直没接。
**Time**: ≤2h omp round

## 改动

| 文件 | 改动 |
|---|---|
| `frontend/src/utils/validators.ts` | 新增 `OCTET` / `HEX_GROUP` / `IPV4_BODY` / `IPV6_BODIES[10]` / `IPV4_PATTERN` / `IPV6_PATTERN` / `IP_PATTERN` / `ipRules`（M60 的 4 组规则一字未动） |
| `frontend/src/components/AssetFormModal.tsx` | `rules={[{required…}, {pattern: /^(\d{1,3}\.){3}\d{1,3}$/…}]}` → `rules={ipRules}` + import；删旧文案「IP 格式不正确」 |
| `frontend/src/utils/validators.test.ts`（新） | 55 用例：IPv4 16 / IPv6 17 / `IP_PATTERN` 12 / `ipRules` 3 / M60 四组规则 7 |
| `frontend/src/components/AssetFormModal.test.tsx`（新） | 4 用例：`192.168.1.1` 过并提交 · `256.0.0.1` 挡且**不提交** · `::1` 过 · 留空必填 |

## Hard pass

| 维度 | 结果 |
|---|---|
| frontend `npx tsc --noEmit` | **0 error** ✓ |
| frontend `npx vitest run src/utils/validators.test.ts` | **55 tests PASS** ✓ |
| frontend `npx vitest run src/components/AssetFormModal.test.tsx` | **4 tests PASS** ✓ |
| frontend `npx vitest run src/pages/Settings.test.tsx` | **58 tests PASS**（未触碰）✓ |
| frontend 全量 `npx vitest run` | **47 files / 489 tests PASS** ✓（M62 前 45 / 430，零退化） |
| eslint（改动 4 文件，`--max-warnings 0`） | 干净 ✓ |
| mutation ①（`rules={ipRules}` → required-only） | `AssetFormModal.test.tsx` **1 failed \| 3 passed** ✓ |
| mutation ②（`OCTET` → `\d{1,3}`） | `validators.test.ts` **4 failed** + UI **1 failed** ✓ |
| mutation ③（IPv6 换回 brief 单行字面量） | `validators.test.ts` **6 failed** ✓ |
| 还原后复跑 | **59/59 PASS**，`git status` 干净 ✓ |
| 双轨分析 | graphify 7002 / 14420 / 453 + 0 anomalies；codegraph 新符号入图（见 `M62-graph-analysis.md`）✓ |

## 关键设计决策（含理由）

### 1. brief 的 IPv6 单行字面量**不能照抄**（本轮唯一的「偏离 brief」）
`intent-M62.md` 给的是一条把 10 个交替式连成一行的字面量。其中
`^([0-9a-fA-F]{1,4}:){1,7}:`（右侧无 `$`）与 `:((:[0-9a-fA-F]{1,4}){1,7}|:)$`（左侧无 `^`）
各自只带一半锚点 —— 粘成单条 regex 后**整个** pattern 在 standalone 使用下退化成子串匹配：
实测 `IPV6_PATTERN.test('zz::1') === true`、`test(':::') === true`。

为什么它「看起来能用」：antd 的 `pattern` 规则走 `value.match(pattern)` 且只判「有没有匹配」，
`zz::1` 在表单里本来也该被拒（它的 v4/v6 都不匹配？—— 不，`zz::1` 会被这条坏 pattern 匹配到
`:1` 那段而**放行**，等于表单也漏），所以坏处不是「只在测试里有害」。修法：10 条形状列成数组、
边界只写一次（外层 `^(?:…)$`），并把族里最易改错的「`::` 两侧组数上界」写成可逐行核对的表。

差异已量化核对（一次性脚本，已删）：长度 ≤7 的字母表穷举 + 合法/非法种子做 1–3 次字符级扰动，
共 **336,949 条样本**：

| pattern | 与 brief 字面量的分歧 | 方向 |
|---|---|---|
| `IPV4_PATTERN` | **0** | — |
| `IP_PATTERN` | **0** | — |
| `IPV6_PATTERN` | 24,753 条 | **单向**：全部是「brief 收、本实现拒」（`zz::1` / `:::` / `a::b::c` / `.::0` …）；反向 0 条 |

即：本实现是 brief 字面量的**严格子集**（收的全是合法地址），没有任何「brief 拒而我方收」的放松。

### 2. IPv6 支持不是「顺手加功能」，是与存储域对齐
`asset_networks` 有 `ipv6_address` 列，`postmortem_service.fetchIP` 明确按
「先第一个非空 IPv4，否则第一个 IPv6」取地址（`postmortem_service.go:97-121`）。旧前端 pattern
只认四段点分十进制 → **前端比存储域更窄**，`::1` 这类合法值在表单层被挡。本轮把闸对齐到
「v4 或 v6」，并明确**不做** zone id（`fe80::1%eth0`）、CIDR、IPv4-mapped：
zone id 与 CIDR 逐字写进 `IPV6_PATTERN` 的注释，IPv4-mapped 记在本报告与 CHANGELOG 的 Out of scope。

### 3. 两层测试，同一道闸（不是重复劳动）
- `validators.test.ts` 钉**规则本身**：`256.0.0.1` / `999.999.999.999` / `1.2.3` /
  `1.2.3.4.5` / `192.168.1.1␠` / `192.168.1.0/24` / `zz::1` / `:::` / 9 组 / 5 位组 / zone id；
  外加 `ipRules[1].pattern === IP_PATTERN`（**禁止第二份拷贝** —— 页面里再抄一条 pattern 是本轮要消灭的形态）。
- `AssetFormModal.test.tsx` 钉**接线**：`256.0.0.1` 显示文案**且 `onSubmit` 未被调用**。
  mutation ①只让后者红、mutation ②让两者同时红 —— 这正是「不同输入、同一规则」的证据。
- **不重复 M59 的 URL/EMAIL 正负样本表**：那 16 条已在 `Settings.test.tsx` 且搬家后一字未改；
  同一契约钉两处只会让将来放宽规则时要改两个文件。本文件只补「M60 四组规则对象的文案/约束」——
  此前它们**只在 UI 层被间接打到**（`arrayOfPatternRules` 的 trim、非数组放行、第三参默认值
  都还没有直接的模块级断言）。

### 4. `AssetFormModal.test.tsx` 不 mock 服务、不经过 Assets 页
组件是纯的：校验通过才调 `onSubmit` → 「有没有走到提交」就是放行与否的观测点，
不需要 axios adapter、不需要 QueryClient，也不依赖页面里 `Asset` 的字段填充。
（对比 `TicketDetailModal.test.tsx` 走真 axios adapter：那边要验的是请求/拦截器，
这里要验的是**表单闸**，两处的取舍不同是有意的。）
antd 细节：两个汉字的按钮会被插空格（`创 建`），故按 `name: /创\s*建/` 匹配。

## 验证链

见 `M62-graph-analysis.md` 的「验证链完整性」表（tsc / 两个新测试文件 / Settings / 全量 /
eslint / 3 处 mutation + 还原 / 336,949 条 pattern 等价性核对 / graphify 0 anomalies / codegraph 入图）。

## 行为变更（运维可见）

1. 资产弹窗填 `256.0.0.1` → 字段级红字「IP 地址格式不正确 (IPv4: 192.168.1.1, IPv6: ::1)」，
   **不发请求**；此前会一路发到后端。
2. 资产弹窗现在**接受** `::1` / `fe80::1` / `2001:db8::1` 等 v6 字面量（旧 pattern 拒）。
3. 与旧闸的关系是**单向下收**：新闸不放行任何旧闸挡下的 v4 形状（旧闸唯一的宽松点是段值越界）。

## 残余 / 未覆盖（如实登记，不假装完整）

- **【新发现，scope 外，已登记 TODO，未修】该字段的值当前落不了库**：
  - `POST /assets` → `handler` 用 `c.ShouldBindJSON(&models.Asset)`，而 `models.Asset`
    **没有 `ip_address` 字段** → 键在绑定阶段被静默丢弃。**实测证据**（临时 `models` 包测试，
    已删）：把 `{"name":"web-01","asset_type":"server","ip_address":"256.0.0.1"}` 反序列化再
    序列化，输出 JSON **不含** `ip_address`。
  - `PUT /assets/:id` → `service.Update` 把整张 map 交给 `Model(&asset).Updates(map)`，
    GORM v1.30.0 对**模型里不存在的键不丢弃**（`callbacks/update.go:211-232`：`LookUpField`
    为 nil 时仍 `append(clause.Assignment{Column: {Name: k}})`，因为 `selectColumns` 为空
    且 `restricted=false`）→ 生成 `SET ip_address = $n` → PG `42703`（`assets` 无此列）→
    handler 走 `apierr.Internal` → **500**。此条是**源码级核实**（GORM 源码 + PG 错误码语义），
    **未在真 PG 上实测**（本轮无 PG 实例）。
  - 连带：`GET /assets` 不投影 IP，故前端 `Asset.ip_address` 列与 Ping/Traceroute 按钮
    （`AssetTable.tsx:149,159` 以 `!record.ip_address` 禁用）在生产数据上是空转 ——
    只有在 `Assets.test.tsx` 的 mock 数据里才「有 IP」。**这解释了为什么「加一个前端闸」
    不会改善任何真实数据**：本轮的产出是把它做成可复用、且**与存储域同域**的规则，
    为将来那条后端写路径准备好唯一出口。
- **API 直连不经过这道闸**：`POST/PUT /assets` 直接收 `ip_address` 字符串，前端 pattern 拦不住
  脚本调用（同 M60 的 T-56 形态）。若将来修写入路径，校验应落在后端（或列类型回到 `INET`）。
- **IPv6 的 CI 覆盖只到 `asset_networks.ipv6_address` 的读取路径**：本轮没有验证 v6 值能否
  走完写入/查询（因为没有写入路径 —— 见上一条）。
- **`IPV6_PATTERN` 无 in-repo 消费者**（codegraph 确认）：它是模块对外声明的边界样本，
  被 17 条用例直接读；按 T-72 口径登记为「导出但无人消费」—— 保留的理由是测试需要按族
  断言（v4 与 v6 的边界不同），**不是**为了凑 API 对称。

## 学到 / Retro

- **brief 里的正则要当作「意图」而不是「文本」来读**：本条 brief 的字面量在表单里「表现得还行」
  （坏 pattern 对坏值也常常为真），只有 standalone 断言才暴露。若本轮只按 brief 抄下来 +
  只写 UI 层用例（brief 只要求 3 条 UI 用例），这个缺陷会以「测试全绿」的形态进 main。
  **规则层用例的价值在这里**：它把「pattern 自身是否守边界」变成可断言的事实，
  而不是靠消费方（antd）的宽松调用方式掩盖。
- **「加前端校验」的收益上限由写入路径决定**：本轮最大的收获不是 IP 正则，而是查清了
  「这个字段的值根本没落库」（create 丢键 / update 撞不存在的列）。**先问值去哪了，
  再问值合不合法** —— 顺序反了就会像本轮一样，修完发现是空转。
- **mutation 打在「新接的线」上最有效**：①打 `rules={ipRules}` 这一行（不是打 pattern 内部），
  它证明的是「界面真的用了共享规则」；②③打在 pattern 内部，证明的是「规则本身对」。
  三类变异各自红在不同的断言上，说明测试分层不是装饰。
- **零成本核对工具值得即写即删**：336,949 条样本的等价性核对脚本跑了 2.7 秒，
  把「我说我的实现更好」变成「两个判定函数在 33 万输入上分歧 24,753 条、方向单一」。
  一次性脚本按要求删除，结论留在本报告与 CHANGELOG 里。

## Follow-up（留 future round）

- **G-UI-AssetIpPersistence（本轮新登记）**：让资产的 IP 真正落库 ——
  `POST/PUT /assets` 应写 `asset_networks`（第一张网卡），且 v4 进 `ipv4_address`、v6 进
  `ipv6_address`（不要都塞一列：两列并存正是为区分族）；`GET /assets` 投影第一张网卡的 IP
  让列表与 Ping/Traceroute 按钮不再空转。跨 backend（service + handler + openapi + 前端类型），
  属独立一轮。
- **其他页接入 `utils/validators`**：Oncall（name / timezone / description 无格式可校验，
  本轮确认**无料可接**）、Runbook（webhook URL 类）、TicketForm —— 逐页评估文案变更面。
- **CIDR / FQDN / 端口范围**：`asset_networks.ipv4_netmask` 列存在但无表单字段；CIDR 若要支持，
  必须与后端写入路径同轮做（避免又一条「前端能填、后端存不下」的字段）。
- **`ws://` / `wss://`**（M60 起未动）。
- **SMTP user 强 email 的 SASL 误伤**（M59 起未解）。
