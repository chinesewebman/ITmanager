// Alerts page：W1 去假数据兜底 + W2 时间格式化 + 空 data 守卫。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import Alerts from "./Alerts";

const h = vi.hoisted(() => ({
  override: {} as Record<string, unknown>,
  refetch: vi.fn(),
  // M14：共享 mutate spy —— 断言「确认后才调用」，不关心是哪个 mutation 的 mutate
  mutate: vi.fn(),
  mutateAsync: vi.fn(),
  // M3/P5：记录 queryKey，供分页/筛选断言
  lastKey: null as unknown,
}))

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
// Alerts 的 useApiQuery 返回 {items, stats, total}。
// total=100 是「过滤后总数」（分页器「共 X 条」），stats.total=15 是「全表总数」（统计卡）——语义分离（M3/P5 方案 B）。
const mockResp = {
  items: mockAlerts,
  stats: { total: 15, problem: 8, acknowledged: 3, resolved: 4 },
  total: 100,
};

vi.mock("../hooks/useApiQuery", () => ({
  useApiQuery: (key: unknown) => {
    h.lastKey = key
    return {
      data: mockResp,
      isLoading: false,
      isError: false,
      error: undefined,
      refetch: h.refetch,
      ...h.override,
    }
  },
  useApiMutation: () => ({ mutate: h.mutate, mutateAsync: h.mutateAsync, isPending: false }),
  queryKeys: { alerts: { list: (f?: Record<string, unknown>) => ["alerts", "list", f ?? {}] } },
}));

beforeEach(() => {
  h.override = {}
  h.refetch.mockClear()
  h.mutate.mockClear()
  h.lastKey = null
})

describe("Alerts page", () => {
  it("渲染告警页 + 表格（mock 数据）", () => {
    render(<Alerts />);
    expect(screen.getByText("告警中心")).toBeInTheDocument();
    // AlertTable 显示 mock 告警 host
    expect(screen.getByText("web-server-01")).toBeInTheDocument();
    expect(screen.getByText("db-server-02")).toBeInTheDocument();
  });

  it("W2：触发时间渲染成 YYYY-MM-DD HH:mm:ss，而非原始串", () => {
    render(<Alerts />);
    expect(screen.getByText("2026-02-14 10:00:00")).toBeInTheDocument();
    expect(screen.getByText("2026-02-14 09:30:00")).toBeInTheDocument();
  });

  // M2：表格排序——此前全站零 sorter，用户无法点击表头排序。
  // AlertTable 给主机/级别/状态/触发时间加前端本地排序；这里验证最核心的时间排序行为。
  it("M2：触发时间列可排序（点击表头后按时间升序重排）", async () => {
    const { container } = render(<Alerts />);
    // 每行取整行 textContent（第一列是 rowSelection 选择框，不能取 td[0]）
    const rowTexts = () =>
      Array.from(container.querySelectorAll("tbody tr[data-row-key]")).map(
        (r) => r.textContent ?? "",
      );

    // 初始顺序 = dataSource 顺序：web-server-01（10:00）在前
    expect(rowTexts()[0]).toContain("web-server-01");

    // 点击「触发时间」表头，antd 默认第一次点击为升序 → 09:30 的 db-server-02 排前
    // （scroll+fixed 列导致 header 渲染两份 title span，取第一个）
    fireEvent.click(screen.getAllByText("触发时间")[0]);
    await waitFor(() => {
      expect(rowTexts()[0]).toContain("db-server-02");
    });
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

  it("W1：接口失败时显示错误态 + 重试，不回落假告警", () => {
    h.override = { data: undefined, isError: true, error: { response: { status: 500 } } }
    render(<Alerts />);
    expect(screen.getByText("数据加载失败")).toBeInTheDocument()
    expect(screen.queryByText("web-server-01")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: /重\s*试/ }))
    expect(h.refetch).toHaveBeenCalled()
  });

  it("W1：200 + 空 data（stats 为 null）不白屏，统计卡显示 0", () => {
    h.override = { data: { items: [], stats: null } }
    render(<Alerts />);
    expect(screen.getByText("暂无告警")).toBeInTheDocument()
    // 统计卡四联全部回落 0（不是虚构的 15/8/3/4）
    expect(screen.queryByText("15")).toBeNull()
    expect(screen.getAllByText("0").length).toBe(4)
  });

  it("M14：批量确认先弹确认框，确认后才执行（误点不生效）", async () => {
    render(<Alerts />);
    // 选中第一行告警（checkbox[0] 是表头全选，[1] 才是第一行）
    const checkboxes = screen.getAllByRole("checkbox");
    fireEvent.click(checkboxes[1]);

    // 选中后批量按钮出现，点「批量确认」
    fireEvent.click(await screen.findByRole("button", { name: /批量确认/ }));

    // 确认框出现，但 mutate 尚未被调用（误点不生效）
    const title = await screen.findByText(/批量确认已选的 1 条告警/);
    expect(title).toBeInTheDocument();
    expect(h.mutate).not.toHaveBeenCalled();

    // 点 Popconfirm 里的「确认」按钮 → 才真正执行
    const popover = title.closest(".ant-popover") as HTMLElement;
    fireEvent.click(within(popover).getByRole("button", { name: /确\s*认/ }));

    await waitFor(() => {
      expect(h.mutate).toHaveBeenCalledWith(["1"]);
    });
  });

  // M3/P5：服务端分页——分页器显示过滤后 total（100），而非当前页 items.length（2）。
  it("M3/P5：分页器显示服务端 total（共 100 条），而非当前页条数", () => {
    render(<Alerts />);
    // mockResp total=100 / items=2 → 分页器「共 100 条」，不是「共 2 条」
    expect(screen.getByText("共 100 条")).toBeInTheDocument();
    expect(screen.queryByText("共 2 条")).toBeNull();
  });

  // M3/P5：翻页更新 queryKey 的 page，筛选变化重置回第 1 页。
  it("M3/P5：翻页更新 queryKey 的 page，筛选变化重置回第 1 页", async () => {
    const { container } = render(<Alerts />);
    // 初始 queryKey 含 page:1/pageSize:20
    expect((h.lastKey as any)[2]).toMatchObject({ page: 1, pageSize: 20 });

    // 点「下一页」→ onPageChange(2, 20) → setPage(2) → queryKey page 变 2
    const next = container.querySelector(".ant-pagination-next");
    expect(next).toBeTruthy();
    fireEvent.click(next as Element);
    await waitFor(() => {
      expect((h.lastKey as any)[2]).toMatchObject({ page: 2 });
    });

    // 切状态筛选 → 重置 page 回 1，status 下沉进 queryKey
    fireEvent.mouseDown(screen.getAllByRole("combobox")[0]);
    fireEvent.click(await screen.findByTitle("已解决"));
    await waitFor(() => {
      expect((h.lastKey as any)[2]).toMatchObject({ status: "resolved", page: 1 });
    });
  });
});
