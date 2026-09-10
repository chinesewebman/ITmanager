import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import "@testing-library/jest-dom";
import { ConfigProvider } from "antd";
import { AlertCard } from "./AlertCard";
import type { Alert } from "./AlertTable";

const base: Alert = {
  id: "a1",
  host: "web-01",
  message: "CPU 使用率过高",
  severity: 5,
  severity_name: "灾难",
  status: "problem",
  created_at: "2026-09-10T02:09:00+08:00",
};

describe("AlertCard", () => {
  it("渲染主机名、告警信息、级别", () => {
    render(
      <ConfigProvider>
        <AlertCard alert={base} onAck={() => {}} onResolve={() => {}} />
      </ConfigProvider>,
    );
    expect(screen.getByText("web-01")).toBeInTheDocument();
    expect(screen.getByText("CPU 使用率过高")).toBeInTheDocument();
    expect(screen.getByText("灾难")).toBeInTheDocument();
  });

  it("status=problem 显示确认/解决，点击确认触发 onAck(id)", () => {
    const onAck = vi.fn();
    const onResolve = vi.fn();
    render(
      <ConfigProvider>
        <AlertCard alert={base} onAck={onAck} onResolve={onResolve} />
      </ConfigProvider>,
    );
    expect(screen.getByText("解决")).toBeInTheDocument();
    fireEvent.click(screen.getByText("确认"));
    expect(onAck).toHaveBeenCalledWith("a1");
  });

  it("status=acknowledged 只显示解决，点击触发 onResolve(id)", () => {
    const onResolve = vi.fn();
    render(
      <ConfigProvider>
        <AlertCard
          alert={{ ...base, status: "acknowledged" }}
          onAck={() => {}}
          onResolve={onResolve}
        />
      </ConfigProvider>,
    );
    expect(screen.queryByText("确认")).toBeNull();
    fireEvent.click(screen.getByText("解决"));
    expect(onResolve).toHaveBeenCalledWith("a1");
  });

  it("未标记误报时显示「标记误报」，点击触发 onMarkFP(id, true)", () => {
    const onMarkFP = vi.fn();
    render(
      <ConfigProvider>
        <AlertCard
          alert={base}
          onAck={() => {}}
          onResolve={() => {}}
          onMarkFP={onMarkFP}
        />
      </ConfigProvider>,
    );
    fireEvent.click(screen.getByText("标记误报"));
    expect(onMarkFP).toHaveBeenCalledWith("a1", true);
  });

  it("已标记误报时显示「取消误报」，点击触发 onMarkFP(id, false)", () => {
    const onMarkFP = vi.fn();
    render(
      <ConfigProvider>
        <AlertCard
          alert={{ ...base, is_false_positive: true }}
          onAck={() => {}}
          onResolve={() => {}}
          onMarkFP={onMarkFP}
        />
      </ConfigProvider>,
    );
    expect(screen.queryByText("标记误报")).toBeNull();
    fireEvent.click(screen.getByText("取消误报"));
    expect(onMarkFP).toHaveBeenCalledWith("a1", false);
  });

  it("onMarkFP 未传时不渲染误报按钮", () => {
    render(
      <ConfigProvider>
        <AlertCard alert={base} onAck={() => {}} onResolve={() => {}} />
      </ConfigProvider>,
    );
    expect(screen.queryByText("标记误报")).toBeNull();
    expect(screen.queryByText("取消误报")).toBeNull();
  });
});
