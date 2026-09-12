# 修复方案：R1 — HolmesGPT toolset 接入（v3 改造点一）

> 状态 **draft** · 日期 2026-09-12 · 作者 hermes@local
> 依据：[`docs/v3-架构优化需求.md`](../v3-架构优化需求.md) §3 R1 + §7 A-6
> 根因证据：v3 §1 P-2（自建 LLM 与 HolmesGPT 能力代差）+ §3 R1 风险（HolmesGPT 拿不到业务上下文）
> 前置：R5（已落）、R2（已落）。本轮是 R1 首次落地。
> 关联：[`09-AI辅助.md`](../../09-AI辅助.md) §9.0-9.4（已有 v3 段落）、[`docs/adr/0007-r1-holmesgpt-toolset-边界.md`](../adr/0007-r1-holmesgpt-toolset-边界.md)（本轮新建）、[`docs/IMPL-R1-HOLMESGPT.md`](../IMPL-R1-HOLMESGPT.md)（本轮新建）

---

## 1. 问题（What / Why）

### 1.1 P-2：自建 LLM 问答能力代差

v1/v2 时期设计了自建 AI 服务层（拼 prompt、调 OpenAI/Azure/Claude/本地模型），但：

|| 事实 | 证据 |
|---|---|---|
| **0 行 Go 代码** | `rg "ai_conversations\\|llm_providers\\|ai_messages" backend/internal/api/` 仅命中旧 `09-AI辅助.md` 提及，无任何 handler/service/route |
| **0 端点 0 页面** | `routes.go` 无 `/api/v1/ai/analyze` 或类似；前端 `frontend/src/` 无 AI 聊天 UI |
| **能力代差** | HolmesGPT 是 CNCF Sandbox 项目，50+ 内置 toolset、agentic 多轮工具调用 + 证据链；自建 prompt 一次性问答无法追上 |

v3 §3 R1 决策：砍自建、改接 HolmesGPT。ITmanager 只提供只读 API 作为数据源，写操作仍由人完成（ADR-0003 三层定位硬约束）。

### 1.2 业务上下文不可替代

`docs/v3-架构优化需求.md` §3 R1 风险条款明确指出：

> HolmesGPT 拿不到 ITmanager 独有的业务上下文（诊断时间线、MTTR、Runbook 推荐），退化成只能查 Zabbix/Splunk 的通用侦探 → **缓解**：toolset 必须显式包含 `GET /api/v1/diagnostics/assets/{id}/timeline` 与 `GET /api/v1/runbooks/recommend`，不接受「给裸 API 让它自己猜」。

换句话说，**toolset 端点列表就是 ITmanager 在 AI 架构中的存在价值**。定义错了，整套 v3 AI 重定位方案就退化成空壳。

### 1.3 本轮是 docs-only：五个端点已存在

09-AI辅助.md §9.2 已经列了 5 个 toolset 端点；§9.4 已经给了 HolmesGPT 侧 YAML 配置示例；§9.3 已经写了三条安全边界（只读 token、写操作不暴露、audit_logs）。本轮把这些散落的信息**收口成 ADR + FIX-PLAN + IMPL**，让后续人/RPA/AI agent 接手时有一份索引可循，而不是反复读 09-AI辅助.md 一整章。

端点路径已在 2026-09-09 校对过 `backend/internal/api/routes.go`：

| 端点 | routes.go 行号 |
|---|---|
| `GET /api/diagnostics/assets/{id}/timeline` | 218 |
| `GET /api/runbooks/recommend` | 242 |
| `GET /api/assets/{id}` | 279 |
| `GET /api/alerts` | 294 |
| `GET /api/dashboard/kpis` | 345 |

（路径无 `/v1` 前缀，初版文档误写已修正——见 09-AI辅助.md §9.2 末尾注）

---

## 2. 计划（Plan）

### 2.1 决策（与 ADR-0007 对齐）

1. **toolset 端点列表 = 5 个**（见上表）。**不接受**「给 HolmesGPT 整个 `/api/...` 让它自己挑」——v3 §R1 风险条款明文禁止。
2. **写操作不暴露**：ack / 建单 / 删除 / 重置密码 / 配置变更等所有 mutating 端点均不进 toolset；HolmesGPT 看到的 API 是「只读子集」。
3. **认证**：单独签发 `role=ai_toolset` 的 API Key（最小 scope、唯一、不复用 admin），独立审计入口。
4. **审计**：所有 toolset 调用必须经 `audit_logs` 表记录（沿用现有审计通路，不新建表）。
5. **失败语义**：toolset 端点允许返回非 2xx，HolmesGPT 看到 401/403/404/500 应转译为「数据不可用」而非「系统错误」（避免幻觉）。

### 2.2 端点契约（API contract）

**所有 5 个端点的入参/出参见 `09-AI辅助.md` §9.2 与 `backend/internal/api/openapi.yaml` 的对应节。**

contract 关键点（写入 v3 强制约束）：

- `GET /api/diagnostics/assets/{id}/timeline`：入参 `{id}` 必须为 UUID；返回结构含 `events: [{ts, source, kind, summary, ref}]`，`source ∈ {alert, ticket, asset_change, network_change}`。
- `GET /api/runbooks/recommend`：入参 `{asset_type, severity}`；返回 `[{id, title, steps: [string], confidence: float}]`，**不允许**包含「自动执行」类步骤（必须由人触发）。
- `GET /api/assets/{id}`：返回资产 360 视图，**必须**包含 `related_alerts: int`、`open_tickets: int`、`recent_changes: int` 三个聚合字段（供 HolmesGPT 评估资产健康度）。
- `GET /api/alerts`：只读列表；不允许带任何 mutation 参数；`status` 过滤仅支持 `active|resolved|all`，不接受 `pending_delete` 等内部态。
- `GET /api/dashboard/kpis`：返回 `{mttr_seconds, mttd_seconds, sla_compliance_pct, window}`；`window` 默认 `24h`，最大 `30d`。

### 2.3 实施分阶段（不在本轮，但留给后续）

- 阶段 0（本轮 docs-only）：spec 落地。
- 阶段 1（未来，需 1-2h）：为 5 个端点加 `audit_logs` 调用埋点（可能通过中间件统一处理）。
- 阶段 2（未来，需 4-6h）：实际部署 HolmesGPT，签发 `role=ai_toolset` API Key，做端到端联调。
- 阶段 3（未来，需 2h）：HolmesGPT 侧写 ITmanager 专属 toolset 配置（基于 09-AI辅助.md §9.4 YAML 模板）。

---

## 3. 验收（A-6）

按 v3 §7 A-6：

> A-6 | R1 定义出 HolmesGPT toolset 的最小端点集 | `09-AI辅助.md` 含端点清单

更细的可验证条款：

- A-6.1：`09-AI辅助.md` §9.2 含 5 个 toolset 端点列表 + 路径已校对 routes.go 的注。
- A-6.2：`docs/adr/0007-r1-holmesgpt-toolset-边界.md` 存在，决策节含 5 条硬约束（对应 §2.1）。
- A-6.3：本 FIX-PLAN 文档存在且 §2.2 端点契约完整。
- A-6.4：`docs/IMPL-R1-HOLMESGPT.md` 存在，把 §2.1 决策与 §2.2 契约收尾为可执行细节。
- A-6.5：`CHANGELOG.md` 含 M35-R1 段，记录本次提交。
- A-6.6：`M35-R1-completion-report.md` 按 `task-completion-protocol` 5-section 模板产出。

---

## 4. 不做（NOT doing）

- 实际写 endpoint handler 代码（5 个端点已存在于 routes.go）。
- 实际部署 HolmesGPT 或签发 token。
- 改 `09-AI辅助.md` §9.4 的 YAML 字段（已是 spec 形态）。
- 改 schema / migrations / openapi.yaml（端点 schema 已与代码一致）。
- 加 `ai_conversations` / `ai_messages` / `llm_providers` 三表（v3 §5 明文不做，保留 `docs/schema-planned.sql` 作历史归档）。

---

## 5. 实施

见 `docs/IMPL-R1-HOLMESGPT.md`。

---

## 6. 风险

| # | 失败模式 | 缓解 |
|---|----------|------|
| 1 | HolmesGPT 调用工具时幻觉「写」类操作 | ADR-0007 §决策.2 硬约束；toolset scope 严格只读 |
| 2 | token 泄漏导致非授权读 | ADR-0007 §决策.3；单独签发、scope 最小、定期轮换 |
| 3 | 5 个端点路径后续漂移（routes.go 重构） | `09-AI辅助.md` §9.2 行号注需随 PR 更新（2026-09-09 已核对一次） |
| 4 | audit_logs 写入开销 | 阶段 1 中间件方案时评估；当前阶段无需埋点 |
| 5 | HolmesGPT 版本升级后 YAML 字段变化 | `09-AI辅助.md` §9.4 注释为「示例」，不绑定具体 HolmesGPT 版本 |

---

## 7. 落地位置（Where）

- ADR：`docs/adr/0007-r1-holmesgpt-toolset-边界.md`（新建）
- 本文档：`docs/FIX-PLAN-R1-HOLMESGPT.md`（新建）
- IMPL：`docs/IMPL-R1-HOLMESGPT.md`（新建）
- CHANGELOG：`CHANGELOG.md` M35-R1 段（追加）
- TODO：本次无新 G 项；现有 G-31 / G-32 / G-33 / G-34 / G-35（错误文本 / 编码 / 凭据相关）暂不阻塞 R1
- 09-AI辅助.md：**不修改**（已是 v3 形态）

---

## 8. 验证

- `git log --oneline -10` 含本轮的 5 个 commit（intention + ADR + FIX-PLAN + IMPL + CHANGELOG）。
- `git push` 全部成功。
- 5 个 docs 文件内容交叉互链（FIX-PLAN 链 ADR + 09；IMPL 链 FIX-PLAN）。
- 不破坏既有 go test / db_smoke（无代码改动）。

---

## 9. 交叉引用

- v3 需求：`docs/v3-架构优化需求.md` §3 R1 line 66-72 + §5 不做清单 + §7 A-6 line 130
- 既有 v3 段落：`09-AI辅助.md` §9.0-9.4 line 5-73
- 相关 ADR：`docs/adr/0003-grpc-作废与三层定位.md`（AI 层只读、写操作必须由人 — 本 ADR 的上位）
- 相关 R：`R2`（已落 — `docs/FIX-PLAN-R2-NETBOX-CIRCUITS.md`）、`R5`（已落 — `docs/v3-架构优化需求.md` R5 节）
