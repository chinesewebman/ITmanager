import { useEffect, useMemo, useState } from "react";
import { Modal, Input, List, Tag } from "antd";
import { useNavigate } from "react-router-dom";
import { assetApi, alertApi, ticketApi } from "../../services/api";
import { useApiQuery, queryKeys } from "../../hooks/useApiQuery";
import { fuzzySearch } from "./fuzzy";
import { useGlobalHotkey } from "../../hooks/useGlobalHotkey";

// 跨资源搜索项（小改进 #3：Cmd+K 全局搜索）
// 4 资源：assets / alerts / tickets / runbooks
// 每项有 id / type / title / subtitle / to (跳路由) / tagColor
type SearchType = "asset" | "alert" | "ticket" | "runbook";

interface SearchItem {
  id: string;
  type: SearchType;
  title: string;
  subtitle?: string;
  to: string;
  tagColor: string;
}

// 资源原始数据 shape（最小字段，避免完整 model 耦合）
interface AssetItem {
  id: string;
  name?: string;
  hostname?: string;
  host?: string;
  asset_type?: string;
}
interface AlertItem {
  id: string;
  host?: string;
  trigger_name?: string;
  message?: string;
  severity_name?: string;
}
interface TicketItem {
  id: string;
  title?: string;
  status?: string;
}

const TYPE_LABELS: Record<SearchType, string> = {
  asset: "资产",
  alert: "告警",
  ticket: "工单",
  runbook: "Runbook",
};

const TYPE_PLACEHOLDER = "搜索资产 / 告警 / 工单 (⌘K / Ctrl+K)";

/**
 * 全局搜索面板（Cmd+K / Ctrl+K 触发）
 * - 打开时并行 fetch 3 资源列表（asset / alert / ticket），失败时降级为空
 * - 输入框实时 fuzzy 搜索（前缀/子序列匹配，连续匹配加权）
 * - 上下箭头 + Enter 选择，Esc / 点击遮罩关闭
 * - 选中后跳对应详情页
 */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIdx, setActiveIdx] = useState(0);
  const navigate = useNavigate();

  // Cmd/Ctrl+K 打开
  useGlobalHotkey("k", () => {
    setOpen((v) => !v); // toggle，支持重复按关闭
    setQuery("");
    setActiveIdx(0);
  });

  // Esc 关闭（全局监听，Modal 没暴露 onEsc prop 在 onCancel 里处理）
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  // 小改进 #3 review 修复：改用 React Query 复用 cache
  // 替代原本"打开时 fetch 3 API"，避免每次 Cmd+K 重复请求
  // staleTime 30s → 30s 内不重 fetch，跨 Cmd+K 多次打开复用
  // 注：axios 响应是 r.data = {code, data: {items, stats}}，fetcher 取最里层 items
  const assetsQuery = useApiQuery<AssetItem[]>(
    queryKeys.assets.list({ page: 1, page_size: 50 }),
    () =>
      assetApi
        .list({ page: 1, page_size: 50 })
        .then((r) => (r.data as any)?.data?.items ?? []),
    { staleTime: 30_000 },
  );
  const alertsQuery = useApiQuery<AlertItem[]>(
    queryKeys.alerts.list({}),
    () => alertApi.list({}).then((r) => (r.data as any)?.data?.items ?? []),
    { staleTime: 30_000 },
  );
  const ticketsQuery = useApiQuery<TicketItem[]>(
    queryKeys.tickets.list({}),
    () => ticketApi.list({}).then((r) => (r.data as any)?.data?.items ?? []),
    { staleTime: 30_000 },
  );

  const loading =
    assetsQuery.isLoading || alertsQuery.isLoading || ticketsQuery.isLoading;

  // 合并 3 资源为统一 SearchItem 列表
  const items: SearchItem[] = useMemo(() => {
    return [
      ...(assetsQuery.data ?? []).map((x) => ({
        id: x.id,
        type: "asset" as const,
        title: x.name || x.hostname || x.host || x.id,
        subtitle: x.asset_type,
        to: `/assets/${x.id}`,
        tagColor: "blue",
      })),
      ...(alertsQuery.data ?? []).map((x) => ({
        id: x.id,
        type: "alert" as const,
        title: x.host || x.trigger_name || x.message || x.id,
        subtitle: x.severity_name,
        to: "/alerts",
        tagColor: "red",
      })),
      ...(ticketsQuery.data ?? []).map((x) => ({
        id: x.id,
        type: "ticket" as const,
        title: x.title || x.id,
        subtitle: x.status,
        to: "/tickets",
        tagColor: "green",
      })),
    ];
  }, [assetsQuery.data, alertsQuery.data, ticketsQuery.data]);

  // fuzzy 搜索结果
  const results = useMemo(() => {
    const r = fuzzySearch(
      query,
      items,
      (item) => `${item.title} ${item.subtitle ?? ""}`,
    );
    return r.slice(0, 20);
  }, [query, items]);

  // query 变化时重置 active idx
  useEffect(() => {
    setActiveIdx(0);
  }, [query]);

  const handleSelect = (item: SearchItem) => {
    navigate(item.to);
    setOpen(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIdx((i) => Math.min(i + 1, results.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIdx((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter" && results[activeIdx]) {
      e.preventDefault();
      handleSelect(results[activeIdx].item);
    }
  };

  return (
    <Modal
      open={open}
      footer={null}
      onCancel={() => setOpen(false)}
      width={600}
      closable={false}
      destroyOnHidden
      maskClosable
      // 关动画：Antd RC Motion 在 jsdom 下 css transition 不触发 onTransitionEnd，
      // 导致 destroyOnClose 永远不卸载 DOM；生产环境仍保留 0.2s 动画
      transitionName=""
      maskTransitionName=""
    >
      <Input
        size="large"
        placeholder={TYPE_PLACEHOLDER}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={handleKeyDown}
        autoFocus
      />
      <div
        role="listbox"
        aria-label="全局搜索结果"
        style={{
          maxHeight: 400,
          overflowY: "auto",
          marginTop: 12,
        }}
      >
        <List
          loading={loading}
          dataSource={results}
          locale={{ emptyText: query ? "无匹配结果" : "暂无数据" }}
          renderItem={(r, idx) => {
            const item = r.item;
            const isActive = idx === activeIdx;
            return (
              <List.Item
                role="option"
                aria-selected={isActive}
                onClick={() => handleSelect(item)}
                style={{
                  cursor: "pointer",
                  padding: "8px 12px",
                  background: isActive ? "#e6f4ff" : "transparent",
                  borderRadius: 4,
                }}
                data-testid={`palette-item-${item.type}-${item.id}`}
              >
                <Tag color={item.tagColor}>{TYPE_LABELS[item.type]}</Tag>
                <span
                  style={{
                    marginLeft: 8,
                    fontWeight: isActive ? 600 : 400,
                  }}
                >
                  {item.title}
                </span>
                {item.subtitle && (
                  <span style={{ marginLeft: 8, color: "#999", fontSize: 12 }}>
                    {item.subtitle}
                  </span>
                )}
              </List.Item>
            );
          }}
        />
      </div>
      <div
        style={{
          marginTop: 8,
          fontSize: 12,
          color: "#999",
          textAlign: "right",
        }}
      >
        ↑↓ 选择 · Enter 跳转 · Esc 关闭
      </div>
    </Modal>
  );
}
