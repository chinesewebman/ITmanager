import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App";
import "./index.css";

// C-P9: 全局 React Query 客户端
// - staleTime 默认 30s（在 hook 内部调整）
// - 网络错重试 2 次（4xx 不重试，由 hook 内部判断）
// - 不自动 refetch on window focus（避免后台窗口频繁打后端）
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 2,
      refetchOnWindowFocus: false,
      staleTime: 30_000,
    },
  },
});

// 注：ConfigProvider 已迁到 App.tsx 顶层 — 需要根据 useThemeStore.mode
// 动态切换 theme.algorithm（dark vs default），不能在 main.tsx 静态包。
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
);
