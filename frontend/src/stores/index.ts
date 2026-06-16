import { create } from "zustand";
import { persist } from "zustand/middleware";
import type {
  User,
  DashboardStats,
  AlertTrend,
  Alert,
  Asset,
  Site,
  Rack,
  Ticket,
  NotificationChannel,
} from "../types";

// ==================== 认证 Store（C-F5：token 改为 httpOnly cookie，不再存 zustand） ====================
interface AuthState {
  user: User | null;
  setAuth: (user: User) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      setAuth: (user) => {
        // C-F5: 不再存 token；后端已 set-cookie auth_token
        // 仅缓存 user 字典（id/nickname/role/avatar，无敏感）
        set({ user });
      },
      logout: () => {
        set({ user: null });
      },
    }),
    {
      name: "auth-storage",
      partialize: (state) => ({ user: state.user }),
    },
  ),
);

// ==================== 仪表盘 Store ====================
interface DashboardState {
  stats: DashboardStats | null;
  trends: AlertTrend[];
  loading: boolean;
  setStats: (stats: DashboardStats) => void;
  setTrends: (trends: AlertTrend[]) => void;
  setLoading: (loading: boolean) => void;
}

export const useDashboardStore = create<DashboardState>()((set) => ({
  stats: null,
  trends: [],
  loading: false,
  setStats: (stats) => set({ stats }),
  setTrends: (trends) => set({ trends }),
  setLoading: (loading) => set({ loading }),
}));

// ==================== 告警 Store ====================
interface AlertState {
  alerts: Alert[];
  stats: {
    total: number;
    problem: number;
    acknowledged: number;
    resolved: number;
  };
  loading: boolean;
  filters: { status?: string; severity?: string };
  setAlerts: (alerts: Alert[]) => void;
  setStats: (stats: {
    total: number;
    problem: number;
    acknowledged: number;
    resolved: number;
  }) => void;
  setLoading: (loading: boolean) => void;
  setFilters: (filters: { status?: string; severity?: string }) => void;
}

export const useAlertStore = create<AlertState>()((set) => ({
  alerts: [],
  stats: { total: 0, problem: 0, acknowledged: 0, resolved: 0 },
  loading: false,
  filters: {},
  setAlerts: (alerts) => set({ alerts }),
  setStats: (stats) => set({ stats }),
  setLoading: (loading) => set({ loading }),
  setFilters: (filters) => set({ filters }),
}));

// ==================== 资产 Store ====================
interface AssetState {
  assets: Asset[];
  loading: boolean;
  filters: { site_id?: string; type?: string; status?: string };
  setAssets: (assets: Asset[]) => void;
  setLoading: (loading: boolean) => void;
  setFilters: (filters: {
    site_id?: string;
    type?: string;
    status?: string;
  }) => void;
}

export const useAssetStore = create<AssetState>()((set) => ({
  assets: [],
  loading: false,
  filters: {},
  setAssets: (assets) => set({ assets }),
  setLoading: (loading) => set({ loading }),
  setFilters: (filters) => set({ filters }),
}));

// ==================== 机房 Store ====================
interface SiteState {
  sites: Site[];
  currentSite: Site | null;
  loading: boolean;
  setSites: (sites: Site[]) => void;
  setCurrentSite: (site: Site | null) => void;
  setLoading: (loading: boolean) => void;
}

export const useSiteStore = create<SiteState>()((set) => ({
  sites: [],
  currentSite: null,
  loading: false,
  setSites: (sites) => set({ sites }),
  setCurrentSite: (site) => set({ currentSite: site }),
  setLoading: (loading) => set({ loading }),
}));

// ==================== 机柜 Store ====================
interface RackState {
  racks: Rack[];
  currentRack: Rack | null;
  loading: boolean;
  setRacks: (racks: Rack[]) => void;
  setCurrentRack: (rack: Rack | null) => void;
  setLoading: (loading: boolean) => void;
}

export const useRackStore = create<RackState>()((set) => ({
  racks: [],
  currentRack: null,
  loading: false,
  setRacks: (racks) => set({ racks }),
  setCurrentRack: (rack) => set({ currentRack: rack }),
  setLoading: (loading) => set({ loading }),
}));

// ==================== 工单 Store ====================
interface TicketState {
  tickets: Ticket[];
  loading: boolean;
  filters: { status?: string; priority?: string };
  setTickets: (tickets: Ticket[]) => void;
  setLoading: (loading: boolean) => void;
  setFilters: (filters: { status?: string; priority?: string }) => void;
}

export const useTicketStore = create<TicketState>()((set) => ({
  tickets: [],
  loading: false,
  filters: {},
  setTickets: (tickets) => set({ tickets }),
  setLoading: (loading) => set({ loading }),
  setFilters: (filters) => set({ filters }),
}));

// ==================== 通知 Store ====================
interface NotificationState {
  channels: NotificationChannel[];
  loading: boolean;
  setChannels: (channels: NotificationChannel[]) => void;
  setLoading: (loading: boolean) => void;
}

export const useNotificationStore = create<NotificationState>()((set) => ({
  channels: [],
  loading: false,
  setChannels: (channels) => set({ channels }),
  setLoading: (loading) => set({ loading }),
}));

// ==================== UI Store ====================
interface UIState {
  collapsed: boolean;
  setCollapsed: (collapsed: boolean) => void;
}

export const useUIStore = create<UIState>()((set) => ({
  collapsed: false,
  setCollapsed: (collapsed) => set({ collapsed }),
}));

// ==================== 主题 Store ====================
// 持久化到 localStorage 'theme-storage'，刷新后保持
export type ThemeMode = "light" | "dark";
interface ThemeState {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
  toggle: () => void;
}

export const useThemeStore = create<ThemeState>()(
  persist(
    (set, get) => ({
      mode: "light",
      setMode: (mode) => set({ mode }),
      toggle: () => set({ mode: get().mode === "dark" ? "light" : "dark" }),
    }),
    {
      name: "theme-storage",
    },
  ),
);
