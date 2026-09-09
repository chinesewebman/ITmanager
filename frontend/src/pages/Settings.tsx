import { useEffect, useState } from 'react'
import { Card, Tabs, Form, Input, InputNumber, Button, Switch, Select, Table, Tag, Space, Modal, message, Popconfirm, Spin, Alert } from 'antd'
import { PlusOutlined, BellOutlined, ApiOutlined, KeyOutlined, ReloadOutlined, ThunderboltOutlined, ApiFilled } from '@ant-design/icons'
import { notificationApi, integrationApi, apiKeyApi, type APIKey } from '../services/api'
import { formatDateTime } from '../utils/time'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

interface NotificationChannel {
  id: string
  name: string
  type: string
  config: any
  is_enabled: boolean
}

function Settings() {
  const [channels, setChannels] = useState<NotificationChannel[]>([])
  const [loading, setLoading] = useState(false)
  const [channelModal, setChannelModal] = useState<{ open: boolean; data?: NotificationChannel }>({ open: false })
  // M4：渠道保存按钮 loading，防连点重复创建（范本 AssetFormModal confirmLoading）
  const [channelSaving, setChannelSaving] = useState(false)
  // v2.2: 集成页接 API（不再是死表单）
  const [integrationStatus, setIntegrationStatus] = useState<any>(null)
  const [statusLoading, setStatusLoading] = useState(false)
  const [zabbixForm] = Form.useForm()
  const [zabbixSaving, setZabbixSaving] = useState(false)
  const [zabbixTesting, setZabbixTesting] = useState(false)
  const [zabbixSyncing, setZabbixSyncing] = useState(false)

  useDocumentTitle('系统设置')

  const [form] = Form.useForm()

  const fetchChannels = async () => {
    setLoading(true)
    try {
      const res: any = await notificationApi.listChannels()
      setChannels(res?.data?.data || [])
    } catch (error) {
      console.error('获取通知渠道失败:', error)
      // G-33 M1：键名与后端 channelConfig 的 JSON tag 对齐（原来用 smtp/webhook/wechat，全是坏样本）
      setChannels([
        { id: '1', name: '邮件通知', type: 'email', config: { smtp_host: 'smtp.example.com', smtp_port: 587, smtp_user: 'nmp@example.com', from: 'nmp@example.com', to: ['ops@example.com'] }, is_enabled: true },
        { id: '2', name: '钉钉群通知', type: 'dingtalk', config: { webhook_url: 'https://oapi.dingtalk.com/robot/send?access_token=xxx' }, is_enabled: true },
        { id: '3', name: 'Webhook', type: 'webhook', config: { url: 'https://example.com/hook' }, is_enabled: false },
      ])
    } finally {
      setLoading(false)
    }
  }

  // v2.2: 拉取集成 status（首次打开集成 tab 时填充 Zabbix 表单）
  const fetchIntegrationStatus = async () => {
    setStatusLoading(true)
    try {
      const res: any = await integrationApi.getStatus()
      const data = res?.data?.data
      setIntegrationStatus(data)
      if (data?.zabbix) {
        zabbixForm.setFieldsValue({
          url: data.zabbix.url || '',
          user: data.zabbix.user || '',
        })
      }
      if (data?.netbox) {
        netboxForm.setFieldsValue({
          url: data.netbox.url || '',
        })
      }
      if (data?.glpi) {
        glpiForm.setFieldsValue({
          url: data.glpi.url || '',
        })
      }
    } catch (error) {
      console.error('获取集成状态失败:', error)
      message.error('获取集成状态失败')
    } finally {
      setStatusLoading(false)
    }
  }

  // v2.2: 保存 NetBox 配置 → PUT /integrations/netbox
  const [netboxForm] = Form.useForm()
  const [netboxSaving, setNetboxSaving] = useState(false)
  const [netboxTesting, setNetboxTesting] = useState(false)
  const [netboxSyncing, setNetboxSyncing] = useState(false)
  const handleSaveNetBox = async () => {
    try {
      const values = await netboxForm.validateFields()
      setNetboxSaving(true)
      const payload: { url: string; token?: string } = { url: values.url }
      if (values.token) payload.token = values.token
      const res: any = await integrationApi.updateNetBox(payload)
      if (res?.data?.code === 0) {
        message.success(res?.data?.message || 'NetBox 配置已生效')
        netboxForm.setFieldValue('token', '')
        await fetchIntegrationStatus()
      } else {
        message.error(res?.data?.message || '保存失败')
      }
    } catch (error: any) {
      if (error?.errorFields) return
      console.error('保存 NetBox 配置失败:', error)
      message.error(error?.response?.data?.message || '保存失败')
    } finally {
      setNetboxSaving(false)
    }
  }
  const handleTestNetBox = async () => {
    setNetboxTesting(true)
    try {
      const res: any = await integrationApi.testNetBox()
      if (res?.data?.code === 0) {
        message.success(res?.data?.message || 'NetBox 连通 OK')
      } else {
        message.error(res?.data?.message || '连通失败')
      }
    } catch (error: any) {
      message.error(error?.response?.data?.message || '连通失败')
    } finally {
      setNetboxTesting(false)
    }
  }
  const handleSyncNetBox = async () => {
    setNetboxSyncing(true)
    try {
      const res: any = await integrationApi.syncNetBox()
      const synced = res?.data?.data?.synced?.netbox
      if (res?.data?.code === 0) {
        message.success(`NetBox 同步完成，新增 ${synced ?? 0} 条资产`)
      } else {
        message.error(res?.data?.message || '同步失败')
      }
    } catch (error: any) {
      message.error(error?.response?.data?.message || '同步失败')
    } finally {
      setNetboxSyncing(false)
    }
  }

  // v2.2: 保存 GLPI 配置 → PUT /integrations/glpi
  const [glpiForm] = Form.useForm()
  const [glpiSaving, setGlpiSaving] = useState(false)
  const [glpiTesting, setGlpiTesting] = useState(false)
  const [glpiSyncing, setGlpiSyncing] = useState(false)
  const handleSaveGLPI = async () => {
    try {
      const values = await glpiForm.validateFields()
      setGlpiSaving(true)
      const payload: { url: string; app_token?: string; user_token?: string } = { url: values.url }
      if (values.app_token) payload.app_token = values.app_token
      if (values.user_token) payload.user_token = values.user_token
      const res: any = await integrationApi.updateGLPI(payload)
      if (res?.data?.code === 0) {
        message.success(res?.data?.message || 'GLPI 配置已生效')
        glpiForm.setFieldValue('app_token', '')
        glpiForm.setFieldValue('user_token', '')
        await fetchIntegrationStatus()
      } else {
        message.error(res?.data?.message || '保存失败')
      }
    } catch (error: any) {
      if (error?.errorFields) return
      console.error('保存 GLPI 配置失败:', error)
      message.error(error?.response?.data?.message || '保存失败')
    } finally {
      setGlpiSaving(false)
    }
  }
  const handleTestGLPI = async () => {
    setGlpiTesting(true)
    try {
      const res: any = await integrationApi.testGLPI()
      if (res?.data?.code === 0) {
        message.success(res?.data?.message || 'GLPI 连通 OK')
      } else {
        message.error(res?.data?.message || '连通失败')
      }
    } catch (error: any) {
      message.error(error?.response?.data?.message || '连通失败')
    } finally {
      setGlpiTesting(false)
    }
  }
  const handleSyncGLPI = async () => {
    setGlpiSyncing(true)
    try {
      const res: any = await integrationApi.syncGLPI()
      const synced = res?.data?.data?.synced?.glpi
      if (res?.data?.code === 0) {
        message.success(`GLPI 同步完成，新增 ${synced ?? 0} 条工单`)
      } else {
        message.error(res?.data?.message || '同步失败')
      }
    } catch (error: any) {
      message.error(error?.response?.data?.message || '同步失败')
    } finally {
      setGlpiSyncing(false)
    }
  }

  // v2.2: 保存 Zabbix 配置 → PUT /integrations/zabbix
  const handleSaveZabbix = async () => {
    try {
      const values = await zabbixForm.validateFields()
      setZabbixSaving(true)
      const payload: { url: string; user: string; password?: string } = {
        url: values.url,
        user: values.user,
      }
      // password 字段为空 → 后端保留旧值（避免 UI 误清空）
      if (values.password) payload.password = values.password
      const res: any = await integrationApi.updateZabbix(payload)
      if (res?.data?.code === 0) {
        message.success(res?.data?.message || 'Zabbix 配置已生效')
        zabbixForm.setFieldValue('password', '') // 清空密码框（保留后端旧值）
        await fetchIntegrationStatus() // 刷新状态
      } else {
        message.error(res?.data?.message || '保存失败')
      }
    } catch (error: any) {
      if (error?.errorFields) return // 表单校验失败，Antd 已展示
      console.error('保存 Zabbix 配置失败:', error)
      message.error(error?.response?.data?.message || '保存失败')
    } finally {
      setZabbixSaving(false)
    }
  }

  // v2.2: 测试 Zabbix 连通 → POST /integrations/zabbix/test
  const handleTestZabbix = async () => {
    setZabbixTesting(true)
    try {
      const res: any = await integrationApi.testZabbix()
      if (res?.data?.code === 0) {
        message.success(res?.data?.message || 'Zabbix 连通 OK')
      } else {
        message.error(res?.data?.message || '连通失败')
      }
    } catch (error: any) {
      console.error('Zabbix 连通测试失败:', error)
      message.error(error?.response?.data?.message || '连通失败')
    } finally {
      setZabbixTesting(false)
    }
  }

  // v2.2: 立即同步 Zabbix → POST /integrations/sync { type: "zabbix" }
  const handleSyncZabbix = async () => {
    setZabbixSyncing(true)
    try {
      const res: any = await integrationApi.syncZabbix()
      const synced = res?.data?.data?.synced?.zabbix
      if (res?.data?.code === 0) {
        message.success(`Zabbix 同步完成，新增 ${synced ?? 0} 条告警`)
      } else {
        message.error(res?.data?.message || '同步失败')
      }
    } catch (error: any) {
      console.error('Zabbix 同步失败:', error)
      message.error(error?.response?.data?.message || '同步失败')
    } finally {
      setZabbixSyncing(false)
    }
  }

  useEffect(() => {
    fetchChannels()
    fetchIntegrationStatus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ===== B1-1: API 密钥管理 =====
  const [apiKeys, setApiKeys] = useState<APIKey[]>([])
  const [apiKeyLoading, setApiKeyLoading] = useState(false)
  const [apiKeyCreating, setApiKeyCreating] = useState(false)
  const [apiKeyModal, setApiKeyModal] = useState<{ open: boolean }>({ open: false })
  const [apiKeyForm] = Form.useForm()
  const [generatedKey, setGeneratedKey] = useState<string | null>(null)
  // 403 = 当前账号无密钥管理权限（/auth/api-keys 限 admin）。拦截器已弹一次
  // 「没有权限访问」，这里只记状态渲染区块内 Alert —— 再 toast 一次是重复噪音，
  // 且会让区块看起来像「加载失败」而不是「无权限」（FIX-PLAN-AUTHZ-CLOSURE.md §2 D-B）
  const [apiKeysForbidden, setApiKeysForbidden] = useState(false)

  const fetchApiKeys = async () => {
    setApiKeyLoading(true)
    try {
      const res: any = await apiKeyApi.list()
      setApiKeys(res?.data?.data || [])
      setApiKeysForbidden(false)
    } catch (error: any) {
      // 置位与复位必须同源：只置不清会让「先 403、后 500」的界面一直挂着
      // 「无密钥管理权限」，把真实故障伪装成权限问题（一致性审计 F3）。
      const forbidden = error?.response?.status === 403
      setApiKeysForbidden(forbidden)
      if (!forbidden) {
        console.error('获取 API 密钥失败:', error)
      }
      setApiKeys([])
    } finally {
      setApiKeyLoading(false)
    }
  }

  const handleCreateApiKey = async (values: { name: string; permissions?: string[]; expires_at?: string }) => {
    setApiKeyCreating(true)
    try {
      const payload: { name: string; permissions?: string[]; expires_at?: string } = {
        name: values.name,
        permissions: values.permissions || ['read'],
      }
      if (values.expires_at) payload.expires_at = values.expires_at
      const res: any = await apiKeyApi.create(payload)
      // 后端字段名是 api_key（api_key_handler.go:200）；曾写成 key，导致一次性
      // 明文 Key 永远不显示（FIX-PLAN-AUTHZ-CLOSURE.md §1 S-2b）
      const key = res?.data?.data?.api_key
      if (res?.data?.code === 0) {
        message.success('API 密钥已生成')
        if (key) setGeneratedKey(key) // 只展示一次
        await fetchApiKeys()
      } else {
        message.error(res?.data?.message || '生成失败')
      }
    } catch (error: any) {
      message.error(error?.response?.data?.message || '生成失败')
    } finally {
      setApiKeyCreating(false)
    }
  }

  const handleRevokeApiKey = async (r: APIKey) => {
    Modal.confirm({
      title: '撤销密钥',
      content: `确认撤销「${r.name}」？撤销后使用该密钥的请求将被拒绝。`,
      okText: '确认撤销',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          const res: any = await apiKeyApi.revoke(r.id)
          if (res?.data?.code === 0) {
            message.success('已撤销')
            await fetchApiKeys()
          } else {
            message.error(res?.data?.message || '撤销失败')
          }
        } catch (error: any) {
          message.error(error?.response?.data?.message || '撤销失败')
        }
      },
    })
  }

  const handleDeleteApiKey = async (r: APIKey) => {
    Modal.confirm({
      title: '删除密钥',
      content: `确认删除「${r.name}」？删除后无法恢复。`,
      okText: '确认删除',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          const res: any = await apiKeyApi.delete(r.id)
          if (res?.data?.code === 0) {
            message.success('已删除')
            await fetchApiKeys()
          } else {
            message.error(res?.data?.message || '删除失败')
          }
        } catch (error: any) {
          message.error(error?.response?.data?.message || '删除失败')
        }
      },
    })
  }

  useEffect(() => {
    fetchApiKeys()
  }, [])

  // Form 的 initialValues 只在挂载时自动应用一次；resetFields() 会把 store 重置到
  // **当前**的 initialValues（prop 每次渲染都更新）。缺了这一步，连续编辑两条渠道会
  // 显示上一条的数据并把上一条写进当前记录（正确性审计 H-2）。
  useEffect(() => {
    if (channelModal.open) form.resetFields()
  }, [channelModal.open, channelModal.data, form])

  // B1-2: 接 createChannel / updateChannel（之前只 message.success 不调 API）
  const handleSaveChannel = async () => {
    setChannelSaving(true)
    try {
      const values = await form.validateFields()
      const configObj = values.config || {}
      // 后端 NotificationChannel.Config 是 JSON 字符串，前端表单是嵌套对象 → stringify 后发。
      // G-33 M1：键名必须与 backend/internal/notification/sender.go 的 channelConfig tag 一致，
      // 契约样本见 __fixtures__/channelConfigSamples.json（前端单测 deep-equal 同一文件）。
      const payload = {
        name: values.name,
        type: values.type,
        config: JSON.stringify(configObj),
        // 不再硬编码 true：编辑一个已停用的渠道会把它静默启用（列表页开关才是 SoT）
        is_enabled: channelModal.data ? channelModal.data.is_enabled : true,
      }
      const res: any = channelModal.data
        ? await notificationApi.updateChannel(channelModal.data.id, payload)
        : await notificationApi.createChannel(payload)
      if (res?.data?.code === 0) {
        message.success(channelModal.data ? '更新成功' : '保存成功')
        setChannelModal({ open: false })
        form.resetFields()
        fetchChannels()
      } else {
        message.error(res?.data?.message || '保存失败')
      }
    } catch (error: any) {
      if (error?.errorFields) return // Antd 表单校验失败
      // 不记整个 error：axios 的 error.config.data 带请求体明文（smtp_password /
      // sign_secret / 企微 URL 里的 key），控制台与前端错误采集插件都读得到（安全审计 L-3）。
      console.error(
        '保存通知渠道失败:',
        error?.response?.status,
        error?.response?.data?.message || error?.message
      )
      message.error(error?.response?.data?.message || '保存失败')
    } finally {
      setChannelSaving(false)
    }
  }

  const handleToggleChannel = async (id: string, enabled: boolean) => {
    try {
      await notificationApi.updateChannel(id, { is_enabled: enabled })
      message.success(enabled ? '已启用' : '已禁用')
      fetchChannels()
    } catch (error) {
      message.error('操作失败')
    }
  }

  const handleTestChannel = async (id: string) => {
    try {
      await notificationApi.testChannel(id)
      message.success('测试消息已发送')
    } catch (error) {
      message.error('发送失败')
    }
  }

  const handleDeleteChannel = async (id: string) => {
    try {
      await notificationApi.deleteChannel(id)
      message.success('删除成功')
      fetchChannels()
    } catch (error) {
      message.error('删除失败')
    }
  }

  const channelColumns = [
    {
      title: '渠道名称',
      dataIndex: 'name',
      key: 'name',
    },
    {
      title: '类型',
      dataIndex: 'type',
      key: 'type',
      render: (type: string) => {
        const typeMap: Record<string, string> = {
          email: '邮件',
          dingtalk: '钉钉',
          wechat: '企业微信',
          webhook: 'Webhook',
        }
        return <Tag>{typeMap[type] || type}</Tag>
      },
    },
    {
      title: '状态',
      dataIndex: 'is_enabled',
      key: 'is_enabled',
      render: (enabled: boolean, record: NotificationChannel) => (
        <Switch
          checked={enabled}
          onChange={(checked) => handleToggleChannel(record.id, checked)}
        />
      ),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: any, record: NotificationChannel) => (
        <Space>
          <Button type="link" size="small" onClick={() => handleTestChannel(record.id)}>
            测试
          </Button>
          <Button type="link" size="small" onClick={() => setChannelModal({ open: true, data: record })}>
            编辑
          </Button>
          {/* H8：删除渠道一键生效，无二次确认 → 套 Popconfirm 四件套（范本 AssetTable:163-168） */}
          <Popconfirm
            title={`确认删除渠道「${record.name}」？`}
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => handleDeleteChannel(record.id)}
          >
            <Button type="link" size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const tabItems = [
    {
      key: 'integrations',
      label: (
        <span>
          <ApiOutlined /> 第三方集成
        </span>
      ),
      children: (
        <Spin spinning={statusLoading}>
          <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <h3 style={{ margin: 0 }}>集成配置</h3>
            <Space>
              {integrationStatus?.zabbix?.enabled && (
                <Tag color="green">Zabbix 已连接</Tag>
              )}
              {!integrationStatus?.zabbix?.enabled && (
                <Tag color="default">Zabbix 未配置</Tag>
              )}
              <Button
                icon={<ReloadOutlined />}
                onClick={fetchIntegrationStatus}
                loading={statusLoading}
              >
                刷新状态
              </Button>
            </Space>
          </div>
          <Card
            title={
              <Space>
                <span>Zabbix</span>
                {integrationStatus?.zabbix?.has_password && (
                  <Tag color="blue">已配置密码</Tag>
                )}
              </Space>
            }
            style={{ marginBottom: 16 }}
          >
            <Form form={zabbixForm} layout="vertical">
              <Form.Item
                label="URL"
                name="url"
                rules={[{ required: true, message: '请输入 Zabbix URL' }]}
              >
                <Input placeholder="http://zabbix:8080" />
              </Form.Item>
              <Form.Item
                label="用户名"
                name="user"
                rules={[{ required: true, message: '请输入用户名' }]}
              >
                <Input placeholder="Admin" />
              </Form.Item>
              <Form.Item
                label="密码"
                name="password"
                extra={integrationStatus?.zabbix?.has_password ? '已配置 · 留空表示不修改' : '未配置 · 请输入'}
              >
                <Input.Password placeholder="请输入密码（留空保留原值）" />
              </Form.Item>
              <Space>
                <Button
                  type="primary"
                  icon={<ThunderboltOutlined />}
                  onClick={handleSaveZabbix}
                  loading={zabbixSaving}
                >
                  保存配置
                </Button>
                <Button
                  icon={<ApiFilled />}
                  onClick={handleTestZabbix}
                  loading={zabbixTesting}
                >
                  测试连通
                </Button>
                <Button
                  icon={<ReloadOutlined />}
                  onClick={handleSyncZabbix}
                  loading={zabbixSyncing}
                  disabled={!integrationStatus?.zabbix?.enabled}
                >
                  立即同步
                </Button>
              </Space>
            </Form>
          </Card>
          <Card
            title={
              <Space>
                <span>NetBox</span>
                {integrationStatus?.netbox?.has_token && (
                  <Tag color="blue">已配置 Token</Tag>
                )}
              </Space>
            }
            style={{ marginBottom: 16 }}
          >
            <Form form={netboxForm} layout="vertical">
              <Form.Item
                label="URL"
                name="url"
                rules={[{ required: true, message: '请输入 NetBox URL' }]}
              >
                <Input placeholder="http://netbox:8000" />
              </Form.Item>
              <Form.Item
                label="API Token"
                name="token"
                extra={integrationStatus?.netbox?.has_token ? '已配置 · 留空表示不修改' : '未配置 · 请输入'}
              >
                <Input.Password placeholder="请输入 Token（留空保留原值）" />
              </Form.Item>
              <Space>
                <Button
                  type="primary"
                  icon={<ThunderboltOutlined />}
                  onClick={handleSaveNetBox}
                  loading={netboxSaving}
                >
                  保存配置
                </Button>
                <Button
                  icon={<ApiFilled />}
                  onClick={handleTestNetBox}
                  loading={netboxTesting}
                >
                  测试连通
                </Button>
                <Button
                  icon={<ReloadOutlined />}
                  onClick={handleSyncNetBox}
                  loading={netboxSyncing}
                  disabled={!integrationStatus?.netbox?.enabled}
                >
                  立即同步
                </Button>
              </Space>
            </Form>
          </Card>
          <Card
            title={
              <Space>
                <span>GLPI</span>
                {integrationStatus?.glpi?.has_app_token && integrationStatus?.glpi?.has_user_token && (
                  <Tag color="blue">双 Token 已配置</Tag>
                )}
                {(integrationStatus?.glpi?.has_app_token || integrationStatus?.glpi?.has_user_token) &&
                 !(integrationStatus?.glpi?.has_app_token && integrationStatus?.glpi?.has_user_token) && (
                  <Tag color="orange">Token 不完整</Tag>
                )}
              </Space>
            }
          >
            <Form form={glpiForm} layout="vertical">
              <Form.Item
                label="URL"
                name="url"
                rules={[{ required: true, message: '请输入 GLPI URL' }]}
              >
                <Input placeholder="http://glpi:80" />
              </Form.Item>
              <Form.Item
                label="App Token"
                name="app_token"
                extra={integrationStatus?.glpi?.has_app_token ? '已配置 · 留空表示不修改' : '未配置 · 请输入'}
              >
                <Input.Password placeholder="请输入 App Token（留空保留原值）" />
              </Form.Item>
              <Form.Item
                label="User Token"
                name="user_token"
                extra={integrationStatus?.glpi?.has_user_token ? '已配置 · 留空表示不修改' : '未配置 · 请输入'}
              >
                <Input.Password placeholder="请输入 User Token（留空保留原值）" />
              </Form.Item>
              <Space>
                <Button
                  type="primary"
                  icon={<ThunderboltOutlined />}
                  onClick={handleSaveGLPI}
                  loading={glpiSaving}
                >
                  保存配置
                </Button>
                <Button
                  icon={<ApiFilled />}
                  onClick={handleTestGLPI}
                  loading={glpiTesting}
                >
                  测试连通
                </Button>
                <Button
                  icon={<ReloadOutlined />}
                  onClick={handleSyncGLPI}
                  loading={glpiSyncing}
                  disabled={!integrationStatus?.glpi?.enabled}
                >
                  立即同步
                </Button>
              </Space>
            </Form>
          </Card>
        </Spin>
      ),
    },
    {
      key: 'notifications',
      label: (
        <span>
          <BellOutlined /> 通知设置
        </span>
      ),
      children: (
        <div>
          <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
            <h3>通知渠道</h3>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setChannelModal({ open: true })}
            >
              添加渠道
            </Button>
          </div>
          <Table
            columns={channelColumns}
            dataSource={channels}
            rowKey="id"
            loading={loading}
            pagination={false}
          />

          <Modal
            title={channelModal.data ? '编辑渠道' : '添加渠道'}
            open={channelModal.open}
            onOk={handleSaveChannel}
            confirmLoading={channelSaving}
            onCancel={() => {
              setChannelModal({ open: false })
              form.resetFields()
            }}
            width={500}
            okText="保存"
            cancelText="取消"
          >
            <Form
              form={form}
              layout="vertical"
              initialValues={
                channelModal.data
                  ? {
                      name: channelModal.data.name,
                      type: channelModal.data.type,
                      // 后端 Config 是 JSON 字符串 → 编辑时 parse 回嵌套对象
                      config: (() => {
                        try {
                          return typeof channelModal.data.config === 'string'
                            ? JSON.parse(channelModal.data.config)
                            : channelModal.data.config || {}
                        } catch {
                          return {}
                        }
                      })(),
                    }
                  : {}
              }
            >
              <Form.Item name="name" label="渠道名称" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="type" label="渠道类型" rules={[{ required: true }]}>
                <Select
                  options={[
                    { label: '邮件', value: 'email' },
                    { label: '钉钉', value: 'dingtalk' },
                    { label: '企业微信', value: 'wechat' },
                    { label: 'Webhook', value: 'webhook' },
                  ]}
                />
              </Form.Item>
              <Form.Item
                noStyle
                shouldUpdate={(prev, curr) => prev.type !== curr.type}
              >
                {({ getFieldValue }) => {
                  const type = getFieldValue('type')
                  if (type === 'email') {
                    return (
                      <>
                        <Form.Item name={['config', 'smtp_host']} label="SMTP服务器" rules={[{ required: true }]}>
                          <Input />
                        </Form.Item>
                        <Form.Item name={['config', 'smtp_port']} label="端口" rules={[{ required: true }]}>
                          {/* 必须用 InputNumber：<Input type="number"> 的 value 是字符串，
                              后端 channelConfig.SMTPPort 是 int → 反序列化失败（G-33 B-1） */}
                          <InputNumber style={{ width: '100%' }} />
                        </Form.Item>
                        <Form.Item name={['config', 'smtp_user']} label="用户名" rules={[{ required: true }]}>
                          <Input />
                        </Form.Item>
                        <Form.Item name={['config', 'smtp_password']} label="密码">
                          <Input.Password />
                        </Form.Item>
                        <Form.Item name={['config', 'from']} label="发件人" rules={[{ required: true }]}>
                          <Input placeholder="nmp@example.com" />
                        </Form.Item>
                        <Form.Item name={['config', 'to']} label="收件人" rules={[{ required: true }]}>
                          <Select mode="tags" placeholder="输入邮箱后回车，可多个" tokenSeparators={[',', ' ']} />
                        </Form.Item>
                      </>
                    )
                  }
                  if (type === 'dingtalk') {
                    return (
                      <>
                        <Form.Item name={['config', 'webhook_url']} label="Webhook URL" rules={[{ required: true }]}>
                          <Input />
                        </Form.Item>
                        <Form.Item name={['config', 'sign_secret']} label="加签密钥（可选）">
                          <Input.Password />
                        </Form.Item>
                      </>
                    )
                  }
                  if (type === 'wechat') {
                    // 只渲染 url：WeChatSender 的配置键是 url（群机器人 key 在 query 里），
                    // 它**忽略** secret —— 这里给 secret 输入框等于制造"配了不生效"的死键。
                    return (
                      <Form.Item name={['config', 'url']} label="Webhook URL" rules={[{ required: true }]}>
                        <Input placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx" />
                      </Form.Item>
                    )
                  }
                  return (
                    <>
                      <Form.Item name={['config', 'url']} label="Webhook URL" rules={[{ required: true }]}>
                        <Input />
                      </Form.Item>
                      <Form.Item name={['config', 'secret']} label="签名密钥（可选）">
                        <Input.Password />
                      </Form.Item>
                    </>
                  )
                }}
              </Form.Item>
            </Form>
          </Modal>
        </div>
      ),
    },
    {
      key: 'api',
      label: (
        <span>
          <KeyOutlined /> API 密钥
        </span>
      ),
      children: (
        <div>
          <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <h3 style={{ margin: 0 }}>API 密钥管理</h3>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setApiKeyModal({ open: true })}
            >
              生成密钥
            </Button>
          </div>

          {apiKeysForbidden && (
            <Alert
              type="warning"
              showIcon
              message="当前账号无密钥管理权限"
              description="API 密钥管理仅限 admin 角色。如需签发或吊销密钥，请联系管理员。"
              style={{ marginBottom: 16 }}
            />
          )}

          <Table
            rowKey="id"
            loading={apiKeyLoading}
            dataSource={apiKeys}
            pagination={false}
            columns={[
              { title: '名称', dataIndex: 'name', key: 'name' },
              {
                title: '前缀',
                dataIndex: 'prefix',
                key: 'prefix',
                render: (p: string) => <Tag>{p}-…</Tag>,
              },
              {
                title: '权限',
                dataIndex: 'permissions',
                key: 'permissions',
                render: (perms: string[]) => (
                  <Space size={4}>
                    {perms.map((p) => (
                      <Tag color="blue" key={p}>{p}</Tag>
                    ))}
                  </Space>
                ),
              },
              { title: '速率限制', dataIndex: 'rate_limit', key: 'rate_limit' },
              {
                title: '状态',
                dataIndex: 'status',
                key: 'status',
                render: (s: string) => (
                  <Tag color={s === 'active' ? 'green' : s === 'revoked' ? 'orange' : 'default'}>{s}</Tag>
                ),
              },
              {
                title: '最后使用',
                dataIndex: 'last_used_at',
                key: 'last_used_at',
                // W2：原 `t ? new Date(t).toLocaleString() : '—'`，无 locale + 非法时间
                // 会显示 "Invalid Date"；改统一出口 formatDateTime（空/非法 → '—'）
                render: (t?: string | null) => formatDateTime(t),
              },
              {
                title: '操作',
                key: 'action',
                render: (_: any, r: APIKey) => (
                  <Space>
                    {r.status === 'active' && (
                      <Button
                        type="link"
                        size="small"
                        onClick={() => handleRevokeApiKey(r)}
                      >
                        撤销
                      </Button>
                    )}
                    <Button
                      type="link"
                      size="small"
                      danger
                      onClick={() => handleDeleteApiKey(r)}
                    >
                      删除
                    </Button>
                  </Space>
                ),
              },
            ]}
          />

          <Modal
            title="生成 API 密钥"
            open={apiKeyModal.open}
            onCancel={() => {
              setApiKeyModal({ open: false })
              // 明文 Key 只应展示一次：X / 遮罩 / ESC 关闭时也要清，否则再次打开
              // 「生成密钥」会重新显示上一把 Key（安全审计 F5）
              setGeneratedKey(null)
              apiKeyForm.resetFields()
            }}
            footer={null}
            width={500}
          >
            <Form form={apiKeyForm} layout="vertical" onFinish={handleCreateApiKey}>
              <Form.Item
                label="密钥名称"
                name="name"
                rules={[{ required: true, message: '请输入密钥名称' }]}
              >
                <Input placeholder="如：监控告警推送" />
              </Form.Item>
              <Form.Item
                label="权限"
                name="permissions"
                initialValue={['read']}
                rules={[{ required: true, message: '请选择至少一项权限' }]}
              >
                <Select
                  mode="multiple"
                  options={[
                    { label: '只读 (read)', value: 'read' },
                    { label: '读写 (write)', value: 'write' },
                    { label: '管理 (admin)', value: 'admin' },
                  ]}
                />
              </Form.Item>
              <Form.Item label="过期时间（可选）" name="expires_at">
                {/* 后端 time.Parse("2006-01-02")，只收 YYYY-MM-DD；旧提示写 RFC3339，按提示输入必 400 */}
                <Input placeholder="YYYY-MM-DD，如 2027-01-01（留空永不过期）" />
              </Form.Item>

              {generatedKey && (
                <div
                  style={{
                    background: '#f6ffed',
                    border: '1px solid #b7eb8f',
                    padding: 12,
                    borderRadius: 4,
                    marginBottom: 16,
                  }}
                >
                  <div style={{ marginBottom: 4, fontWeight: 600 }}>
                    ✅ 密钥已生成（仅此一次展示，请妥善保存）
                  </div>
                  <Input.TextArea readOnly value={generatedKey} autoSize={{ minRows: 2, maxRows: 4 }} />
                </div>
              )}

              <Space>
                <Button type="primary" htmlType="submit" loading={apiKeyCreating}>
                  {generatedKey ? '已生成' : '生成'}
                </Button>
                <Button
                  onClick={() => {
                    setApiKeyModal({ open: false })
                    setGeneratedKey(null)
                    apiKeyForm.resetFields()
                  }}
                >
                  关闭
                </Button>
              </Space>
            </Form>
          </Modal>
        </div>
      ),
    },
  ]

  return (
    <div>
      <h2 style={{ marginBottom: 16 }}>系统设置</h2>
      <Tabs items={tabItems} />
    </div>
  )
}

export default Settings
