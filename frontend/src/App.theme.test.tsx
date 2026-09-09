// H6：App 主题必须开启 cssVar，否则 antd 不注入 --ant-* CSS 变量，
// 13 处 var(--ant-*)（EmptyState/AssetTable/CommandPalette/StatusPage/Alerts/Assets
// /AppBreadcrumb/useResponsiveTable）全部失效 → 次要文字色/背景/边框丢失。
import { render } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { describe, it, expect } from "vitest";
import { buildTheme } from "./App";

describe("H6: App 主题开启 cssVar（13 处 var(--ant-*) 依赖 --ant-* 注入）", () => {
  it("buildTheme 明暗两态均开启 cssVar", () => {
    expect(buildTheme("light").cssVar).toBe(true);
    expect(buildTheme("dark").cssVar).toBe(true);
  });

  it("cssVar:true 时 antd 注入 --ant-color-text-secondary 变量", () => {
    render(
      <ConfigProvider theme={buildTheme("light")}>
        <div>hi</div>
      </ConfigProvider>,
    );
    const styles = Array.from(document.querySelectorAll("style"))
      .map((s) => s.textContent || "")
      .join("\n");
    expect(styles).toContain("--ant-color-text-secondary");
  });
});
