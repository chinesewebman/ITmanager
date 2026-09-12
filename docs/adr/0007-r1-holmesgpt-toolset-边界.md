# ADR-0007: HolmesGPT toolset 边界与认证

**状态**: ✅ 已采纳 (2026-09-12)
**日期**: 2026-09-12
**决策者**: 主人 (燕如) + 助手
**关联**: [ADR-0003](0003-grpc-作废与三层定位.md)（AI 层只读、写操作必须由人）、[v3 §3 R1](../v3-架构优化需求.md)、[`09-AI辅助.md`](../../09-AI辅助.md) §9.0-9.4、[`docs/FIX-PLAN-R1-HOLMESGPT.md`](../FIX-PLAN-R1-HOLMESGPT.md)

---

## 背景

v3 §3 R1 决策把 ITmanager 从「自建 LLM 问答」重定位为「HolmesGPT 的数据源」。HolmesGPT 是 CNCF Sandbox 项目，agentic 多轮工具调用 + 证据链；ITmanager 不重复造轮子，只暴露只读 API。

v3 §3 R1 风险条款明确警告：

> HolmesGPT 拿不到 ITmanager 独有的业务上下文（诊断时间线、MTTR、Runbook 推荐），退化成只能查 Zabbix/Splunk 的通用侦探 → **缓解**：toolset 必须显式包含 `GET /api/v1/diagnostics/assets/{id}/timeline` 与 `GET /api/v1/runbooks/recommend`，不接受「给裸 API 让它自己猜」。

换句话说，toolset 端点列表就是 ITmanager 在 AI 架构中的存在价值。本 ADR 把 5 条硬约束写下来，避免后续 implementor「好心扩展 toolset」导致写操作泄出或工具集膨胀到不可控。

---

## 决策

1. **toolset 端点列表 = 5 个，固定不变**：
   - `GET /api/diagnostics/assets/{id}/timeline`
   - `GET /api/runbooks/recommend`
   - `GET /api/assets/{id}`
   - `GET /api/alerts`
   - `GET /api/dashboard/kpis`

   路径无 `/v1` 前缀（详见 `09-AI辅助.md` §9.2 行号注 2026-09-09 校对 `routes.go:218/242/279/294/345`）。

   **禁止**扩到 `GET /api/...`（裸 API 全开）——v3 §R1 风险条款明文禁止。
   **禁止**把 write 类端点（ack / 建单 / 删除 / 重置密码 / 配置变更）加入 toolset——ADR-0003 三层定位的硬约束。

2. **认证 = 单独 API Key，role=ai_toolset，最小 scope**：
   - 不复用 admin 账号。
   - 不复用 operator / viewer 账号。
   - Key 单独签发、可独立轮换、独立审计、独立吊销。
   - role 词表与权限矩阵见 [ADR-0005](0005-角色词表与权限矩阵.md)。

3. **写操作不暴露**：toolset 调用必须仅限 GET。任何 mutating HTTP method（POST/PUT/PATCH/DELETE）即便 endpoint 存在也不进 toolset YAML。

4. **审计必走**：所有 toolset 调用进 `audit_logs` 表。沿用现有审计通路，不新建表；阶段 1（不在本轮）通过中间件统一埋点。

5. **失败语义统一**：toolset 端点允许返回 401/403/404/500，HolmesGPT 侧需转译为「数据不可用」（避免幻觉为「系统错误」或「用户输入有误」）。具体映射不在本 ADR 范围，由 HolmesGPT 侧配置决定。

---

## 后果

### 正面

- ITmanager 的 AI 暴露面是**静态的、可审计的**，未来加新 toolset 端点必须显式改本 ADR。
- 写操作硬不暴露，杜绝 LLM 幻觉带来的「AI 自动建单/ack」类风险（v3 §5 明文不做「AI 触发自动建单」）。
- 单独 API Key + role=ai_toolset 让权限边界与 token 生命周期与运维/业务账号彻底隔离。

### 负面 / 残余

- **业务上下文「瘦」**：5 个端点可能不够覆盖 HolmesGPT 真实诊断场景。**缓解**：阶段 2 联调时按需迭代本 ADR（加端点要走 ADR 修订流程，不可越权改 routes.go）。
- **路径漂移**：routes.go 重构后路径变了，本 ADR 与 `09-AI辅助.md` §9.2 路径注都会过时。**缓解**：routes.go 改路由的 PR 必须同步更新 §9.2 注 + 本 ADR 的「行号对照表」（非硬性约束，但 PR review 必须点名）。
- **审计开销**：阶段 1 加中间件后，AI 工具调用会显著增加 audit_logs 写入量。**缓解**：阶段 1 实施时评估，必要时给 audit_logs 加 partitioning 或降采样策略。
- **5 个端点的 schema 变更**：如果未来 `GET /api/dashboard/kpis` 加了新字段（KPI 维度），下游 HolmesGPT toolset 配置也要变。**缓解**：openapi.yaml 是契约的 SoT，schema 变更走 `npm run validate:api` 守门（per M31 G-37 闭环经验）。

---

## 不可逆性

**不可逆**：本 ADR 一旦签发，5 个端点列表 + 5 条硬约束即成为 ITmanager AI 暴露面的契约。任何扩展需要显式修订本 ADR（status 从 ✅ → 🔄 修订中 → ✅ v2）。

---

## 替代方案（已 reject）

| 方案 | 拒绝理由 |
|---|---|
| 给 HolmesGPT 整个 `/api/...` 范围，让它自己挑 | v3 §R1 风险条款明文禁止；等同于「裸 API 全开」反模式 |
| 复用 admin token | 一旦泄漏即获得全部权限；token 轮换与其他 admin 不可分离 |
| 不写 toolset，让 HolmesGPT 直连 PG | 绕开所有应用层安全/审计；违反 ADR-0003 三层定位 |
| 自建 LLM + RAG | v3 §1 P-2 决策已 reject；能力代差、维护成本 |

---

## 实施阶段

| 阶段 | 内容 | 估计 |
|---|---|---|
| 0（本轮 docs-only） | ADR + FIX-PLAN + IMPL + CHANGELOG 落定 | 30min |
| 1（未来） | 中间件埋点：toolset 调用全部写 audit_logs | 1-2h |
| 2（未来） | 实际部署 HolmesGPT + 签发 role=ai_toolset token + 端到端联调 | 4-6h |
| 3（未来） | HolmesGPT 侧写 ITmanager 专属 toolset 配置 | 2h |

---

## 交叉引用

- [ADR-0003](0003-grpc-作废与三层定位.md) — 上位决策：AI 层只读、写操作必须由人
- [ADR-0005](0005-角色词表与权限矩阵.md) — role 词表与权限矩阵（role=ai_toolset 在此登记）
- [`docs/v3-架构优化需求.md` §3 R1](../v3-架构优化需求.md) — 需求源头
- [`docs/FIX-PLAN-R1-HOLMESGPT.md`](../FIX-PLAN-R1-HOLMESGPT.md) — 本 ADR 的 FIX-PLAN
- [`docs/IMPL-R1-HOLMESGPT.md`](../IMPL-R1-HOLMESGPT.md) — 本 ADR 的实施细节
- [`09-AI辅助.md`](../../09-AI辅助.md) §9.0-9.4 — 既有 v3 段落（与本 ADR 一致）
