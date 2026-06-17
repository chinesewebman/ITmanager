// Alerts page smoke test
import '@testing-library/jest-dom'
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import Alerts from "./Alerts";

const mockAlerts = [
  {
    id: "1",
    host: "web-server-01",
    message: "CPU使用率超过90%",
    severity: 5,
    severity_name: "灾难",
    status: "problem",
    created_at: "2026-02-14 10:00:00",
  },
  {
    id: "2",
    host: "db-server-02",
    message: "磁盘空间不足",
    severity: 4,
    severity_name: "严重",
    status: "problem",
    created_at: "2026-02-14 09:30:00",
  },
];
// Alerts 的 useApiQuery 返回 {items, stats}（page 自己在 fetcher 里 wrap）
const mockResp = {
  items: mockAlerts,
  stats: { total: 15, problem: 8, acknowledged: 3, resolved: 4 },
};

vi.mock("../hooks/useApiQuery", () => ({
  useApiQuery: () => ({ data: mockResp, isLoading: false, refetch: vi.fn() }),
  useApiMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
  queryKeys: { alerts: { list: () => ["alerts", "list"] } },
}));

describe("Alerts page", () => {
  it("渲染告警页 + 表格（mock 数据）", () => {
    render(<Alerts />);
    expect(screen.getByText("告警中心")).toBeInTheDocument();
    // AlertTable 显示 mock 告警 host
    expect(screen.getByText("web-server-01")).toBeInTheDocument();
    expect(screen.getByText("db-server-02")).toBeInTheDocument();
  });

  it("不 crash 渲染", () => {
    expect(() => render(<Alerts />)).not.toThrow();
  });

  it("小改进 #2：显示「导出训练集」按钮", () => {
    render(<Alerts />);
    expect(screen.getByText("导出训练集")).toBeInTheDocument();
  });

  it("小改进 #2：告警行显示「标记误报」按钮（未标记时）", () => {
    render(<Alerts />);
    // mock 数据中 is_false_positive 未设置 → 显示「标记误报」按钮
    const buttons = screen.getAllByText("标记误报");
    expect(buttons.length).toBeGreaterThanOrEqual(1);
  });
});
