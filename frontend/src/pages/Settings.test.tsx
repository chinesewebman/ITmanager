// FIX-PLAN-AUTHZ-CLOSURE.md §2 D-B / AV-5、AV-6
//
// 密钥管理区块此前两处前端契约缺陷（§1 S-2）：
//   - 读 res.data.data.key，后端返回 api_key → 一次性明文 Key 永不显示；
//   - list 返 403 时 catch 里只 console.error + 置空数组 → 非 admin 看到静默空白。
import "@testing-library/jest-dom";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import Settings from "./Settings";

// Mock antd message（避免 jsdom 副作用）
vi.mock("antd", async () => {
  const actual = await vi.importActual<typeof import("antd")>("antd");
  return {
    ...actual,
    message: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
  };
});

// Mock services/api：Settings 首屏会拉渠道 + 集成状态，这里全部给出空实现
vi.mock("../services/api", () => ({
  notificationApi: {
    listChannels: vi.fn(),
    createChannel: vi.fn(),
    updateChannel: vi.fn(),
    deleteChannel: vi.fn(),
    testChannel: vi.fn(),
  },
  integrationApi: {
    getStatus: vi.fn(),
    updateZabbix: vi.fn(),
    testZabbix: vi.fn(),
    syncZabbix: vi.fn(),
    updateNetBox: vi.fn(),
    testNetBox: vi.fn(),
    syncNetBox: vi.fn(),
    updateGLPI: vi.fn(),
    testGLPI: vi.fn(),
    syncGLPI: vi.fn(),
  },
  apiKeyApi: {
    list: vi.fn(),
    create: vi.fn(),
    revoke: vi.fn(),
    delete: vi.fn(),
  },
}));

import { apiKeyApi, notificationApi, integrationApi } from "../services/api";

// 密钥管理在第三个 tab（integrations / notifications / api），antd 默认只渲染
// 激活面板，所以必须先点开该 tab 才能断言其内容
async function renderApiKeyTab() {
  render(<Settings />);
  fireEvent.click(await screen.findByRole("tab", { name: /API 密钥/ }));
}

describe("Settings 密钥管理区块 (D-B)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(notificationApi.listChannels).mockResolvedValue({
      data: { code: 0, data: [] },
    } as any);
    vi.mocked(integrationApi.getStatus).mockResolvedValue({
      data: { code: 0, data: {} },
    } as any);
    vi.mocked(apiKeyApi.list).mockResolvedValue({
      data: { code: 0, data: [] },
    } as any);
  });

  // AV-5：后端字段名是 api_key —— 用 key 时这块永远空白
  it("创建成功后渲染后端返回的 api_key 明文", async () => {
    const plaintext = "sk_live_abc123def456";
    vi.mocked(apiKeyApi.create).mockResolvedValue({
      data: { code: 0, data: { id: "k1", name: "ci", api_key: plaintext } },
    } as any);

    await renderApiKeyTab();
    fireEvent.click(await screen.findByRole("button", { name: /生成密钥/ }));

    fireEvent.change(await screen.findByPlaceholderText("如：监控告警推送"), {
      target: { value: "ci" },
    });
    fireEvent.click(screen.getByRole("button", { name: "生 成" }));

    await waitFor(() => {
      expect(apiKeyApi.create).toHaveBeenCalled();
    });
    // 明文渲染在只读 TextArea 里（value 属性，不是文本节点）
    expect(await screen.findByDisplayValue(plaintext)).toBeInTheDocument();
  });

  // 安全审计 F5：X / 遮罩 / ESC 关闭 Modal 时若不清 generatedKey，再次点「生成密钥」
  // 会重新显示上一把明文 Key，违背「仅此一次展示」
  it("关闭 Modal 后重新打开不再显示上一把明文 Key", async () => {
    const plaintext = "sk_live_should_not_persist";
    vi.mocked(apiKeyApi.create).mockResolvedValue({
      data: { code: 0, data: { id: "k1", name: "ci", api_key: plaintext } },
    } as any);

    await renderApiKeyTab();
    fireEvent.click(await screen.findByRole("button", { name: /生成密钥/ }));
    fireEvent.change(await screen.findByPlaceholderText("如：监控告警推送"), {
      target: { value: "ci" },
    });
    fireEvent.click(screen.getByRole("button", { name: "生 成" }));
    expect(await screen.findByDisplayValue(plaintext)).toBeInTheDocument();
    const shownBlock = /密钥已生成（仅此一次展示/;
    expect(screen.getByText(shownBlock)).toBeInTheDocument();

    // 点 Modal 右上角 X（antd 关闭按钮 aria-label="Close"）后重新打开。
    //
    // 不断言「关闭瞬间块消失」：antd Modal 的离场动画由 rc-motion 驱动，jsdom 里
    // transitionend 不触发，离场节点会被保留在 DOM 中（实测关闭后 queryByText
    // 仍命中），断言它反而会假红。用户实际能看到的时刻是**重新打开**——那里
    // Modal 内容重新渲染，generatedKey 若没清就会重新显示明文 Key。
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    fireEvent.click(screen.getByRole("button", { name: /生成密钥/ }));
    await waitFor(() => {
      expect(screen.queryByText(shownBlock)).not.toBeInTheDocument();
      expect(screen.queryByDisplayValue(plaintext)).not.toBeInTheDocument();
    });
  });

  // AV-6：非 admin 访问 /auth/api-keys 返 403 → 必须给出可读的说明，而非空白
  it("list 返 403 时渲染「无密钥管理权限」Alert", async () => {
    vi.mocked(apiKeyApi.list).mockRejectedValue({
      response: { status: 403, data: {} },
    });

    await renderApiKeyTab();

    expect(
      await screen.findByText("当前账号无密钥管理权限"),
    ).toBeInTheDocument();
  });

  // 一致性审计 F3：`apiKeysForbidden` 曾只置不清——一旦出现 403，后续真实故障
  // （500）也会继续显示「无密钥管理权限」，把故障伪装成权限问题。
  // 触发第二次 list 走既有路径：生成成功后组件会 `await fetchApiKeys()`。
  it("先 403 后 500 时撤下「无密钥管理权限」Alert", async () => {
    vi.mocked(apiKeyApi.list)
      .mockRejectedValueOnce({ response: { status: 403, data: {} } })
      .mockRejectedValueOnce({ response: { status: 500, data: {} } })
    vi.mocked(apiKeyApi.create).mockResolvedValue({
      data: { code: 0, data: { id: "k1", name: "ci", api_key: "sk_x" } },
    } as any);

    await renderApiKeyTab();
    expect(
      await screen.findByText("当前账号无密钥管理权限"),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /生成密钥/ }));
    fireEvent.change(await screen.findByPlaceholderText("如：监控告警推送"), {
      target: { value: "ci" },
    });
    fireEvent.click(screen.getByRole("button", { name: "生 成" }));

    await waitFor(() => {
      expect(apiKeyApi.list).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(
        screen.queryByText("当前账号无密钥管理权限"),
      ).not.toBeInTheDocument();
    });
  });

  // 反向：非 403 失败不得被误报成「无权限」（否则真实故障会被掩盖成权限问题）
  it("list 返 500 时不显示权限 Alert", async () => {
    vi.mocked(apiKeyApi.list).mockRejectedValue({
      response: { status: 500, data: {} },
    });

    await renderApiKeyTab();
    // 等首屏两个请求都落地，避免「Alert 还没渲染」被当成通过
    await waitFor(() => {
      expect(apiKeyApi.list).toHaveBeenCalled();
    });

    expect(
      screen.queryByText("当前账号无密钥管理权限"),
    ).not.toBeInTheDocument();
  });
});
