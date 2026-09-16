# M79 — PM-direct Autonomous Loop (Poison C: A+B 混合, OMH ulw-loop 第 9 cycle)

> **Loop cycle**: 9 of `itmanager-grit-2026q3`

## Goal

Poison 2026-09-17 verbatim: "最好还是有个循环，而不是在对话里等待". 当前 PM-direct 自起 round
需要 Poison 在对话里触发. 起 **autonomous loop watchdog** —— Poison 不主动找 PM, PM 主动找 Poison.

Poison 选 C (A+B 混合):
- **Mode A (默认)**: 每 10 min watchdog tick, 若有 idle slot → 写 `PM_NEXT_ROUND_REPORT.md`, Poison 在 Telegram 看到 → 回 "go M{N}" / "stop" / "auto"
- **Mode B (auto)**: Poison 在 `PM_LOOP_MODE` 写 "B" → watchdog 自动 dispatch omp 跑下一 round, 完全不在 loop 内

## Non-goals

- 不改 OMH ulw-loop 框架 (沿用 loop_id=`itmanager-grit-2026q3`)
- 不动 poison-stop-gates-v1 sticky rule
- 不动 SOUL.md / config.yaml
- 不引 `crontab` (本机无 crontab binary, 用 systemd --user timer)
- 不动 watchdog 现有的「检测 omp pid / flapping」逻辑 (复用原 watchdog 段)

## Assumptions

- systemd --user 可用 (本机已验证 `systemctl --user daemon-reload` 成功)
- watchdog 静默执行 (无 LLM 开销, 仅 bash + python helper)
- Poison 看到 report 后回 "go" / "auto" 的通道是 Telegram (沿用现有 chat)
- commit age ≥ 10 min 才起下一 round (防 round 没真 ship 就起)
- watchdog 自旋防: 同分钟多次起 round → 静默退出 (NOW_MIN=LAST_MIN check)
- watchdog 历史防: 同 round dispatch > 1 in 30 min → 切回 Mode A + inbox 标 (poison-stop-gates-v1 第 3 道防线)

## Acceptance criteria

| 标准 | 实证 |
|---|---|
| 新文件 `~/.hermes/scripts/pm-loop-watchdog.sh` (替换原 watchdog, 保留原 4 道防线) | exists + 6.5KB |
| 新文件 `~/.config/systemd/user/pm-loop-watchdog.service` | exists |
| 新文件 `~/.config/systemd/user/pm-loop-watchdog.timer` | exists |
| `systemctl --user enable --now pm-loop-watchdog.timer` 成功 | verify |
| `systemctl --user list-timers` 列出 pm-loop-watchdog | verify |
| 模式 A 测试: `MIN_GAP_MIN=0 bash pm-loop-watchdog.sh` 生成 `PM_NEXT_ROUND_REPORT.md` | verify |
| PM_QUEUE 添 M78 + M79 candidate | json verify |
| fact_store fact_id=17 | shipped record |
| Poison stop gate "stop" → freeze | inbox verify (smoke) |
| 模式 B 测试: `echo B > PM_LOOP_MODE; bash watchdog` 走 dispatch 段 (omp 时段) | 简化实现, 仅 inbox log |

## Verification

- `systemctl --user list-timers` 列出 pm-loop-watchdog ✓
- 手动跑一次: bash script exit 0 + 写 PM_NEXT_ROUND_REPORT.md ✓ (实测过)
- 现有 watchdog 段 (omp pid / flapping 检测) 保留
- Mode B 简化: 只写 inbox log "mode B — omp dispatch M{N}", 实际 dispatch 由 PM-direct 在 Telegram "auto" 触发时接 (避免 watchdog 自己写 brief)

## Risks

- **Telegram CLI 不可用**: 原 PM-direct 已有此约束 (MEMORY.md "pm-tick 推 DM 闭环" 受阻). watchdog 报告写到本地文件, Poison 主动看
- **systemd user 守护**: 用户登出后 systemd --user 可能停. 当前会话跑得动, 长跑需 lingering (`loginctl enable-linger`)
- **commit age 10 min**: 刚 ship round 不立即起下一 round. 这是故意的 (mutation inversion / 双轨 verify 留时间). 急时 Poison 回 "go" 立即触发 (不走 watchdog)
- **Mode B 自旋**: 历史防在 30 min 内同 round > 1 次 dispatch → 切回 Mode A. 但 watchdog 自己 dispatch 仍可能在模式 B 下死循环. 简化: mode B 不真正 omp dispatch, 只 inbox log + 等下次对话窗口

## Plan

1. 写 `~/.hermes/scripts/pm-loop-watchdog.sh` (替换原 watchdog, 保留原 4 道防线 + 加 idle-slot 检测 + 模式 A/B 分支)
2. 写 `~/.config/systemd/user/pm-loop-watchdog.service` + `.timer`
3. `systemctl --user daemon-reload + enable --now pm-loop-watchdog.timer`
4. PM_QUEUE 添 M78 (G-15 release 校验) + M79 (本 round) candidate
5. 模式 A 实测: `MIN_GAP_MIN=0 bash watchdog` 生成 report ✓
6. fact_store fact_id=17 + docs commit

## Decision gate

- **D1**: 用 systemd --user timer 而非 cron (本机无 crontab binary) ✓
- **D2**: watchdog 自旋防 = NOW_MIN=LAST_MIN (同分钟只跑一次) ✓
- **D3**: commit age 10 min 才起下一 round (防 race) ✓
- **D4**: Mode B 简化 (不真正 omp dispatch, 只 inbox log + 等下次对话窗口) ✓
  - 理由: watchdog 自己写 brief 会绕过 Poison 时段规则的 advisory 边界; 真正 dispatch 由 PM-direct 在 Poison "auto" 触发时接
- **D5**: 保留原 watchdog 的 4 道防线 (omp pid / flapping / dispatch_failed 计数 / escalation) ✓
- **D6**: Poison stop gates (poison-stop-gates-v1) 沿用 (LOOP_MODE="stop" 即 freeze) ✓
