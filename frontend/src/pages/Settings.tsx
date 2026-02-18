import { useEffect, useState } from 'react'
import { Card, Tabs, Form, Input, Button, Switch, Select, Table, Tag, Space, Modal, message, Divider } from 'antd'
import { PlusOutlined, BellOutlined, ApiOutlined, KeyOutlined } from '@ant-design/icons'
import axios from 'axios'

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
  const [form] = Form.useForm()

  const fetchChannels = async () => {
    setLoading(true)
    try {
      const res = await axios.get('/api/notification/channels')
      setChannels(res.data.data || [])
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

  useEffect(() => {
    fetchChannels()
  }, [])

  const handleSaveChannel = async () => {
    try {
      await form.validateFields()
      message.success('保存成功')
      setChannelModal({ open: false })
      form.resetFields()
      fetchChannels()
    } catch (error) {
      console.error(error)
    }
  }

  const handleToggleChannel = async (id: string, enabled: boolean) => {
    try {
      await axios.put(`/api/notification/channels/${id}`, { is_enabled: enabled })
      message.success(enabled ? '已启用' : '已禁用')
      fetchChannels()
    } catch (error) {
      message.error('操作失败')
    }
  }

  const handleTestChannel = async (id: string) => {
    try {
      await axios.post(`/api/notification/channels/${id}/test`)
      message.success('测试消息已发送')
    } catch (error) {
      message.error('发送失败')
    }
  }

  const handleDeleteChannel = async (id: string) => {
    try {
      await axios.delete(`/api/notification/channels/${id}`)
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
        <div>
          <h3 style={{ marginBottom: 16 }}>集成配置</h3>
          <Card title="NetBox" style={{ marginBottom: 16 }}>
            <Form layout="vertical">
              <Form.Item label="URL">
                <Input placeholder="http://localhost:8000" defaultValue="http://localhost:8000" />
              </Form.Item>
              <Form.Item label="API Token">
                <Input.Password placeholder="请输入API Token" defaultValue="" />
              </Form.Item>
              <Button type="primary">保存</Button>
            </Form>
          </Card>
          <Card title="Zabbix" style={{ marginBottom: 16 }}>
            <Form layout="vertical">
              <Form.Item label="URL">
                <Input placeholder="http://localhost:8080" defaultValue="http://localhost:8080" />
              </Form.Item>
              <Form.Item label="用户名">
                <Input placeholder="Admin" defaultValue="Admin" />
              </Form.Item>
              <Form.Item label="密码">
                <Input.Password placeholder="请输入密码" />
              </Form.Item>
              <Button type="primary">保存</Button>
            </Form>
          </Card>
          <Card title="GLPI">
            <Form layout="vertical">
              <Form.Item label="URL">
                <Input placeholder="http://localhost" defaultValue="http://localhost" />
              </Form.Item>
              <Form.Item label="API Token">
                <Input.Password placeholder="请输入API Token" />
              </Form.Item>
              <Button type="primary">保存</Button>
            </Form>
          </Card>
        </div>
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
          >
            <Form
              form={form}
              layout="vertical"
              initialValues={channelModal.data || {}}
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
          <h3 style={{ marginBottom: 16 }}>API 密钥管理</h3>
          <Card>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <strong>默认 API 密钥</strong>
                  <div style={{ color: '#999', fontSize: 12 }}>用于系统间集成</div>
                </div>
                <Button>重新生成</Button>
              </div>
              <Divider style={{ margin: '12px 0' }} />
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <strong>只读 API 密钥</strong>
                  <div style={{ color: '#999', fontSize: 12 }}>用于只读访问</div>
                </div>
                <Button type="primary">生成</Button>
              </div>
            </Space>
          </Card>
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
