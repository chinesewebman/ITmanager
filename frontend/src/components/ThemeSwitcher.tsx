import { Button, Tooltip } from "antd";
import { SunOutlined, MoonOutlined } from "@ant-design/icons";
import { useThemeStore } from "../stores";

/**
 * 暗色模式切换按钮
 * - light → 显示 Moon，提示"切换到深色"
 * - dark  → 显示 Sun，提示"切换到浅色"
 * - 状态持久化到 localStorage 'theme-storage'（zustand persist）
 */
export function ThemeSwitcher() {
  const mode = useThemeStore((s) => s.mode);
  const toggle = useThemeStore((s) => s.toggle);
  const isDark = mode === "dark";

  return (
    <Tooltip title={isDark ? "切换到浅色" : "切换到深色"}>
      <Button
        type="text"
        icon={isDark ? <SunOutlined /> : <MoonOutlined />}
        onClick={toggle}
        aria-label="切换主题"
      />
    </Tooltip>
  );
}
