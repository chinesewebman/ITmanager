// Alerts page：W1 去假数据兜底 + W2 时间格式化 + 空 data 守卫。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within, act } from "@testing-library/react";
// antd 的 message 在 src/test/setup.ts 里被全局 mock 成 vi.fn()，断言打在调用上
// （jsdom 下静态 message 不落 DOM，查文本必失败）。
import { message } from "antd";
import Alerts, { ticketResultMessage } from "./Alerts";

const h = vi.hoisted(() => ({
  override: {} as Record<string, unknown>,
  refetch: vi.fn(),
  // M14：共享 mutate spy —— 断言「确认后才调用」，不关心是哪个 mutation 的 mutate
  mutate: vi.fn(),
  mutateAsync: vi.fn(),
  // M3/P5：记录 queryKey，供分页/筛选断言
  lastKey: null as unknown,
  // D-3：每次 useApiMutation 的 (opts, mutate) 都留档。mock 的 mutate 不会真发请求，
  // 于是 onSuccess 的回调分支（created 分流）没有触发入口 —— 靠「哪个 mutate 被点了」
  // 反查它对应的 opts，再手工驱动。按顺序取 index 会因为新增 mutation 而悄悄错位，
  // 反查不会：点没点过是事实。
  mutations: [] as { mutate: ReturnType<typeof vi.fn>; opts: any }[],
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
  useApiMutation: (_fn: unknown, opts: unknown) => {
    // 每次调用一个独立 spy（都转发到共享的 h.mutate，既有断言不受影响），
    // 这样「哪一个 mutation 被触发」是可判定的。
    const mutate = vi.fn((...args: unknown[]) => h.mutate(...args))
    h.mutations.push({ mutate, opts })
    return { mutate, mutateAsync: h.mutateAsync, isPending: false }
  },
  queryKeys: { alerts: { list: (f?: Record<string, unknown>) => ["alerts", "list", f ?? {}] } },
}));

beforeEach(() => {
  h.override = {}
  h.refetch.mockClear()
  h.mutate.mockClear()
  h.lastKey = null
  h.mutations = []
  vi.mocked(message.success).mockClear()
  vi.mocked(message.info).mockClear()
  vi.mocked(message.error).mockClear()
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

  // ---- D-3 告警一键建单 ----

  // 入口在 getAlertActions（桌面表格 / 移动卡片共用），这里验证页面确实把 onClick
  // 接到了建单 mutation 上、且带的是该行的告警 id（接错行会建出别的告警的票）。
  it("D-3：点告警行「建单」调用建单 mutation，参数是该行告警 id", async () => {
    render(<Alerts />);
    // 「建单」精确匹配，不会命中「已建单」；两行都未建单，取第一行（id=1）
    fireEvent.click(screen.getAllByText("建单")[0]);
    await waitFor(() => {
      expect(h.mutate).toHaveBeenCalledWith("1");
    });
  });

  // R-3：created=false 是「这张告警早就有票」，不是失败。写成 error 会让运维以为建单挂了
  // 而反复重试 —— 断言用户真的看到 info 文案，而不是「建单失败」。
  it("D-3：created=false 提示「该告警已建单」，不是失败", async () => {
    render(<Alerts />);
    fireEvent.click(screen.getAllByText("建单")[0]);
    // 反查被触发的那个 mutation，手工驱动它的 onSuccess（mock 不会真发请求）
    const fired = h.mutations.find((m) => m.mutate.mock.calls.length > 0);
    expect(fired).toBeTruthy();
    await act(async () => {
      fired!.opts.onSuccess({
        created: false,
        ticket: { ticket_number: "TICKET-9" },
      });
    });
    expect(message.info).toHaveBeenCalledWith(
      expect.stringContaining("TICKET-9"),
    );
    // 关键：不是失败提示。说成 error 会让运维以为建单挂了而反复重试。
    expect(message.error).not.toHaveBeenCalled();
    expect(message.success).not.toHaveBeenCalled();
    expect(h.refetch).toHaveBeenCalled();
  });

  it("D-3：created=true 提示新建的工单号", async () => {
    render(<Alerts />);
    fireEvent.click(screen.getAllByText("建单")[0]);
    const fired = h.mutations.find((m) => m.mutate.mock.calls.length > 0);
    await act(async () => {
      fired!.opts.onSuccess({
        created: true,
        ticket: { ticket_number: "TICKET-10" },
      });
    });
    expect(message.success).toHaveBeenCalledWith(
      expect.stringContaining("TICKET-10"),
    );
    expect(message.info).not.toHaveBeenCalled();
    expect(message.error).not.toHaveBeenCalled();
  });
});

// 提示分流的纯逻辑。抽出来就是为了能这样直接钉住「已建单 ≠ 失败」。
describe("ticketResultMessage", () => {
  it("created=true → success，文案带工单号", () => {
    const m = ticketResultMessage(true, "TICKET-20260910-001");
    expect(m.level).toBe("success");
    expect(m.text).toContain("TICKET-20260910-001");
  });

  it("created=false → info（不是 error），且文案与新建可区分", () => {
    const m = ticketResultMessage(false, "TICKET-20260910-001");
    expect(m.level).toBe("info");
    expect(m.text).toContain("TICKET-20260910-001");
    // 两条文案必须不同：否则运维看不出「这次没新建」还是「又建了一张」
    expect(m.text).not.toBe(ticketResultMessage(true, "TICKET-20260910-001").text);
  });

  it("工单号缺失时不渲染出裸 undefined", () => {
    expect(ticketResultMessage(true).text).not.toContain("undefined");
    expect(ticketResultMessage(false).text).not.toContain("undefined");
  });
});
