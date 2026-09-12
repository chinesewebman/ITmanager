# IMPL-R1-HOLMESGPT: 实施细节

> 状态 **active** · 日期 2026-09-12 · 作者 hermes@local
> 依据：[`docs/FIX-PLAN-R1-HOLMESGPT.md`](../FIX-PLAN-R1-HOLMESGPT.md)、[`docs/adr/0007-r1-holmesgpt-toolset-边界.md`](../adr/0007-r1-holmesgpt-toolset-边界.md)
> 范围：本轮 docs-only; 阶段 1~3 不在本轮。

---

## 0. 变更清单（D-1 ~ D-5）

| # | 文件 | 行数估算 | 内容 |
|---|------|---------|------|
| D-1 | `intent-M35-R1.md` (repo root) | +99 | intent-spec-author artifact |
| D-2 | `docs/FIX-PLAN-R1-HOLMESGPT.md` | +152 | 需求/计划/验收/Risk 7 段 |
| D-3 | `docs/adr/0007-r1-holmesgpt-toolset-边界.md` | +102 | 5 条决策 + 不可逆性 + 替代方案 |
| D-4 | `docs/IMPL-R1-HOLMESGPT.md` (本文件) | +200 | 实施细节 + 收口 |
| D-5 | `CHANGELOG.md` M35-R1 段 | +5 | release notes |

**范围外（不动）**：
- `09-AI辅助.md` §9.0-9.4（已是 v3 形态）
- `routes.go`（5 端点已存在，路径 2026-09-09 校对过）
- `schema.sql` / 任何 migration / `openapi.yaml`
- `backend/internal/api/handlers/` / `services/` / `models/`
- `frontend/src/`
- `TODO.md`（本次无新 G 项；现有 G-31~G-35 不阻塞 R1）

---

## 1. 端点契约落地（无需代码）

5 个 toolset 端点的入参/出参在 v3 R1 文档落定，**实际 handler 代码已存在**：

| 端点 | 路径（routes.go 实测） | 入参 | 出参关键字段 |
|------|------------------------|------|--------------|
| timeline | `GET /api/diagnostics/assets/{id}/timeline` | `{id}` UUID | `events: [{ts, source, kind, summary, ref}]`, `source ∈ {alert, ticket, asset_change, network_change}` |
| runbooks | `GET /api/runbooks/recommend` | `{asset_type, severity}` | `[{id, title, steps: [string], confidence: float}]` |
| assets 360 | `GET /api/assets/{id}` | `{id}` UUID | 含 `related_alerts: int`, `open_tickets: int`, `recent_changes: int` |
| alerts | `GET /api/alerts` | query params | `status` 仅支持 `active|resolved|all`; 不接受内部态 |
| kpis | `GET /api/dashboard/kpis` | `{window}` 默认 `24h` 最大 `30d` | `{mttr_seconds, mttd_seconds, sla_compliance_pct, window}` |

handler 实现若与上表不符，**不在本轮范围**——已在 ADR-0007「负面/残余」§路径漂移登记，由未来 PR 显式修订。

---

## 2. ADR-0007 5 条决策的物理体现

| 决策 | 物理体现 | 阶段 |
|------|----------|------|
| ① 5 端点列表固定 | `09-AI辅助.md` §9.2 表 + ADR-0007 §决策.1 + FIX-PLAN §2.1 | 0 (本轮) |
| ② 单独 token + role=ai_toolset | 待 OpenAPI 角色矩阵支持 | 2 (未来) |
| ③ 写操作不暴露 | toolset YAML 仅含 GET（`09-AI辅助.md` §9.4） | 0 (本轮, spec) + 2 (部署) |
| ④ audit_logs 必走 | 阶段 1 中间件 | 1 (未来) |
| ⑤ 失败语义统一 | HolmesGPT 侧配置 | 2 (未来) |

---

## 3. 阶段 1~3 实施细节（不在本轮，但留口子）

### 3.1 阶段 1：audit_logs 中间件埋点（1-2h）

新增 `backend/internal/middleware/audit_toolset.go`：
- 拦截所有 `/api/diagnostics/`, `/api/runbooks/`, `/api/assets/{id}`, `/api/alerts`, `/api/dashboard/kpis` 路径
- 仅 audit `role=ai_toolset` 的 request（避免运维操作也被双写）
- 写 `audit_logs` 行 `{actor=ai_toolset_key_xxx, action=toolset_call, resource=<endpoint>, ip, request_id, ...}`
- 必须验证 `audit_logs.resource` 列宽（G-55 fix 已迁 50，VARCHAR(50) 够 `diagnostics/assets/{id}/timeline` 字符串）

风险：增加约 1-3% QPS 开销（每条 toolset 调用多一次 INSERT）。**缓解**：批量写入（aggregate 1s 内多次调用）。

### 3.2 阶段 2：HolmesGPT 部署与端到端联调（4-6h）

依赖：
- HolmesGPT 0.x+（最新 stable）
- 内网 DeepSeek 或 Ollama（推荐内网 — 数据不外流）
- ITmanager 部署在 HTTPS endpoint（自签证书 HolmesGPT 需 `INSECURE_SKIP_VERIFY=true` 或带 CA bundle）
- 新建 `roles` 表中插入 `ai_toolset` 角色（参考 `roles/code='ai_toolset'`）
- 新建 `users` 表中插入 `username='ai_toolset_holmesgpt'` 用户（service account）
- 签发 API Key，给该用户挂 `role=ai_toolset`，scope=read-only

联调：
- 用 HolmesGPT 模拟「服务器告警 → 调 timeline → 调 assets/{id} → 调 runbooks/recommend」完整链路
- 验证 audit_logs 行确实产生
- 验证 HolmesGPT 不会试图 POST（即便误传，server-side reject by middleware）

### 3.3 阶段 3：HolmesGPT 侧 toolset 配置（2h）

基于 `09-AI辅助.md` §9.4 YAML 模板，按实际 HolmesGPT 版本调整字段。配置存 ITmanager 这边的 secrets manager，不直接进 git。

---

## 4. 执行顺序

**本轮 (docs-only)**：
1. ✅ D-1 intent spec
2. ✅ D-2 FIX-PLAN
3. ✅ D-3 ADR-0007
4. → D-4 IMPL (本文件)
5. → D-5 CHANGELOG + closure

**未来 (阶段 1~3)**：
- 阶段 1 → 阶段 2 → 阶段 3

---

## 5. 验证

本轮（docs-only）的验证：
- `git log --oneline -10` 含 5 commits（intention + FIX-PLAN + ADR + IMPL + CHANGELOG）
- `git push` 全部成功
- 5 docs 文件相互交叉互链（FIX-PLAN 链 ADR + 09；IMPL 链 FIX-PLAN；CHANGELOG 引用全部 4 commits）
- 不破坏既有 go test / db_smoke / go vet（无代码改动）

---

## 6. 与 R2/R5 的接口

- **R2 已落**（`docs/FIX-PLAN-R2-NETBOX-CIRCUITS.md` + ADR-0006）—— R1 阶段 2 部署时，端点路径之一 `GET /api/assets/{id}` 应返回 `related_lines: [...]` 聚合（来自 NetBox Circuits read-only）。**协调**：阶段 2 实施时再补，不需要本轮做。
- **R5 已落**（CHANGELOG / TODO / ADR-0003 / 13 章 audit）—— R1 本轮落定后，§7 A-1 / A-2 / A-6 三条验收从「TODO」转「DONE」。

---

## 7. 残余风险

| # | 风险 | 缓解 |
|---|------|------|
| 1 | 5 端点 schema 后续漂移 | ADR-0007 不可逆；schema 变更走 ADR 修订流程 |
| 2 | 阶段 1 中间件方案还没设计 | 不在本轮范围；阶段 1 启动时按本节 §3.1 设计 |
| 3 | HolmesGPT 版本升级后 YAML 字段变化 | `09-AI辅助.md` §9.4 注释为示例；不绑定版本 |
| 4 | role=ai_toolset 未在 roles 表登记 | 阶段 2 部署前必须 INSERT（admin-bootstrap 脚本需更新） |
| 5 | R1 与 R2 协调（assets/{id} 含 related_lines） | R2 阶段 2 协调时补；本轮不阻塞 |

---

## 8. 交叉引用

- `docs/FIX-PLAN-R1-HOLMESGPT.md` — 本 IMPL 的依据
- `docs/adr/0007-r1-holmesgpt-toolset-边界.md` — 本 IMPL 的硬约束
- `09-AI辅助.md` §9.0-9.4 — 既有 v3 段落
- `intent-M35-R1.md` — intent-spec-author artifact
- ADR-0003 / ADR-0005 / ADR-0006 — 上位/相关决策
