import { describe, it, expect, beforeEach } from "vitest";
import {
  useAuthStore,
  useDashboardStore,
  useAlertStore,
  useAssetStore,
  useSiteStore,
  useRackStore,
  useTicketStore,
  useNotificationStore,
  useUIStore,
  useThemeStore,
} from "./index";

// ==================== useAuthStore ====================

describe("useAuthStore", () => {
  beforeEach(() => {
    useAuthStore.setState({ user: null });
  });

  it("初始 user 为 null", () => {
    expect(useAuthStore.getState().user).toBeNull();
  });

  it("setAuth 设 user", () => {
    const user = { id: "1", username: "admin", role: "admin" } as any;
    useAuthStore.getState().setAuth(user);
    expect(useAuthStore.getState().user).toEqual(user);
  });

  it("logout 清空 user", () => {
    useAuthStore.getState().setAuth({ id: "1", username: "admin" } as any);
    useAuthStore.getState().logout();
    expect(useAuthStore.getState().user).toBeNull();
  });

  it("不存 token (C-F5: token 改 httpOnly cookie)", () => {
    // store 接口根本无 token 字段（验证：strict mode 访问会 undefined）
    const state = useAuthStore.getState() as any;
    expect(state.token).toBeUndefined();
  });
});

// ==================== useDashboardStore ====================

describe("useDashboardStore", () => {
  beforeEach(() => {
    useDashboardStore.setState({ stats: null, trends: [], loading: false });
  });

  it("初始 stats=null, trends=[], loading=false", () => {
    const s = useDashboardStore.getState();
    expect(s.stats).toBeNull();
    expect(s.trends).toEqual([]);
    expect(s.loading).toBe(false);
  });

  it("setStats 设 stats 对象", () => {
    const stats = { totalAssets: 100, activeAlerts: 5 } as any;
    useDashboardStore.getState().setStats(stats);
    expect(useDashboardStore.getState().stats).toEqual(stats);
  });

  it("setTrends 追加 trend 数据", () => {
    const trends = [{ date: "2026-06-16", count: 3 }] as any;
    useDashboardStore.getState().setTrends(trends);
    expect(useDashboardStore.getState().trends).toEqual(trends);
  });

  it("setLoading 切 loading 状态", () => {
    useDashboardStore.getState().setLoading(true);
    expect(useDashboardStore.getState().loading).toBe(true);
    useDashboardStore.getState().setLoading(false);
    expect(useDashboardStore.getState().loading).toBe(false);
  });
});

// ==================== useAlertStore ====================

describe("useAlertStore", () => {
  beforeEach(() => {
    useAlertStore.setState({
      alerts: [],
      stats: { total: 0, problem: 0, acknowledged: 0, resolved: 0 },
      loading: false,
      filters: {},
    });
  });

  it("初始 stats 4 个 counter 都是 0", () => {
    const s = useAlertStore.getState();
    expect(s.stats).toEqual({
      total: 0,
      problem: 0,
      acknowledged: 0,
      resolved: 0,
    });
  });

  it("setAlerts 设告警列表", () => {
    const alerts = [{ id: "a1", severity: 5 }] as any;
    useAlertStore.getState().setAlerts(alerts);
    expect(useAlertStore.getState().alerts).toEqual(alerts);
  });

  it("setStats 4 字段统计", () => {
    useAlertStore
      .getState()
      .setStats({ total: 100, problem: 10, acknowledged: 5, resolved: 85 });
    expect(useAlertStore.getState().stats).toEqual({
      total: 100,
      problem: 10,
      acknowledged: 5,
      resolved: 85,
    });
  });

  it("setFilters 设 status/severity", () => {
    useAlertStore.getState().setFilters({ status: "problem", severity: "5" });
    expect(useAlertStore.getState().filters).toEqual({
      status: "problem",
      severity: "5",
    });
  });
});

// ==================== useAssetStore ====================

describe("useAssetStore", () => {
  beforeEach(() => {
    useAssetStore.setState({ assets: [], loading: false, filters: {} });
  });

  it("初始 assets=[]", () => {
    expect(useAssetStore.getState().assets).toEqual([]);
  });

  it("setAssets 设资产列表", () => {
    const assets = [{ id: "a1", name: "web-server" }] as any;
    useAssetStore.getState().setAssets(assets);
    expect(useAssetStore.getState().assets).toEqual(assets);
  });

  it("setFilters 支持 site_id/type/status", () => {
    useAssetStore
      .getState()
      .setFilters({ site_id: "s1", type: "server", status: "active" });
    expect(useAssetStore.getState().filters).toEqual({
      site_id: "s1",
      type: "server",
      status: "active",
    });
  });
});

// ==================== useSiteStore ====================

describe("useSiteStore", () => {
  beforeEach(() => {
    useSiteStore.setState({ sites: [], currentSite: null, loading: false });
  });

  it("初始 sites=[], currentSite=null", () => {
    const s = useSiteStore.getState();
    expect(s.sites).toEqual([]);
    expect(s.currentSite).toBeNull();
  });

  it("setSites 设机房列表", () => {
    const sites = [{ id: "s1", name: "北京机房" }] as any;
    useSiteStore.getState().setSites(sites);
    expect(useSiteStore.getState().sites).toEqual(sites);
  });

  it("setCurrentSite 切到指定机房", () => {
    const site = { id: "s1", name: "北京机房" } as any;
    useSiteStore.getState().setCurrentSite(site);
    expect(useSiteStore.getState().currentSite).toEqual(site);
  });

  it("setCurrentSite(null) 清空当前机房", () => {
    useSiteStore.getState().setCurrentSite({ id: "s1" } as any);
    useSiteStore.getState().setCurrentSite(null);
    expect(useSiteStore.getState().currentSite).toBeNull();
  });
});

// ==================== useRackStore ====================

describe("useRackStore", () => {
  beforeEach(() => {
    useRackStore.setState({ racks: [], currentRack: null, loading: false });
  });

  it("初始 racks=[]", () => {
    expect(useRackStore.getState().racks).toEqual([]);
  });

  it("setRacks 设机柜列表", () => {
    const racks = [{ id: "r1", name: "Rack-A01" }] as any;
    useRackStore.getState().setRacks(racks);
    expect(useRackStore.getState().racks).toEqual(racks);
  });

  it("setCurrentRack 切机柜", () => {
    useRackStore.getState().setCurrentRack({ id: "r1" } as any);
    expect(useRackStore.getState().currentRack).toEqual({ id: "r1" });
  });
});

// ==================== useTicketStore ====================

describe("useTicketStore", () => {
  beforeEach(() => {
    useTicketStore.setState({ tickets: [], loading: false, filters: {} });
  });

  it("初始 tickets=[]", () => {
    expect(useTicketStore.getState().tickets).toEqual([]);
  });

  it("setTickets 做工单列表", () => {
    const tickets = [{ id: "t1", title: "CPU 飙高" }] as any;
    useTicketStore.getState().setTickets(tickets);
    expect(useTicketStore.getState().tickets).toEqual(tickets);
  });

  it("setFilters 支持 status/priority", () => {
    useTicketStore.getState().setFilters({ status: "open", priority: "high" });
    expect(useTicketStore.getState().filters).toEqual({
      status: "open",
      priority: "high",
    });
  });
});

// ==================== useNotificationStore ====================

describe("useNotificationStore", () => {
  beforeEach(() => {
    useNotificationStore.setState({ channels: [], loading: false });
  });

  it("初始 channels=[]", () => {
    expect(useNotificationStore.getState().channels).toEqual([]);
  });

  it("setChannels 设渠道列表", () => {
    const channels = [{ id: "c1", type: "email" }] as any;
    useNotificationStore.getState().setChannels(channels);
    expect(useNotificationStore.getState().channels).toEqual(channels);
  });
});

// ==================== useUIStore ====================

describe("useUIStore", () => {
  beforeEach(() => {
    useUIStore.setState({ collapsed: false });
  });

  it("初始 collapsed=false", () => {
    expect(useUIStore.getState().collapsed).toBe(false);
  });

  it("setCollapsed(true) 折叠侧栏", () => {
    useUIStore.getState().setCollapsed(true);
    expect(useUIStore.getState().collapsed).toBe(true);
  });

  it("setCollapsed(false) 展开侧栏", () => {
    useUIStore.getState().setCollapsed(true);
    useUIStore.getState().setCollapsed(false);
    expect(useUIStore.getState().collapsed).toBe(false);
  });
});

// ==================== Cross-Store 隔离 ====================

describe("store 隔离", () => {
  it("不同 store 互不污染", () => {
    useAuthStore.getState().setAuth({ id: "1" } as any);
    useUIStore.getState().setCollapsed(true);

    expect(useAuthStore.getState().user).toEqual({ id: "1" });
    expect(useUIStore.getState().collapsed).toBe(true);
    expect(useSiteStore.getState().sites).toEqual([]); // site 不受影响
  });
});

// ==================== useThemeStore ====================

describe("useThemeStore", () => {
  beforeEach(() => {
    useThemeStore.setState({ mode: "light" });
    localStorage.removeItem("theme-storage");
  });

  it("初始 mode=light", () => {
    expect(useThemeStore.getState().mode).toBe("light");
  });

  it('setMode("dark") 切换到暗色', () => {
    useThemeStore.getState().setMode("dark");
    expect(useThemeStore.getState().mode).toBe("dark");
  });

  it('setMode("light") 回到浅色', () => {
    useThemeStore.getState().setMode("dark");
    useThemeStore.getState().setMode("light");
    expect(useThemeStore.getState().mode).toBe("light");
  });

  it("toggle() 浅色 → 暗色", () => {
    useThemeStore.getState().toggle();
    expect(useThemeStore.getState().mode).toBe("dark");
  });

  it("toggle() 暗色 → 浅色", () => {
    useThemeStore.getState().setMode("dark");
    useThemeStore.getState().toggle();
    expect(useThemeStore.getState().mode).toBe("light");
  });

  it("主题 store 与其他 store 隔离", () => {
    useUIStore.getState().setCollapsed(true);
    useThemeStore.getState().setMode("dark");

    expect(useUIStore.getState().collapsed).toBe(true);
    expect(useThemeStore.getState().mode).toBe("dark");
    // UI/theme 操作不影响 alert.alerts（初始 []）
    expect(useAlertStore.getState().alerts).toEqual([]);
  });

  it('toggle() 持久化到 localStorage "theme-storage"', () => {
    useThemeStore.getState().toggle();
    const stored = JSON.parse(localStorage.getItem("theme-storage") || "{}");
    expect(stored.state.mode).toBe("dark");
  });

  it("setMode() 持久化到 localStorage", () => {
    useThemeStore.getState().setMode("dark");
    const stored = JSON.parse(localStorage.getItem("theme-storage") || "{}");
    expect(stored.state.mode).toBe("dark");

    useThemeStore.getState().setMode("light");
    const stored2 = JSON.parse(localStorage.getItem("theme-storage") || "{}");
    expect(stored2.state.mode).toBe("light");
  });
});
