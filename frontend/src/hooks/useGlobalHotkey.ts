import { useEffect } from "react";

/**
 * 全局键盘快捷键 hook（小改进 #3：Cmd+K 全局搜索）
 * Mac 用 ⌘ (metaKey)，其他用 Ctrl
 * @param key 单字符，如 'k'
 * @param callback 触发回调
 */
export function useGlobalHotkey(key: string, callback: () => void): void {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const isMac =
        typeof navigator !== "undefined" &&
        navigator.platform.toUpperCase().includes("MAC");
      const cmdOrCtrl = isMac ? e.metaKey : e.ctrlKey;
      if (
        cmdOrCtrl &&
        e.key.toLowerCase() === key.toLowerCase() &&
        !e.shiftKey &&
        !e.altKey
      ) {
        e.preventDefault();
        callback();
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [key, callback]);
}
