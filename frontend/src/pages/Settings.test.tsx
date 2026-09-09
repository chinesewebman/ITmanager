// FIX-PLAN-AUTHZ-CLOSURE.md §2 D-B / AV-5、AV-6
//
// 密钥管理区块此前两处前端契约缺陷（§1 S-2）：
//   - 读 res.data.data.key，后端返回 api_key → 一次性明文 Key 永不显示；
//   - list 返 403 时 catch 里只 console.error + 置空数组 → 非 admin 看到静默空白。
import "@testing-library/jest-dom";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import Settings from "./Settings";
// G-33 M1：跨语言配置契约样本（后端 internal/service/channel_service_test.go 读同一文件）
import channelConfigSamples from "./__fixtures__/channelConfigSamples.json";

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

// ==================== G-33 M1：通知渠道配置契约 ====================
// 样本文件是跨语言契约的单一事实来源（后端 channel_service_test.go 读同一文件喂
// notification.NewSender）。改表单键名/改类型时这里先红。
describe("通知渠道配置契约 (G-33 M1)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(notificationApi.listChannels).mockResolvedValue({
      data: { code: 0, data: [] },
    } as any);
    vi.mocked(integrationApi.getStatus).mockResolvedValue({
      data: { code: 0, data: {} },
    } as any);
    vi.mocked(notificationApi.createChannel).mockResolvedValue({
      data: { code: 0, data: {} },
    } as any);
  });

  // 页面里还有 Zabbix 表单的「用户名」等同名 label → 所有字段查询都限定在渠道弹窗内
  async function openChannelForm(typeLabel: string) {
    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));
    fireEvent.click(await screen.findByRole("button", { name: /添加渠道/ }));

    const nameInput = await screen.findByLabelText("渠道名称");
    fireEvent.change(nameInput, { target: { value: "契约样本" } });
    // antd Select：mouseDown 打开下拉，再点选项（选项带 title=label）
    fireEvent.mouseDown(screen.getByLabelText("渠道类型"));
    fireEvent.click(await screen.findByTitle(typeLabel));

    return within(nameInput.closest(".ant-modal") as HTMLElement);
  }

  function savedConfig(): any {
    const payload: any = vi.mocked(notificationApi.createChannel).mock.calls[0][0];
    return { type: payload.type, config: JSON.parse(payload.config) };
  }

  it("email 表单产出与样本一致（端口必须是数字）", async () => {
    const modal = await openChannelForm("邮件");

    fireEvent.change(await modal.findByLabelText("SMTP服务器"), {
      target: { value: channelConfigSamples.email.smtp_host },
    });
    fireEvent.change(modal.getByLabelText("端口"), {
      target: { value: String(channelConfigSamples.email.smtp_port) },
    });
    fireEvent.change(modal.getByLabelText("用户名"), {
      target: { value: channelConfigSamples.email.smtp_user },
    });
    fireEvent.change(modal.getByLabelText("密码"), {
      target: { value: channelConfigSamples.email.smtp_password },
    });
    fireEvent.change(modal.getByLabelText("发件人"), {
      target: { value: channelConfigSamples.email.from },
    });
    const toInput = modal.getByLabelText("收件人");
    fireEvent.change(toInput, {
      target: { value: channelConfigSamples.email.to[0] },
    });
    fireEvent.keyDown(toInput, { key: "Enter", code: "Enter", keyCode: 13 });

    fireEvent.click(modal.getByRole("button", { name: "保 存" }));
    await waitFor(() => expect(notificationApi.createChannel).toHaveBeenCalled());

    const saved = savedConfig();
    expect(saved.type).toBe("email");
    expect(saved.config).toEqual(channelConfigSamples.email);
  });

  it("dingtalk 表单产出与样本一致", async () => {
    const modal = await openChannelForm("钉钉");

    fireEvent.change(await modal.findByLabelText("Webhook URL"), {
      target: { value: channelConfigSamples.dingtalk.webhook_url },
    });
    fireEvent.change(modal.getByLabelText("加签密钥（可选）"), {
      target: { value: channelConfigSamples.dingtalk.sign_secret },
    });

    fireEvent.click(modal.getByRole("button", { name: "保 存" }));
    await waitFor(() => expect(notificationApi.createChannel).toHaveBeenCalled());

    const saved = savedConfig();
    expect(saved.type).toBe("dingtalk");
    expect(saved.config).toEqual(channelConfigSamples.dingtalk);
  });

  it("webhook 表单产出与样本一致", async () => {
    const modal = await openChannelForm("Webhook");

    fireEvent.change(await modal.findByLabelText("Webhook URL"), {
      target: { value: channelConfigSamples.webhook.url },
    });
    fireEvent.change(modal.getByLabelText("签名密钥（可选）"), {
      target: { value: channelConfigSamples.webhook.secret },
    });

    fireEvent.click(modal.getByRole("button", { name: "保 存" }));
    await waitFor(() => expect(notificationApi.createChannel).toHaveBeenCalled());

    const saved = savedConfig();
    expect(saved.type).toBe("webhook");
    expect(saved.config).toEqual(channelConfigSamples.webhook);
  });

  // H-2：Modal 不销毁重建时 Form.initialValues 只在挂载时生效，连续编辑两条渠道
  // 会把上一条的 name/type/config 写进当前记录（正确性审计 H-2）。
  it("连续编辑两条渠道：表单显示当前记录", async () => {
    vi.mocked(notificationApi.listChannels).mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: "1",
            name: "渠道A",
            type: "webhook",
            config: JSON.stringify(channelConfigSamples.webhook),
            is_enabled: true,
          },
          {
            id: "2",
            name: "渠道B",
            type: "dingtalk",
            config: JSON.stringify(channelConfigSamples.dingtalk),
            is_enabled: true,
          },
        ],
      },
    } as any);
    vi.mocked(notificationApi.updateChannel).mockResolvedValue({
      data: { code: 0, data: {} },
    } as any);

    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));
    const editButtons = await screen.findAllByRole("button", { name: "编辑" });

    fireEvent.click(editButtons[0]);
    const modalA = within(
      (await screen.findByLabelText("渠道名称")).closest(".ant-modal") as HTMLElement
    );
    expect(modalA.getByLabelText("渠道名称")).toHaveValue("渠道A");
    fireEvent.click(modalA.getByRole("button", { name: "取 消" }));

    fireEvent.click(editButtons[1]);
    const modalB = within(
      (await screen.findByLabelText("渠道名称")).closest(".ant-modal") as HTMLElement
    );
    expect(modalB.getByLabelText("渠道名称")).toHaveValue("渠道B");
    expect(modalB.getByLabelText("Webhook URL")).toHaveValue(
      channelConfigSamples.dingtalk.webhook_url
    );
  });

  // 正确性 M-1：存量 wechat 行既不能被当成 Webhook 渲染（键名对不上、必填永远过不了），
  // 也不能显示裸值 wechat —— 类型框要显示可读标签 + 下线提示。
  it("存量 wechat 行：可读标签 + 下线提示，不按 Webhook 渲染", async () => {
    vi.mocked(notificationApi.listChannels).mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: "9",
            name: "企微",
            type: "wechat",
            config: `{"webhook_url":"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"}`,
            is_enabled: false,
          },
        ],
      },
    } as any);

    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));
    fireEvent.click(await screen.findByRole("button", { name: "编辑" }));

    const modal = within(
      (await screen.findByLabelText("渠道名称")).closest(".ant-modal") as HTMLElement
    );
    expect(modal.getByText("企业微信（暂不支持）")).toBeInTheDocument();
    expect(modal.getByText("企业微信通知暂不支持")).toBeInTheDocument();
    expect(modal.queryByLabelText("Webhook URL")).not.toBeInTheDocument();
  });
});
