import { useState, useEffect } from 'react'
import { Layout, Menu, theme, Dropdown, Avatar, Space } from 'antd'
import type { MenuProps } from 'antd'
import {
  DashboardOutlined,
  DesktopOutlined,
  AlertOutlined,
  BuildOutlined,
  FolderOutlined,
  SettingOutlined,
  UserOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import { BrowserRouter, Routes, Route, Navigate, useLocation, useNavigate } from 'react-router-dom'
import Dashboard from './pages/Dashboard'
import Assets from './pages/Assets'
import Alerts from './pages/Alerts'
import Racks from './pages/Racks'
import Tickets from './pages/Tickets'
import Settings from './pages/Settings'
import Login from './pages/Login'
import { authApi } from './services/api'

const { Header, Sider, Content } = Layout

interface UserInfo {
  id: string
  username: string
  nickname: string
  role: string
  avatar?: string
}

function AppLayout() {
  const [collapsed, setCollapsed] = useState(false)
  const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
  const location = useLocation()
  const navigate = useNavigate()
  const {
    token: { colorBgContainer, borderRadiusLG },
  } = theme.useToken()

  useEffect(() => {
    // 从 localStorage 获取用户信息
    const userStr = localStorage.getItem('user')
    if (userStr) {
      setUserInfo(JSON.parse(userStr))
    }
  }, [])

  const menuItems = [
    { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
    { key: '/assets', icon: <DesktopOutlined />, label: '资产管理' },
    { key: '/alerts', icon: <AlertOutlined />, label: '告警中心' },
    { key: '/racks', icon: <FolderOutlined />, label: '机房机柜' },
    { key: '/tickets', icon: <BuildOutlined />, label: '工单管理' },
    { key: '/settings', icon: <SettingOutlined />, label: '系统设置' },
  ]

  const handleMenuClick = (e: { key: string }) => {
    navigate(e.key)
  }

  const handleLogout = async () => {
    try {
      await authApi.logout()
    } catch (e) {
      // 忽略登出错误
    }
    // C-F5: token 已在后端 cookie 中清掉；只清前端 user 缓存
    localStorage.removeItem('user')
    navigate('/login')
  }

  const userMenuItems: MenuProps['items'] = [
    {
      key: 'profile',
      icon: <UserOutlined />,
      label: userInfo?.nickname || userInfo?.username || '用户',
      disabled: true,
    },
    { type: 'divider' },
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: '退出登录',
      onClick: handleLogout,
    },
  ]

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={(value) => setCollapsed(value)}
        theme="dark"
      >
        <div style={{
          height: 32,
          margin: 16,
          background: 'rgba(255, 255, 255, 0.2)',
          borderRadius: 6,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#fff',
          fontWeight: 'bold',
        }}>
          {collapsed ? 'IT' : '网络运维监控平台'}
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
        <Header style={{ padding: '0 24px', background: colorBgContainer, display: 'flex', justifyContent: 'flex-end', alignItems: 'center' }}>
          <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
            <Space style={{ cursor: 'pointer' }}>
              <Avatar icon={<UserOutlined />} />
              <span>{userInfo?.nickname || userInfo?.username || '管理员'}</span>
            </Space>
          </Dropdown>
        </Header>
        <Content style={{ margin: 16 }}>
          <div
            style={{
              padding: 24,
              minHeight: '100%',
              background: colorBgContainer,
              borderRadius: borderRadiusLG,
            }}
          >
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/assets" element={<Assets />} />
              <Route path="/alerts" element={<Alerts />} />
              <Route path="/racks" element={<Racks />} />
              <Route path="/tickets" element={<Tickets />} />
              <Route path="/settings" element={<Settings />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}

function App() {
  const [isLoggedIn, setIsLoggedIn] = useState(false)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    // C-F5: 不再读 localStorage token（token 在 httpOnly cookie 中）
    // 仅靠 user 缓存判断（user 无敏感信息，可放 localStorage）
    const userStr = localStorage.getItem('user')
    setIsLoggedIn(!!userStr)
    setLoading(false)
  }, [])

  if (loading) {
    return null
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={isLoggedIn ? <Navigate to="/" replace /> : <Login />} />
        <Route path="/*" element={isLoggedIn ? <AppLayout /> : <Navigate to="/login" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
