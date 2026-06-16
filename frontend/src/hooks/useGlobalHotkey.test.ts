import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useGlobalHotkey } from "./useGlobalHotkey";

describe("useGlobalHotkey", () => {
  beforeEach(() => {
    // jsdom 默认 platform 是 ''，改 'MacIntel' 测 mac 路径
    Object.defineProperty(navigator, "platform", {
      value: "MacIntel",
      configurable: true,
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("Mac: Cmd+K 触发 callback", () => {
    const cb = vi.fn();
    renderHook(() => useGlobalHotkey("k", cb));
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", metaKey: true }),
    );
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it("Mac: Ctrl+K 不触发（避免 Windows 行为）", () => {
    const cb = vi.fn();
    renderHook(() => useGlobalHotkey("k", cb));
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", ctrlKey: true }),
    );
    expect(cb).not.toHaveBeenCalled();
  });

  it("Mac: 单按 k 不触发（没修饰键）", () => {
    const cb = vi.fn();
    renderHook(() => useGlobalHotkey("k", cb));
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "k" }));
    expect(cb).not.toHaveBeenCalled();
  });

  it("大小写不敏感", () => {
    const cb = vi.fn();
    renderHook(() => useGlobalHotkey("k", cb));
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "K", metaKey: true }),
    );
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it("Shift / Alt 修饰时不触发", () => {
    const cb = vi.fn();
    renderHook(() => useGlobalHotkey("k", cb));
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", metaKey: true, shiftKey: true }),
    );
    expect(cb).not.toHaveBeenCalled();
  });

  it("unmount 解除监听", () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => useGlobalHotkey("k", cb));
    unmount();
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", metaKey: true }),
    );
    expect(cb).not.toHaveBeenCalled();
  });
});
