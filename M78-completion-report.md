# M78 Completion Report — G-15 release 校验解耦

> **Loop cycle**: 10 of `itmanager-grit-2026q3`
> **Feat**: `b5e1549`
> **Intent**: `intent-M78.md` (in same commit)

## 摩擦

`Config.Validate()` 在 release 下**无条件**要求 `integrations.netbox.token` 与 `glpi.*_token`,
而 zabbix 已是「URL 配置了才校验」模式. compose 默认 `debug` 才能起 — debug 下登录 cookie
不带 `Secure`、弱凭据不拒. 安全审计长期 R-5 待解.

## 改动 (backend + compose + docs, ≤2h)

| 文件 | 改动 |
|---|---|
| `backend/internal/config/config.go` | Validate: netbox / glpi 改 URL-aware (与 zabbix 一致); 新增 `isPlaceholderToken` helper |
| `backend/internal/config/config_test.go` | 4 新 cases (URL 空 + token 空 通过 / URL 配 + 占位 token 报错 / GLPI 镜像 / 既有 3 case 加 URL) |
| `docker-compose.yml` api 服务 | `NMP_SERVER_MODE=${NMP_SERVER_MODE:-release}` (默认 release) |
| `08-部署运维.md` §8.3.2 | 同步 default release + 占位值拒启说明 |

## Verify

- `go build ./...` 0 err ✓
- `go test -count=1 ./...` **27 packages 全绿** ✓
- TestValidate_ReleaseMode 11 cases 全 PASS ✓
- **mutation inversion 5 red**:
  - revert netbox URL guard → NetboxURL_Empty_NoTokenRequired FAIL ✓
  - revert glpi URL guard → GLPIURL_Empty_NoTokenRequired FAIL ✓
  - revert isPlaceholderToken → NetboxPlaceholder + GLPIPlaceholder FAIL (2 红) ✓

## 决策点

- **D1**: netbox + glpi 同步改 URL-aware, 与 zabbix 模板一致 ✓
- **D2**: 占位值检查加进 URL 守卫内 ✓
- **D3**: compose 默认翻 release ✓
- **D4**: 不引新 config key ✓
- **D5**: 现有 release-mode 测试保留 + 4 新 cases ✓

## Poison "auto 切换" 实证

Poison 2026-09-17 verbatim: "auto 切换是吧，做吧" — PM_LOOP_MODE=B 切换生效,
watchdog 自主 dispatch 路径激活, 本 round 即 watchdog 模式 B 实证.
