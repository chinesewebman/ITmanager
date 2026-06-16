import { lazy, Suspense } from "react";
import { useState, useEffect } from "react";
import {
  Layout,
  Menu,
  theme,
  ConfigProvider,
  Dropdown,
  Avatar,
  Space,
} from "antd";
import type { MenuProps } from "antd";
import zhCN from "antd/locale/zh_CN";
import {
  DashboardOutlined,
  DesktopOutlined,
  AlertOutlined,
  BuildOutlined,
  FolderOutlined,
  SettingOutlined,
  ShareAltOutlined,
  UserOutlined,
  LogoutOutlined,
  BookOutlined,
  LineChartOutlined,
} from "@ant-design/icons";
import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
  useLocation,
  useNavigate,
} from "react-router-dom";
// C-P10: 路由级 code-splitting（每个 page 独立 chunk，首屏只下载用到的）
const Dashboard = lazy(() => import("./pages/Dashboard"));
const Assets = lazy(() => import("./pages/Assets"));
const Alerts = lazy(() => import("./pages/Alerts"));
const Racks = lazy(() => import("./pages/Racks"));
const Tickets = lazy(() => import("./pages/Tickets"));
const Settings = lazy(() => import("./pages/Settings"));
const AssetTimeline = lazy(() => import("./pages/AssetTimeline"));
const AlertSuppressions = lazy(() => import("./pages/AlertSuppressions"));
const Topology = lazy(() => import("./pages/Topology"));
const Oncall = lazy(() => import("./pages/Oncall"));
const Runbook = lazy(() => import("./pages/Runbook"));
const MetricSnapshot = lazy(() => import("./pages/MetricSnapshot"));
import Login from "./pages/Login"; // Login 走 SSR 首屏（无 lazy）
import { ErrorBoundary } from "./components/ErrorBoundary";
import { ThemeSwitcher } from "./components/ThemeSwitcher";
import { CommandPalette } from "./components/CommandPalette";
import { useThemeStore } from "./stores";
import { authApi } from "./services/api";

const { Header, Sider, Content } = Layout;

interface UserInfo {
  id: string;
  username: string;
  nickname: string;
  role: string;
  avatar?: string;
}

function AppLayout() {
  const [collapsed, setCollapsed] = useState(false);
  const [userInfo, setUserInfo] = useState<UserInfo | null>(null);
  const location = useLocation();
  const navigate = useNavigate();
  const {
    token: { colorBgContainer, borderRadiusLG },
  } = theme.useToken();

  useEffect(() => {
    // 从 localStorage 获取用户信息
    const userStr = localStorage.getItem("user");
    if (userStr) {
      setUserInfo(JSON.parse(userStr));
    }
  }, []);

  const menuItems = [
    { key: "/", icon: <DashboardOutlined />, label: "仪表盘" },
    { key: "/assets", icon: <DesktopOutlined />, label: "资产管理" },
    { key: "/alerts", icon: <AlertOutlined />, label: "告警中心" },
    { key: "/alert-suppressions", icon: <AlertOutlined />, label: "告警抑制" },
    { key: "/racks", icon: <FolderOutlined />, label: "机房机柜" },
    { key: "/topology", icon: <ShareAltOutlined />, label: "网络拓扑" },
    { key: "/oncall", icon: <UserOutlined />, label: "值班管理" },
    { key: "/runbooks", icon: <BookOutlined />, label: "故障 Runbook" },
    {
      key: "/metric-snapshots",
      icon: <LineChartOutlined />,
      label: "指标快照",
    },
    { key: "/tickets", icon: <BuildOutlined />, label: "工单管理" },
    { key: "/settings", icon: <SettingOutlined />, label: "系统设置" },
  ];

  const handleMenuClick = (e: { key: string }) => {
    navigate(e.key);
  };

  const handleLogout = async () => {
    try {
      await authApi.logout();
    } catch (e) {
      // 忽略登出错误
    }
    // C-F5: token 已在后端 cookie 中清掉；只清前端 user 缓存
    localStorage.removeItem("user");
    navigate("/login");
  };

  const userMenuItems: MenuProps["items"] = [
    {
      key: "profile",
      icon: <UserOutlined />,
      label: userInfo?.nickname || userInfo?.username || "用户",
      disabled: true,
    },
    { type: "divider" },
    {
      key: "logout",
      icon: <LogoutOutlined />,
      label: "退出登录",
      onClick: handleLogout,
    },
  ];

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={(value) => setCollapsed(value)}
        theme="dark"
      >
        <div
          style={{
            height: 32,
            margin: 16,
            background: "rgba(255, 255, 255, 0.2)",
            borderRadius: 6,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: "#fff",
            fontWeight: "bold",
          }}
        >
          {collapsed ? "IT" : "网络运维监控平台"}
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[location.pathname]}
          items={menuItems}
          onClick={handleMenuClick}
          style={{ fontSize: 14 }}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            padding: "0 24px",
            background: colorBgContainer,
            display: "flex",
            justifyContent: "flex-end",
            alignItems: "center",
            gap: 12,
          }}
        >
          <ThemeSwitcher />
          <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
            <Space style={{ cursor: "pointer" }}>
              <Avatar icon={<UserOutlined />} />
              <span>
                {userInfo?.nickname || userInfo?.username || "管理员"}
              </span>
            </Space>
          </Dropdown>
        </Header>
        <Content style={{ margin: 16 }}>
          <div
            style={{
              padding: 24,
              minHeight: "100%",
              background: colorBgContainer,
              borderRadius: borderRadiusLG,
            }}
          >
            <Suspense
              fallback={
                <div style={{ textAlign: "center", padding: 100 }}>加载中…</div>
              }
            >
              <Routes>
                <Route
                  path="/"
                  element={
                    <ErrorBoundary pageName="仪表盘">
                      <Dashboard />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/assets"
                  element={
                    <ErrorBoundary pageName="资产管理">
                      <Assets />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/assets/:id/diagnostics"
                  element={
                    <ErrorBoundary pageName="资产诊断">
                      <AssetTimeline />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/alerts"
                  element={
                    <ErrorBoundary pageName="告警中心">
                      <Alerts />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/alert-suppressions"
                  element={
                    <ErrorBoundary pageName="告警抑制">
                      <AlertSuppressions />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/racks"
                  element={
                    <ErrorBoundary pageName="机房机柜">
                      <Racks />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/topology"
                  element={
                    <ErrorBoundary pageName="网络拓扑">
                      <Topology />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/oncall"
                  element={
                    <ErrorBoundary pageName="值班管理">
                      <Oncall />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/runbooks"
                  element={
                    <ErrorBoundary pageName="故障 Runbook">
                      <Runbook />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/metric-snapshots"
                  element={
                    <ErrorBoundary pageName="指标快照">
                      <MetricSnapshot />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/tickets"
                  element={
                    <ErrorBoundary pageName="工单管理">
                      <Tickets />
                    </ErrorBoundary>
                  }
                />
                <Route
                  path="/settings"
                  element={
                    <ErrorBoundary pageName="系统设置">
                      <Settings />
                    </ErrorBoundary>
                  }
                />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </Suspense>
          </div>
        </Content>
      </Layout>
    </Layout>
  );
}

function App() {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [loading, setLoading] = useState(true);
  const themeMode = useThemeStore((s) => s.mode);

  useEffect(() => {
    // C-F5: 不再读 localStorage token（token 在 httpOnly cookie 中）
    // 仅靠 user 缓存判断（user 无敏感信息，可放 localStorage）
    const userStr = localStorage.getItem("user");
    setIsLoggedIn(!!userStr);
    setLoading(false);
  }, []);

  if (loading) {
    return null;
  }

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm:
          themeMode === "dark" ? theme.darkAlgorithm : theme.defaultAlgorithm,
      }}
    >
      {/* 小改进 #3：Cmd/Ctrl+K 全局搜索面板（登录前后均可用） */}
      <CommandPalette />
      <BrowserRouter>
        <Routes>
          <Route
            path="/login"
            element={isLoggedIn ? <Navigate to="/" replace /> : <Login />}
          />
          <Route
            path="/*"
            element={
              isLoggedIn ? <AppLayout /> : <Navigate to="/login" replace />
            }
          />
        </Routes>
      </BrowserRouter>
    </ConfigProvider>
  );
}

export default App;
