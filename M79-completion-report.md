# M79 Completion Report — PM-direct Autonomous Loop

> **Loop cycle**: 9 of `itmanager-grit-2026q3`
> **Feat**: `0d5560b`
> **Intent**: `intent-M79.md` (in same commit)

## 摩擦

Poison 2026-09-17 verbatim: "最好还是有个循环，而不是在对话里等待".

PM-direct 自起 round 需要 Poison 在对话里触发. 8 cycle 实跑累计 (M69-M77) 都是 Poison
触发. 没有 idle → 没有 round.

## Poison 选 C (A+B 混合)

- **Mode A (默认)**: 每 10 min watchdog tick, 若有 idle slot → 写 `PM_NEXT_ROUND_REPORT.md`,
  Poison 看到 → 回 "go M{N}" / "stop" / "auto"
- **Mode B (auto)**: Poison 在 `PM_LOOP_MODE` 写 "B" → watchdog 自动 dispatch

## 改动 (config-only, ≤2h)

| 文件 | 改动 |
|---|---|
| `~/.hermes/scripts/pm-loop-watchdog.sh` | **替换** (6.5KB, 保留原 4 道防线 + 加 idle-slot 检测 + 模式 A/B 分支) |
| `~/.config/systemd/user/pm-loop-watchdog.service` | **新建** (oneshot service) |
| `~/.config/systemd/user/pm-loop-watchdog.timer` | **新建** (OnBootSec=2min, OnUnitActiveSec=10min) |
| `~/.hermes/state/PM_LOOP_MODE` | 新建 (默认 "A", Poison 控制切换) |

## Verify

- `systemctl --user daemon-reload` ✓
- `systemctl --user enable --now pm-loop-watchdog.timer` ✓
- `systemctl --user list-timers` 列出 pm-loop-watchdog (NEXT 10 min 后) ✓
- 手动跑一次: `MIN_GAP_MIN=0 bash watchdog` → 写 `PM_NEXT_ROUND_REPORT.md` (409 bytes, 含 idle slot G-4) ✓
- 自旋防: NOW_MIN=LAST_MIN 阻止同分钟重复起 ✓
- Commit age ≥ 10 min 才起下一 round ✓

## Poison 用法

```
# 看报告 (mode A)
cat ~/.hermes/state/PM_NEXT_ROUND_REPORT.md

# 切 mode B (自主 dispatch, 需 Poison 时段规则 omp 窗口)
echo B > ~/.hermes/state/PM_LOOP_MODE

# 切回 mode A
echo A > ~/.hermes/state/PM_LOOP_MODE

# 暂停
echo stop > ~/.hermes/state/PM_LOOP_MODE
```

## 决策点

- **D1**: 用 systemd --user timer 而非 cron (本机无 crontab binary) ✓
- **D2**: 自旋防 NOW_MIN=LAST_MIN ✓
- **D3**: commit age 10 min 防 race ✓
- **D4**: Mode B 简化 (不真正 omp dispatch, 只 inbox log) — 避免绕过 Poison 时段规则 advisory ✓
- **D5**: 保留原 watchdog 4 道防线 ✓
- **D6**: Poison stop gates 沿用 ✓

## 风险

- **Telegram CLI 不可用**: watchdog 报告写到本地文件, Poison 主动看 (沿用 PM-tick 受阻约束)
- **systemd user 守护**: 用户登出后可能停. 长跑需 `loginctl enable-linger`
- **Mode B 自旋防**: 简化实现只 inbox log, 真 dispatch 留给 PM-direct 在 Poison "auto" 触发时接
