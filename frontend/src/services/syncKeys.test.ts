// G-41/M81: 跨语言键名常量契约测试 (前端侧)。
//
// 历史背景: 后端 integration/service.go 的 results = map[string]int{"zabbix_truncated": truncated}
// 与前端 Settings.tsx 的 res?.data?.data?.synced?.zabbix_truncated ?? 0 各写一遍字面量 —
// 改名/拼写漂移会静默失效: 前端读不到 → ?? 0 兜底 → UI 永远显示「未截断」,
// 运维错过 6000-1=5999 条丢告警的真相。
//
// 本测试守住:
//   1. SYNC_KEY_ZABBIX_TRUNCATED 常量值不变
//   2. Settings.tsx 用常量引用, 不写回裸字符串 (grep guard)
//   3. TS 常量值与 Go 端 KeyZabbixTruncated 一致 (跨语言漂移守卫)
//
// 工作目录: frontend/, 跨仓走相对路径 ../../../backend/internal/integration/sync_keys.go

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { SYNC_KEY_ZABBIX_TRUNCATED } from "./syncKeys";

describe("syncKeys / G-41 跨语言键名契约", () => {
  it("SYNC_KEY_ZABBIX_TRUNCATED 值等于 'zabbix_truncated'", () => {
    expect(SYNC_KEY_ZABBIX_TRUNCATED).toBe("zabbix_truncated");
  });

  it("Settings.tsx 用 SYNC_KEY_ZABBIX_TRUNCATED 常量, 不写回裸字符串", () => {
    // 工作目录: vitest 默认 = frontend/, 所以走 ../src/pages/Settings.tsx
    const settingsPath = resolve(__dirname, "../pages/Settings.tsx");
    const src = readFileSync(settingsPath, "utf8");

    // Settings.tsx 必须 import SYNC_KEY_ZABBIX_TRUNCATED
    expect(src).toMatch(
      /import\s*\{[^}]*SYNC_KEY_ZABBIX_TRUNCATED[^}]*\}\s*from\s*['"]\.\.\/services\/syncKeys['"]/,
      "Settings.tsx 必须从 ../services/syncKeys import SYNC_KEY_ZABBIX_TRUNCATED (G-41/M81)",
    );

    // Settings.tsx 在 production 代码路径必须用 SYNC_KEY_ZABBIX_TRUNCATED 引用 key
    // (允许注释里出现裸字符串 — 不算 production 路径)
    expect(src).toMatch(
      /\[\s*SYNC_KEY_ZABBIX_TRUNCATED\s*\]/,
      "Settings.tsx 必须用 [SYNC_KEY_ZABBIX_TRUNCATED] 访问 data.synced 键 (G-41/M81)",
    );

    // 反证: 排除「裸字符串 .zabbix_truncated」在 production 路径
    // 抓形如 `.zabbix_truncated` 或 ['zabbix_truncated'] / ["zabbix_truncated"] 等 production 访问形态
    // 注释里有 `zabbix_truncated` 字面量是 OK 的 (讲解语义), 不算 production 路径
    const bareAccessPatterns = [
      /\?\.\s*['"]zabbix_truncated['"]/, // ?.'zabbix_truncated'
      /\?\.\['"]zabbix_truncated['"]\]/, // ?.['zabbix_truncated']
      /\[['"]zabbix_truncated['"]\]/, // ['zabbix_truncated']
    ];
    for (const re of bareAccessPatterns) {
      expect(src).not.toMatch(re);
    }
  });

  it("与后端 Go 常量 KeyZabbixTruncated 一致 (跨语言漂移守卫)", () => {
    // 跨仓路径: frontend/ → ../../../backend/internal/integration/sync_keys.go
    const goPath = resolve(
      __dirname,
      "../../../backend/internal/integration/sync_keys.go",
    );
    const goSrc = readFileSync(goPath, "utf8");

    // 匹配: KeyZabbixTruncated = "zabbix_truncated"
    // 对空白宽容 (等号前后空格 / tab)
    const match = goSrc.match(/KeyZabbixTruncated\s*=\s*"([^"]+)"/);
    expect(match, "后端 sync_keys.go 找不到 KeyZabbixTruncated 常量声明").not.toBeNull();

    const goValue = match![1];
    expect(SYNC_KEY_ZABBIX_TRUNCATED).toBe(
      goValue,
      `TS SYNC_KEY_ZABBIX_TRUNCATED (${SYNC_KEY_ZABBIX_TRUNCATED}) 与 Go KeyZabbixTruncated (${goValue}) 必须一致 — 跨语言契约`,
    );
  });
});
