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

  // W2：最后使用列原 `t ? new Date(t).toLocaleString() : '—'`，口径与其它页不一致
  // 且非法时间会显示 "Invalid Date"；改 formatDateTime 统一 'YYYY-MM-DD HH:mm:ss'。
  // 时间串不带偏移 → dayjs 按本地解析，断言与 CI 时区无关。
  it("W2：最后使用列走 formatDateTime 统一格式", async () => {
    vi.mocked(apiKeyApi.list).mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: "k1",
            name: "ci",
            prefix: "sk_live",
            permissions: ["read"],
            ip_whitelist: [],
            rate_limit: 100,
            status: "active",
            last_used_at: "2026-02-14T10:00:00",
            created_at: "2026-02-14T00:00:00",
          },
        ],
      },
    } as any);

    await renderApiKeyTab();

    expect(await screen.findByText("2026-02-14 10:00:00")).toBeInTheDocument();
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
    // L-6（测试有效性审计）：vi.clearAllMocks() 不清实现，本 describe 不设
    // apiKeyApi.list 时会继承上一 describe 的 mockRejectedValue（stderr 噪声 + 顺序耦合）。
    vi.mocked(apiKeyApi.list).mockResolvedValue({
      data: { code: 0, data: [] },
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

  // M6：required 无 message → 统一「请输入/请选择 XXX」（本文件 10 处）。
  // 无 message 时 antd 默认英文「${label} is required」，语气不一致。
  it("M6：渠道表单顶层必填项带中文提示", async () => {
    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));
    fireEvent.click(await screen.findByRole("button", { name: /添加渠道/ }));

    // 不填名称/类型直接保存 → 触发校验
    const modal = (await screen.findByLabelText("渠道名称")).closest(".ant-modal") as HTMLElement;
    fireEvent.click(within(modal).getByRole("button", { name: /保\s*存/ }));

    expect(await within(modal).findByText("请输入渠道名称")).toBeInTheDocument();
    expect(within(modal).getByText("请选择渠道类型")).toBeInTheDocument();
  });

  it("M6：email 渠道条件字段带中文提示", async () => {
    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));
    fireEvent.click(await screen.findByRole("button", { name: /添加渠道/ }));

    fireEvent.mouseDown(await screen.findByLabelText("渠道类型"));
    fireEvent.click(await screen.findByTitle("邮件"));

    const modal = (await screen.findByLabelText("渠道名称")).closest(".ant-modal") as HTMLElement;
    fireEvent.click(within(modal).getByRole("button", { name: /保\s*存/ }));

    expect(await within(modal).findByText("请输入SMTP服务器")).toBeInTheDocument();
    expect(within(modal).getByText("请输入端口")).toBeInTheDocument();
    expect(within(modal).getByText("请输入用户名")).toBeInTheDocument();
    expect(within(modal).getByText("请输入发件人")).toBeInTheDocument();
    expect(within(modal).getByText("请输入收件人")).toBeInTheDocument();
  });

  it("M6：dingtalk 渠道 Webhook URL 带中文提示", async () => {
    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));
    fireEvent.click(await screen.findByRole("button", { name: /添加渠道/ }));

    fireEvent.mouseDown(await screen.findByLabelText("渠道类型"));
    fireEvent.click(await screen.findByTitle("钉钉"));

    const modal = (await screen.findByLabelText("渠道名称")).closest(".ant-modal") as HTMLElement;
    fireEvent.click(within(modal).getByRole("button", { name: /保\s*存/ }));

    expect(await within(modal).findByText("请输入Webhook URL")).toBeInTheDocument();
  });

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
            // M-1（测试有效性审计）：停用状态必须原样保留 —— 硬编码 true 会让编辑
            // 一条已停用渠道把它静默启用（开始真发告警）。
            is_enabled: false,
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

    // M-1：保存时 is_enabled 必须沿用当前记录（false），不能硬编码 true
    fireEvent.click(modalB.getByRole("button", { name: "保 存" }));
    await waitFor(() => expect(notificationApi.updateChannel).toHaveBeenCalled());
    const payload: any = vi.mocked(notificationApi.updateChannel).mock.calls[0][1];
    expect(payload.is_enabled).toBe(false);
  });

  // L-1（测试有效性审计）：listChannels 失败时的兜底样本也是 M1 的改动（原来键名全错），
  // 但没有任何用例让请求失败 → 改回坏键全绿。这里用编辑弹窗的回填值钉住兜底键名。
  it("listChannels 失败：兜底样本键名可回填", async () => {
    vi.mocked(notificationApi.listChannels).mockRejectedValue(new Error("500"));
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));
    fireEvent.click((await screen.findAllByRole("button", { name: "编辑" }))[0]);

    const modal = within(
      (await screen.findByLabelText("渠道名称")).closest(".ant-modal") as HTMLElement
    );
    expect(modal.getByLabelText("SMTP服务器")).toHaveValue("smtp.example.com");
    errSpy.mockRestore();
  });

  // G-36 M3：企微 sender 已实现 → 下拉可选、表单只产出 url（不含 secret）。
  it("wechat 表单产出与样本一致（且不含 secret 死键）", async () => {
    const modal = await openChannelForm("企业微信");

    // WeChatSender 忽略 secret —— 表单给这个框会造出"配了不生效"的死键。
    // 必须在**保存前**断言：保存会 resetFields()，表单回到 type 未选中的兜底分支
    // （那里有 secret），此时断言恒假红。
    expect(modal.queryByLabelText("签名密钥（可选）")).not.toBeInTheDocument();

    fireEvent.change(await modal.findByLabelText("Webhook URL"), {
      target: { value: channelConfigSamples.wechat.url },
    });

    fireEvent.click(modal.getByRole("button", { name: "保 存" }));
    await waitFor(() => expect(notificationApi.createChannel).toHaveBeenCalled());

    const saved = savedConfig();
    expect(saved.type).toBe("wechat");
    expect(saved.config).toEqual(channelConfigSamples.wechat);
  });

  // M3 正确性审计 LOW-1：wechat 的 rules={[{ required: true }]} 此前无测试钉住 ——
  // 删掉它全部用例仍绿（上面那条自己填了 URL），用户可从 UI 提交空 URL。
  it("wechat 不填 URL 不可保存", async () => {
    const modal = await openChannelForm("企业微信");

    fireEvent.click(modal.getByRole("button", { name: "保 存" }));

    await waitFor(() =>
      expect(modal.getByLabelText("Webhook URL")).toHaveClass(
        "ant-input-status-error",
      ),
    );
    expect(notificationApi.createChannel).not.toHaveBeenCalled();
  });

  // 存量 wechat 行：旧键名是 webhook_url（M1 前 seed 写的），WeChatSender 只认 url
  // → 编辑时 URL 留空待补填，而不是静默把旧键回填进表单（保存后键名又变回去）。
  it("存量 wechat 行：可读标签 + url 留空待补填", async () => {
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
    expect(modal.getByText("企业微信")).toBeInTheDocument();
    expect(modal.getByLabelText("Webhook URL")).toHaveValue("");
  });

  // 安全审计 L-3：保存失败时 console.error 不得把整个 axios error 丢出去 ——
  // error.config.data 是请求体明文（smtp_password / sign_secret / 企微 key）。
  it("保存失败只记状态码与已脱敏文案，不记 error 对象", async () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const leaky = {
      response: {
        status: 400,
        data: { message: "配置无效" },
        // 真实 axios 错误里这个字段就是请求体
        config: { data: '{"smtp_password":"LEAKEDPW"}' },
      },
    };
    vi.mocked(notificationApi.createChannel).mockRejectedValue(leaky);

    const modal = await openChannelForm("企业微信");
    fireEvent.change(modal.getByLabelText("Webhook URL"), {
      target: { value: channelConfigSamples.wechat.url },
    });
    fireEvent.click(modal.getByRole("button", { name: "保 存" }));

    // jsdom 自己也会往 console.error 写噪声（getComputedStyle 等），按首参过滤
    const call = await waitFor(() => {
      const hit = errSpy.mock.calls.find((c) => c[0] === "保存通知渠道失败:");
      expect(hit).toBeTruthy();
      return hit;
    });
    expect(call).toEqual(["保存通知渠道失败:", 400, "配置无效"]);
    expect(JSON.stringify(call)).not.toContain("LEAKEDPW");
    errSpy.mockRestore();
  });
});

// ==================== W4-H8：删除渠道二次确认 ====================
describe("H8 删除渠道二次确认", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(notificationApi.listChannels).mockResolvedValue({
      data: {
        code: 0,
        data: [
          { id: "c1", name: "钉钉告警", type: "dingtalk", config: "{}", is_enabled: true },
        ],
      },
    } as any);
    vi.mocked(notificationApi.deleteChannel).mockResolvedValue({
      data: { code: 0, data: {} },
    } as any);
    vi.mocked(integrationApi.getStatus).mockResolvedValue({
      data: { code: 0, data: {} },
    } as any);
    vi.mocked(apiKeyApi.list).mockResolvedValue({
      data: { code: 0, data: [] },
    } as any);
  });

  it("点删除先弹确认框（不立即调 deleteChannel），确认后才调用", async () => {
    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));

    // 列表删除按钮（antd 两个汉字自动插空格 → "删 除"）
    fireEvent.click(await screen.findByRole("button", { name: /删\s*除/ }));

    // 确认框出现，且 deleteChannel 尚未被调用
    const title = await screen.findByText(/确认删除渠道「钉钉告警」/);
    expect(title).toBeInTheDocument();
    expect(notificationApi.deleteChannel).not.toHaveBeenCalled();

    // 点 Popconfirm 里的确认按钮
    const popover = title.closest(".ant-popover") as HTMLElement;
    fireEvent.click(within(popover).getByRole("button", { name: /删\s*除/ }));

    await waitFor(() => {
      expect(notificationApi.deleteChannel).toHaveBeenCalledWith("c1");
    });
  });
});

// ==================== W4-M4：渠道保存按钮 loading 防连点 ====================
describe("W4-M4 渠道保存按钮 loading 防连点", () => {
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

  it("提交中保存按钮 loading 且防连点（createChannel 只调一次）", async () => {
    let resolveSend!: (v: unknown) => void;
    vi.mocked(notificationApi.createChannel).mockImplementationOnce(
      () => new Promise((resolve) => { resolveSend = resolve; }) as any,
    );

    render(<Settings />);
    fireEvent.click(await screen.findByRole("tab", { name: /通知设置/ }));
    fireEvent.click(await screen.findByRole("button", { name: /添加渠道/ }));

    fireEvent.change(await screen.findByLabelText("渠道名称"), { target: { value: "测试渠道" } });
    fireEvent.mouseDown(screen.getByLabelText("渠道类型"));
    fireEvent.click(await screen.findByTitle("钉钉"));

    const modal = within(
      (await screen.findByLabelText("Webhook URL")).closest(".ant-modal") as HTMLElement,
    );
    fireEvent.change(modal.getByLabelText("Webhook URL"), { target: { value: "https://example.com/hook" } });

    const saveBtn = modal.getByRole("button", { name: /保\s*存/ });
    fireEvent.click(saveBtn);

    // 请求 pending → 按钮进入 loading
    await waitFor(() => {
      expect(saveBtn).toHaveClass("ant-btn-loading");
    });

    // loading 期间连点不应触发第二次提交
    fireEvent.click(saveBtn);
    expect(notificationApi.createChannel).toHaveBeenCalledTimes(1);

    resolveSend({ data: { code: 0, data: {} } });
  });
});
