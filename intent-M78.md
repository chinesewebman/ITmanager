# M78 — G-15 release 校验解耦（OMH ulw-loop 第 10 cycle）

> **Loop cycle**: 10 of `itmanager-grit-2026q3`

## Goal

`Config.Validate()` 在 release 下**无条件**要求 `integrations.netbox.token` 与 `glpi.*_token`,
而 zabbix 已是「URL 配置了才校验」模式. 解耦后 compose 默认模式可翻 `release` (G-15 完成).

## Non-goals

- 不改 zabbix 校验 (已是 URL 配置了才校验, 是参考模板)
- 不动 server.trusted_proxies / auth / database 校验
- 不改 compose aux profile (netbox / glpi / zabbix 容器本身的占位凭据仍是 G-17 范畴)
- 不改 API Key / JWT secret 校验
- 不引新的 config key

## Assumptions

- zabbix 现行模式是参考: `if URL != "" { 校验 }`
- netbox + glpi 同步改 URL-aware 校验
- compose 当前默认 `NMP_SERVER_MODE=${NMP_SERVER_MODE:-debug}` (原因: debug 是 release 拒启时的 fallback). M78 完成后翻 `release`
- 占位值检查 (nmp123 / your-jwt) 保留, 但只在 URL 配置后才检查
- 测试用例要拆: 「URL 空 + token 空」通过 (集成未启用), 「URL 配 + token 空」报缺, 「URL 配 + 占位 token」报错

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| `config.Validate()` netbox token 检查外加 `if URL != ""` | grep verify |
| `config.Validate()` glpi tokens 检查外加 `if URL != ""` | grep verify |
| `docker-compose.yml` api 服务 `NMP_SERVER_MODE=${NMP_SERVER_MODE:-release}` | grep verify |
| `08-部署运维.md` §8.3.2 同步 default release | section patch |
| `config_test.go` 新增 4 cases: URL 空 + token 空 通过 / URL 配 + 占位 token 报错 | mutation inversion |
| 现有 release-mode 集成测试不破 | go test |
| `go test -count=1 ./...` 全绿 | verify |
| mutation inversion red ≥ 3 | verify |
| fact_store fact_id=18 | shipped |

## Verification

- `go build ./...` 0 err
- `go test -count=1 ./...` 全绿 (27 packages)
- mutation inversion: revert URL guard → 至少 3 tests FAIL
- `docker compose config` 仍解析

## Risks

- **release 模式翻默认后, 现有部署若没配 netbox URL 会启动失败**: 这是**预期行为** — release 模式就是要强制集成要么真配、要么 URL 显式留空 (集成可选). 文档同步写明
- **G-7 静态 IP + auth secret 默认**: M78 不动这些, 不引入新校验失败
- **G-13 viper AllKeys**: M78 不引新键, 不触发 G-13 坑

## Plan

1. `config.go` Validate: netbox + glpi 加 `if URL != ""` 守卫
2. `config.go` 加占位值检查 (URL 配 + token 是 "your-token-here" / "change-in-production" 类占位 → 报错)
3. `config_test.go` 新增 4 cases (URL 空 + token 空 通过 / URL 配 + token 空 报错 / URL 配 + 占位 token 报错 / URL 配 + 真 token 通过)
4. `docker-compose.yml` api 服务 `NMP_SERVER_MODE=${NMP_SERVER_MODE:-release}` (默认 release)
5. `08-部署运维.md` §8.3.2 同步
6. fact_store + docs commit

## Decision gate

- **D1**: netbox + glpi 同步改 URL-aware, 与 zabbix 模板一致 ✓
- **D2**: 占位值检查加进 URL 守卫内 (集成启用才校验占位) ✓
- **D3**: compose 默认翻 release (M78 完成前是 debug) ✓
- **D4**: 不引新 config key (避免 G-13 坑) ✓
- **D5**: 现有 release-mode 测试保留 + 新增 4 cases ✓
