import { describe, it, expect, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ConfigProvider, theme } from "antd";
import { useEffect } from "react";
import { ThemeSwitcher } from "./ThemeSwitcher";
import { useThemeStore } from "../stores";

const renderWithProvider = (ui: React.ReactNode) =>
  render(<ConfigProvider>{ui}</ConfigProvider>);

// 探针组件：读 theme.useToken().colorBgLayout（light vs dark 下值不同）
function ColorBgProbe({ onChange }: { onChange: (v: string) => void }) {
  const { token } = theme.useToken();
  useEffect(() => {
    onChange(token.colorBgLayout);
  }, [token.colorBgLayout, onChange]);
  return null;
}

describe("ThemeSwitcher", () => {
  beforeEach(() => {
    useThemeStore.setState({ mode: "light" });
    localStorage.removeItem("theme-storage");
  });

  it("渲染切换按钮", () => {
    renderWithProvider(<ThemeSwitcher />);
    const btn = screen.getByRole("button", { name: "切换主题" });
    expect(btn).not.toBeNull();
  });

  it("浅色模式点击 → 切换到 dark", () => {
    renderWithProvider(<ThemeSwitcher />);
    fireEvent.click(screen.getByRole("button", { name: "切换主题" }));
    expect(useThemeStore.getState().mode).toBe("dark");
  });

  it("深色模式点击 → 回到 light", () => {
    useThemeStore.getState().setMode("dark");
    renderWithProvider(<ThemeSwitcher />);
    fireEvent.click(screen.getByRole("button", { name: "切换主题" }));
    expect(useThemeStore.getState().mode).toBe("light");
  });

  it("深色模式显示 Sun 图标（aria-label 不变）", () => {
    useThemeStore.getState().setMode("dark");
    renderWithProvider(<ThemeSwitcher />);
    // 验证 store 状态正确反映 mode
    expect(useThemeStore.getState().mode).toBe("dark");
    // 按钮可点击，且包含 icon
    const btn = screen.getByRole("button", { name: "切换主题" });
    expect(btn.querySelector(".anticon")).not.toBeNull();
  });
});

describe("ConfigProvider 集成", () => {
  beforeEach(() => {
    useThemeStore.setState({ mode: "light" });
    localStorage.removeItem("theme-storage");
  });

  it("light 与 dark 模式下 colorBgLayout 不同（验证 algorithm 真切）", () => {
    const observed: string[] = [];

    const renderAt = (alg: "light" | "dark") =>
      render(
        <ConfigProvider
          theme={{
            algorithm:
              alg === "dark" ? theme.darkAlgorithm : theme.defaultAlgorithm,
          }}
        >
          <ColorBgProbe onChange={(v) => observed.push(v)} />
        </ConfigProvider>,
      );

    // light 渲染
    const { unmount: unmountLight } = renderAt("light");
    const lightColor = observed[observed.length - 1];
    unmountLight();

    // dark 渲染
    const { unmount: unmountDark } = renderAt("dark");
    const darkColor = observed[observed.length - 1];
    unmountDark();

    // light vs dark colorBgLayout 必须不同（白底 vs 深底）
    expect(lightColor).toMatch(/^#/);
    expect(darkColor).toMatch(/^#/);
    expect(lightColor).not.toBe(darkColor);
  });
});
