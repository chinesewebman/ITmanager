// C7: ChangePassword 页测试
// 关键: 主人 7/02 决策 — 首次登录 (reason=first_login) 不可跳, 隐藏"本次跳过"按钮
import "@testing-library/jest-dom";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import ChangePassword from "./ChangePassword";

// Mock antd message (避免 jsdom 副作用)
vi.mock("antd", async () => {
  const actual = await vi.importActual<typeof import("antd")>("antd");
  return {
    ...actual,
    message: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
  };
});

// Mock services/api
vi.mock("../services/api", () => ({
  authApi: {
    changePassword: vi.fn(),
    skipPasswordChange: vi.fn(),
  },
}));

import { authApi } from "../services/api";

function renderChangePassword(queryString: string = "") {
  return render(
    <MemoryRouter initialEntries={[`/change-password${queryString}`]}>
      <Routes>
        <Route path="/change-password" element={<ChangePassword />} />
        <Route path="/" element={<div>HOME_PAGE</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("ChangePassword page (C7)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // 核心: 首次登录不可跳
  it('首次登录 (reason=first_login) 不显示"本次跳过"按钮', () => {
    renderChangePassword("?reason=first_login");

    // 提交按钮文案区分 (改密并继续 vs 确认修改)
    expect(
      screen.getByRole("button", { name: /修改密码并继续/ }),
    ).toBeInTheDocument();

    // "本次跳过" 按钮不渲染
    expect(
      screen.queryByRole("button", { name: /本次跳过/ }),
    ).not.toBeInTheDocument();

    // Alert 警告出现
    expect(screen.getByText("首次登录必须修改默认密码")).toBeInTheDocument();
  });

  it('非首次 (?reason=optional) 显示"本次跳过"按钮', () => {
    renderChangePassword("?reason=optional");

    expect(
      screen.getByRole("button", { name: /确认修改/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /本次跳过/ }),
    ).toBeInTheDocument();
  });

  it('不带 query 参数 (默认 optional) 也显示"本次跳过"按钮', () => {
    renderChangePassword("");

    expect(
      screen.getByRole("button", { name: /本次跳过/ }),
    ).toBeInTheDocument();
  });

  it('点"本次跳过"调 authApi.skipPasswordChange("optional") 并跳首页', async () => {
    vi.mocked(authApi.skipPasswordChange).mockResolvedValue({
      data: { code: 0 },
    } as any);

    renderChangePassword("?reason=optional");
    fireEvent.click(screen.getByRole("button", { name: /本次跳过/ }));

    await waitFor(() => {
      expect(authApi.skipPasswordChange).toHaveBeenCalledWith("optional");
    });

    // 跳回首页
    await waitFor(() => {
      expect(screen.getByText("HOME_PAGE")).toBeInTheDocument();
    });
  });
});
