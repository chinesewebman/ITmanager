import { useState, useEffect } from 'react'
import { Form, Input, Button, Card, message, Checkbox, Space, Typography } from 'antd'
import { UserOutlined, LockOutlined, MonitorOutlined } from '@ant-design/icons'
import { useNavigate, useLocation } from 'react-router-dom'
import { authApi } from '../services/api'

const { Title, Text } = Typography

const REMEMBER_KEY = 'itmanager_login_remember'

function Login() {
  const [loading, setLoading] = useState(false)
  const [form] = Form.useForm()
  const navigate = useNavigate()
  const location = useLocation()

  // 从 localStorage 恢复记住的 username
  useEffect(() => {
    const remembered = localStorage.getItem(REMEMBER_KEY)
    if (remembered) {
      try {
        const { username, remember } = JSON.parse(remembered)
        if (remember && username) {
          form.setFieldsValue({ username, remember: true })
        }
      } catch {
        // ignore parse error
      }
    }
  }, [form])

  const onFinish = async (values: { username: string; password: string; remember?: boolean }) => {
    setLoading(true)
    try {
      // C-F5: 后端 set-cookie auth_token（httpOnly, SameSite=Strict）
      // 不再在前端存 token；仅缓存 user 字典
      const response: any = await authApi.login(values)
      const { user, must_change_password } = response.data.data

      localStorage.setItem('user', JSON.stringify(user))

      // 记住密码（仅 username，不存密码）
      if (values.remember) {
        localStorage.setItem(REMEMBER_KEY, JSON.stringify({ username: values.username, remember: true }))
      } else {
        localStorage.removeItem(REMEMBER_KEY)
      }

      // C7: 首次登录强改密 — 跳到改密页 (后端 must_change_password=true)
      // reason=first-login 让 ChangePassword 知道"首次不可跳"
      if (must_change_password) {
        message.warning('检测到您首次登录, 请修改默认密码')
        navigate('/change-password?reason=first-login', { replace: true })
        return
      }

      message.success('登录成功')

      // 登录成功后跳回原页面 (来自 P1-审计 401 重定向 state.from)
      const state = location.state as { from?: string } | null
      const target = state?.from || '/'
      // 用 navigate 保留 history (替代 window.location.href)
      navigate(target, { replace: true })
    } catch (error: any) {
      message.error(error.response?.data?.message || '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #667eea 0%, #764ba2 50%, #f093fb 100%)',
        padding: 16,
      }}
    >
      <Card
        style={{
          width: 420,
          boxShadow: '0 20px 50px rgba(0, 0, 0, 0.25)',
          borderRadius: 12,
          border: 'none',
        }}
        styles={{ body: { padding: '40px 36px 32px' } }}
      >
        {/* Logo + 标题 */}
        <div style={{ textAlign: 'center', marginBottom: 32 }}>
          <div
            style={{
              width: 64,
              height: 64,
              margin: '0 auto 16px',
              borderRadius: 16,
              background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              boxShadow: '0 8px 16px rgba(102, 126, 234, 0.4)',
            }}
          >
            <MonitorOutlined style={{ fontSize: 32, color: '#fff' }} />
          </div>
          <Title level={3} style={{ margin: 0, marginBottom: 4 }}>
            网络运维监控平台
          </Title>
          <Text type="secondary" style={{ fontSize: 13 }}>
            Network Operations Monitor
          </Text>
        </div>

        <Form form={form} name="login" onFinish={onFinish} autoComplete="off" size="large">
          <Form.Item
            name="username"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input
              prefix={<UserOutlined style={{ color: 'rgba(0,0,0,0.45)' }} />}
              placeholder="用户名"
              autoComplete="username"
            />
          </Form.Item>

          <Form.Item
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password
              prefix={<LockOutlined style={{ color: 'rgba(0,0,0,0.45)' }} />}
              placeholder="密码"
              autoComplete="current-password"
            />
          </Form.Item>

          <Form.Item>
            <Space style={{ width: '100%', justifyContent: 'space-between' }}>
              <Form.Item name="remember" valuePropName="checked" noStyle>
                <Checkbox>记住用户名</Checkbox>
              </Form.Item>
              <a style={{ fontSize: 13 }} onClick={(e) => e.preventDefault()} href="#">
                忘记密码？
              </a>
            </Space>
          </Form.Item>

          <Form.Item style={{ marginBottom: 12 }}>
            <Button
              type="primary"
              htmlType="submit"
              loading={loading}
              block
              style={{ height: 44, fontSize: 15, fontWeight: 500 }}
            >
              登录
            </Button>
          </Form.Item>
        </Form>

        <div
          style={{
            textAlign: 'center',
            color: 'rgba(0, 0, 0, 0.45)',
            fontSize: 12,
            padding: '12px 0',
            background: 'rgba(0, 0, 0, 0.02)',
            borderRadius: 6,
            marginTop: 8,
          }}
        >
          默认账号: admin / admin123
        </div>
      </Card>
    </div>
  )
}

export default Login
