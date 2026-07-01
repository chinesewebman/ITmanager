import { useEffect, useState } from 'react'
import { Card, Tabs, Form, Input, Button, Switch, Select, Table, Tag, Space, Modal, message, Spin } from 'antd'
import { PlusOutlined, BellOutlined, ApiOutlined, KeyOutlined, ReloadOutlined, ThunderboltOutlined, ApiFilled } from '@ant-design/icons'
import { notificationApi, integrationApi, apiKeyApi, type APIKey } from '../services/api'
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
      setChannels([
        { id: '1', name: '邮件通知', type: 'email', config: { smtp: 'smtp.example.com' }, is_enabled: true },
        { id: '2', name: '钉钉 webhook', type: 'dingtalk', config: { webhook: 'https://oapi.dingtalk.com/robot/send?access_token=xxx' }, is_enabled: true },
        { id: '3', name: '企业微信', type: 'wechat', config: { webhook: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx' }, is_enabled: false },
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

  const fetchApiKeys = async () => {
    setApiKeyLoading(true)
    try {
      const res: any = await apiKeyApi.list()
      setApiKeys(res?.data?.data || [])
    } catch (error) {
      console.error('获取 API 密钥失败:', error)
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
      const key = res?.data?.data?.key
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

  // B1-2: 接 createChannel / updateChannel（之前只 message.success 不调 API）
  const handleSaveChannel = async () => {
    try {
      const values = await form.validateFields()
      const configObj = values.config || {}
      // 后端 NotificationChannel.Config 是 JSON 字符串，前端表单是嵌套对象
      // → stringify 后发；后端 Service.Update/Create 应当接受字符串或对象（看 service 实现）
      const payload = {
        name: values.name,
        type: values.type,
        config: JSON.stringify(configObj),
        is_enabled: true,
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
      console.error('保存通知渠道失败:', error)
      message.error(error?.response?.data?.message || '保存失败')
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
          <Button type="link" size="small" danger onClick={() => handleDeleteChannel(record.id)}>
            删除
          </Button>
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
                        <Form.Item name={['config', 'smtp']} label="SMTP服务器">
                          <Input />
                        </Form.Item>
                        <Form.Item name={['config', 'port']} label="端口">
                          <Input type="number" />
                        </Form.Item>
                        <Form.Item name={['config', 'username']} label="用户名">
                          <Input />
                        </Form.Item>
                        <Form.Item name={['config', 'password']} label="密码">
                          <Input.Password />
                        </Form.Item>
                      </>
                    )
                  }
                  return (
                    <Form.Item name={['config', 'webhook']} label="Webhook URL">
                      <Input />
                    </Form.Item>
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
                render: (t?: string | null) => (t ? new Date(t).toLocaleString() : '—'),
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
                <Input placeholder="RFC3339，如 2027-01-01T00:00:00Z（留空永不过期）" />
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
