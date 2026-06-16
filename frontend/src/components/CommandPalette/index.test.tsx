import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CommandPalette } from "./index";

// 共享 QueryClient 实例（避免每个 test 重建）
const testQueryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: 0, gcTime: 0 },
  },
});

// 小改进 #3 review 修复 #4：spy useNavigate 验证 navigate 调用
const navigateMock = vi.fn();
vi.mock("react-router-dom", async () => {
  const actual: any = await vi.importActual("react-router-dom");
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

// mock 3 个 API
const mockAssets = {
  data: {
    data: {
      items: [
        { id: "a1", name: "web-server-01", asset_type: "server" },
        { id: "a2", name: "db-server-01", asset_type: "server" },
      ],
    },
  },
};
const mockAlerts = {
  data: {
    data: {
      items: [
        { id: "al1", host: "switch-core-01", severity_name: "严重" },
        { id: "al2", host: "web-01", severity_name: "一般" },
      ],
    },
  },
};
const mockTickets = {
  data: {
    data: {
      items: [{ id: "t1", title: "服务器CPU高", status: "open" }],
    },
  },
};

vi.mock("../../services/api", () => ({
  assetApi: { list: vi.fn(() => Promise.resolve(mockAssets)) },
  alertApi: { list: vi.fn(() => Promise.resolve(mockAlerts)) },
  ticketApi: { list: vi.fn(() => Promise.resolve(mockTickets)) },
}));

const renderPalette = () =>
  render(
    <ConfigProvider>
      <QueryClientProvider client={testQueryClient}>
        <MemoryRouter>
          <CommandPalette />
        </MemoryRouter>
      </QueryClientProvider>
    </ConfigProvider>,
  );

describe("CommandPalette", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("初始不渲染 modal（Cmd+K 才开）", () => {
    renderPalette();
    // placeholder 不应在 Document 可见
    expect(screen.queryByPlaceholderText(/搜索/)).toBeNull();
  });

  it("Cmd+K 打开 modal + 显示 3 资源搜索项", async () => {
    renderPalette();
    // 模拟 Mac 平台
    Object.defineProperty(navigator, "platform", {
      value: "MacIntel",
      configurable: true,
    });

    fireEvent.keyDown(document, { key: "k", metaKey: true });

    await waitFor(() => {
      expect(screen.getByPlaceholderText(/搜索/)).not.toBeNull();
    });
    // 等待数据加载
    await waitFor(() => {
      expect(screen.getByText("web-server-01")).not.toBeNull();
    });
    expect(screen.getByText("switch-core-01")).not.toBeNull();
    expect(screen.getByText("服务器CPU高")).not.toBeNull();
  });

  it("输入模糊匹配过滤结果", async () => {
    renderPalette();
    Object.defineProperty(navigator, "platform", {
      value: "MacIntel",
      configurable: true,
    });
    fireEvent.keyDown(document, { key: "k", metaKey: true });

    await waitFor(() => screen.getByText("web-server-01"));

    const input = screen.getByPlaceholderText(/搜索/) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "web" } });

    await waitFor(() => {
      // web 匹配的：web-server-01, web-01 (alert) — 都有 web
      expect(screen.getByText("web-server-01")).not.toBeNull();
      // db-server 不应再显示
      expect(screen.queryByText("db-server-01")).toBeNull();
    });
  });

  it("Esc 关闭 modal", async () => {
    renderPalette();
    Object.defineProperty(navigator, "platform", {
      value: "MacIntel",
      configurable: true,
    });
    fireEvent.keyDown(document, { key: "k", metaKey: true });

    await waitFor(() => screen.getByPlaceholderText(/搜索/));
    fireEvent.keyDown(document, { key: "Escape" });

    // Antd Modal 关闭有过渡动画（jsdom 下不跑 rAF，但 close 状态切换需 re-render）
    await waitFor(
      () => {
        expect(screen.queryByPlaceholderText(/搜索/)).toBeNull();
      },
      { timeout: 3000 },
    );
  });

  it("ArrowDown / ArrowUp 切换 active 项", async () => {
    renderPalette();
    Object.defineProperty(navigator, "platform", {
      value: "MacIntel",
      configurable: true,
    });
    fireEvent.keyDown(document, { key: "k", metaKey: true });

    await waitFor(() => screen.getByText("web-server-01"));

    const input = screen.getByPlaceholderText(/搜索/) as HTMLInputElement;
    // ArrowDown → active idx 1
    fireEvent.keyDown(input, { key: "ArrowDown" });
    await waitFor(() => {
      const items = document.querySelectorAll('[data-testid^="palette-item-"]');
      // Antd v5 把 #e6f4ff 转成 rgb(230, 244, 255)
      expect((items[1] as HTMLElement).style.background).toMatch(
        /230.*244.*255|e6f4ff/,
      );
    });

    // ArrowUp 回到 0
    fireEvent.keyDown(input, { key: "ArrowUp" });
    await waitFor(() => {
      const items = document.querySelectorAll('[data-testid^="palette-item-"]');
      expect((items[0] as HTMLElement).style.background).toMatch(
        /230.*244.*255|e6f4ff/,
      );
    });
  });

  it("Enter 触发跳转（active item）", async () => {
    renderPalette();
    Object.defineProperty(navigator, "platform", {
      value: "MacIntel",
      configurable: true,
    });
    fireEvent.keyDown(document, { key: "k", metaKey: true });

    await waitFor(() => screen.getByText("web-server-01"));
    const input = screen.getByPlaceholderText(/搜索/) as HTMLInputElement;
    fireEvent.keyDown(input, { key: "Enter" });

    // modal 关闭（同上：等动画）
    await waitFor(
      () => {
        expect(screen.queryByPlaceholderText(/搜索/)).toBeNull();
      },
      { timeout: 3000 },
    );
  });

  // 小改进 #3 review 修复 #3：Cmd+K 重复按 toggle 关闭
  it("Cmd+K 重复按 toggle 关闭 modal", async () => {
    renderPalette();
    Object.defineProperty(navigator, "platform", {
      value: "MacIntel",
      configurable: true,
    });

    // 第一次按：打开
    fireEvent.keyDown(document, { key: "k", metaKey: true });
    await waitFor(() => screen.getByPlaceholderText(/搜索/));

    // 第二次按：toggle 关闭
    fireEvent.keyDown(document, { key: "k", metaKey: true });
    await waitFor(
      () => {
        expect(screen.queryByPlaceholderText(/搜索/)).toBeNull();
      },
      { timeout: 3000 },
    );
  });

  // 小改进 #3 review 修复 #4：navigate 跳转断言
  it("点击资产项触发 navigate(/assets/a1)", async () => {
    renderPalette();
    Object.defineProperty(navigator, "platform", {
      value: "MacIntel",
      configurable: true,
    });

    fireEvent.keyDown(document, { key: "k", metaKey: true });
    await waitFor(() => screen.getByText("web-server-01"));

    // 点击资产项（应 navigate to /assets/a1）
    fireEvent.click(screen.getByText("web-server-01"));

    // modal 关闭 + navigate 调用
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith("/assets/a1");
    });
  });
});
