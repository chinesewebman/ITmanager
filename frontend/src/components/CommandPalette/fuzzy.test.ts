import { describe, it, expect } from "vitest";
import { fuzzySearch } from "./fuzzy";

interface Item {
  name: string;
}

describe("fuzzySearch", () => {
  const items: Item[] = [
    { name: "web-server-01" },
    { name: "web-server-02" },
    { name: "db-server-01" },
    { name: "switch-core-01" },
    { name: "firewall-main" },
  ];

  it("空 query 返回所有 items score=0", () => {
    const r = fuzzySearch("", items, (x) => x.name);
    expect(r).toHaveLength(5);
    expect(r.every((x) => x.score === 0)).toBe(true);
  });

  it("完整包含 query 命中（大小写不敏感）", () => {
    const r = fuzzySearch("web", items, (x) => x.name);
    expect(r).toHaveLength(2);
    expect(r[0].item.name).toBe("web-server-01");
    expect(r[0].score).toBeGreaterThan(50);
  });

  it("起始包含 score 高于中间包含", () => {
    const r1 = fuzzySearch(
      "web",
      [{ name: "web-server" }, { name: "something-web" }],
      (x) => x.name,
    );
    expect(r1[0].item.name).toBe("web-server");
    expect(r1[0].score).toBeGreaterThan(r1[1].score);
  });

  it('子序列匹配：query="wb" 命中 "web-server"', () => {
    const r = fuzzySearch("wb", items, (x) => x.name);
    expect(r.some((x) => x.item.name === "web-server-01")).toBe(true);
  });

  it("完全不匹配返回空", () => {
    const r = fuzzySearch("xyz123", items, (x) => x.name);
    expect(r).toHaveLength(0);
  });

  it("结果按 score 降序", () => {
    const r = fuzzySearch("server", items, (x) => x.name);
    // server 出现在 web-server/db-server 里，全部完整包含
    // 起始包含 server 的（如 "server-01"）应排前
    expect(r.length).toBeGreaterThan(0);
    for (let i = 1; i < r.length; i++) {
      expect(r[i - 1].score).toBeGreaterThanOrEqual(r[i].score);
    }
  });
});
