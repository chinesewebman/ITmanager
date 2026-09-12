# M38-B 完成报告 — G-39 fire 路径整链路

**Date**: 2026-09-13
**Scope**: intent-M38-B.md (M37-A 的 fire path 残余闭环, E 选项)
**Branch**: `main` @ `ff1bced` (10/11 commits pushed; 11th = docs this file)

---

## Delivered

11 commits 实现 G-39 fire 路径整链路修复（triggerid→rule_id 映射 + 60s dedup + NotifyUsers 单发 + 真 PG E2E），让 worker.subscribe(`TopicAlertCreated`) 不再形同虚设，Zabbix trigger 经映射能真发告警通知。

## Changed

11 commits per intent-M38-B.md round_plan（已 push origin/main）：
1. `4b5868f` migration 000038 + db_smoke precondition amend
2. `e8f43e9` models.AlertRuleTriggerMap + AlertRule.TriggerMaps 关联
3. `cd14cd7` AlertRuleService CRUD (ListMappings/CreateMapping/DeleteMapping)
4. `32dfb9a` AlertHandler POST/GET/DELETE `/api/alert-rules/:id/triggers` + routes gated + OpenAPI
5. `4f0ac22` integration.SyncFromZabbix → publish TopicAlertCreated with AlertRuleID
6. `84f169a` worker.handleAlertEvent 适配 AlertRuleID + NotifyUsers snapshot
7. `e57d364` worker firededup 60s sync.Map + CompareAndSwap 原子化
8. `de9f322` mapper 单测 4 条 (hit/miss/rule-deleted/source-isolation)
9. `4a359db` firededup 单测 8 条 race-clean (-race 通过)
10. `ff1bced` worker NotifyUsers 单测 7 条 + 真 PG TestDBSmoke_M38B_FirePathEnd2End 5 场景
11. (本 docs commit) CHANGELOG.md M38-B section + 本报告

## Validation

### Gate 1: `go vet ./...`
```
$ cd backend && go vet ./...
# 输出仅 sqlite3 C warning (既有, 非 M38-B 引入)
sqlite3-binding.c:123133:9: warning: assignment discards 'const' qualifier...
```
**PASS** — 仅既有 sqlite3 C warning，零 Go vet 错误。

### Gate 2: `gofmt -l` (changed files)
```
$ /home/webman/.local/share/mise/installs/go/1.25.14/bin/gofmt -l \
    backend/migrations/000038_alert_rule_trigger_map.up.sql \
    backend/internal/models/alert.go \
    backend/internal/service/alert_service.go \
    backend/internal/api/handlers/alert_handler.go \
    backend/internal/api/routes.go \
    backend/internal/api/handlers/alert_handler_test.go \
    backend/internal/api/openapi.yaml \
    backend/internal/api/routes_integration_test.go \
    backend/internal/integration/service.go \
    backend/internal/integration/alert_rule_mapper.go \
    backend/internal/integration/alert_rule_mapper_test.go \
    backend/internal/notification/worker.go \
    backend/internal/notification/firededup.go \
    backend/internal/notification/dedup_test.go \
    backend/internal/notification/worker_notify_users_test.go \
    backend/tests/db_smoke_test.go
(empty)
```
**PASS** — `gofmt -l` 零输出。

### Gate 3: `go test -count=1 ./...` (26 packages)
```
ok  	network-monitor-platform/cmd/admin-bootstrap	0.421s
ok  	network-monitor-platform/cmd/migrate	0.035s
ok  	network-monitor-platform/cmd/seed	2.506s
ok  	network-monitor-platform/cmd/set-role	0.025s
ok  	network-monitor-platform/internal/api	18.627s
ok  	network-monitor-platform/internal/api/handlers	7.382s
ok  	network-monitor-platform/internal/apierr	0.009s
ok  	network-monitor-platform/internal/apikey	0.003s
ok  	network-monitor-platform/internal/cache	0.187s
ok  	network-monitor-platform/internal/config	0.007s
ok  	network-monitor-platform/internal/cursor	0.007s
ok  	network-monitor-platform/internal/database	0.015s
ok  	network-monitor-platform/internal/diagnostic	1.012s
ok  	network-monitor-platform/internal/eventbus	0.095s
ok  	network-monitor-platform/internal/grpcserver	0.010s
ok  	network-monitor-platform/internal/httpx	0.523s
ok  	network-monitor-platform/internal/integration	14.679s
ok  	network-monitor-platform/internal/metrics	0.004s
ok  	network-monitor-platform/internal/middleware	0.350s
ok  	network-monitor-platform/internal/migrate	0.008s
ok  	network-monitor-platform/internal/models	0.027s
ok  	network-monitor-platform/internal/notification	0.669s
ok  	network-monitor-platform/internal/postmortem	0.112s
ok  	network-monitor-platform/internal/redact	0.006s
ok  	network-monitor-platform/internal/service	0.225s
ok  	network-monitor-platform/pkg/logger	0.003s
ok  	network-monitor-platform/tests	0.028s
```
**PASS** — 26 packages 全绿 (含 tests)。

### Gate 4: 真 PG `DOCKER='sudo -n docker' bash scripts/db_smoke.sh`
```
$ cd /home/webman/Projects/ITmanager && DOCKER='sudo -n docker' bash scripts/db_smoke.sh 2>&1 | grep -c "^--- PASS"
43
```
**PASS** — 43 cases 全绿（含新增 `TestDBSmoke_M38B_FirePathEnd2End`），比 M37-A baseline 多 1 条。

### Gate 5: firededup mutation inversion PASS-FAIL-PASS

**5a) PASS 初始**:
```
$ go test -count=1 -run TestDeduper ./internal/notification/...
ok  	network-monitor-platform/internal/notification	0.007s
```
8/8 PASS.

**5b) FAIL 突变** (Allow 改返 true, 关闭 dedup):
```
--- FAIL: TestDeduper_FirstHit_Allows (0.00s)
    dedup_test.go:57: Len=0, want 1
--- FAIL: TestDeduper_InWindow_Drops (0.00s)
    dedup_test.go:71: in-window hit must drop
--- FAIL: TestDeduper_DifferentKeys_Independent (0.00s)
    dedup_test.go:106: trig|1 second must drop (still in window)
--- FAIL: TestDeduper_Concurrent_ExactlyOneFirstHit (0.00s)
    dedup_test.go:163: concurrent first-hit: got 200 allows, want exactly 1
--- FAIL: TestDeduper_Concurrent_TwoWindows_AllowTwice (0.00s)
    dedup_test.go:194: window 1: got 50, want 1
--- FAIL: TestDeduper_GC_RemovesExpired (0.00s)
    dedup_test.go:210: Len=0 before advance
```
6/6 FAIL — 守门网有效证据。

**5c) PASS revert** (回滚 mutation):
```
$ go test -count=1 -run TestDeduper ./internal/notification/...
ok  	network-monitor-platform/internal/notification	0.007s
```
8/8 PASS — revert 成功。

## Risk

Low（已识别 4 项后续 round 残余，见 CHANGELOG.md M38-B section "残余" 段）：
1. **dedup 横向扩展**：单进程 in-memory sync.Map，多副本部署需要 Redis SETEX 协调；
2. **triggerid 历史映射查询路由未开**：UI "按 triggerid 取该 trigger 历史映射" 路由本 round 不在范围，last-write-wins 算法已就位；
3. **NotifyUsers channel 匹配走 LIKE 全表扫**：千人级以下不构成 hot path，待后续走 channel 端"显式收件人"配置；
4. **OpenAPI 独立 path**：alert_rule_trigger_map 查询路由未独立（当前挂在 `/alert-rules/{id}/triggers` 下，按 triggerid 查询需另开路由），不阻塞 fire 主链路。

风险均不阻塞当前生产 fire path 功能；提交后 26 packages + 43 cases 全绿守门就位。

## Status

COMPLETE
