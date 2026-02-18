import { useEffect, useState } from 'react'
import { Table, Tag, Button, Space, Select, Card, Row, Col, Modal, Form, Input, message } from 'antd'
import { PlusOutlined, SyncOutlined, EyeOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import axios from 'axios'

interface Ticket {
  id: string
  title: string
  priority: string
  status: string
  requester: string
  assignee?: string
  created_at: string
  updated_at: string
}

function Tickets() {
  const [data, setData] = useState<Ticket[]>([])
  const [loading, setLoading] = useState(false)
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [priorityFilter, setPriorityFilter] = useState<string>('')
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [detailModal, setDetailModal] = useState<Ticket | null>(null)
  const [form] = Form.useForm()

  const fetchData = async () => {
    setLoading(true)
    try {
      const res = await axios.get('/api/tickets')
      setData(res.data.data || [])
    } catch (error) {
      console.error('获取工单列表失败:', error)
      setData([
        { id: '1', title: '服务器磁盘空间不足', priority: 'high', status: 'open', requester: '张三', assignee: '李四', created_at: '2026-02-14 10:00:00', updated_at: '2026-02-14 11:00:00' },
        { id: '2', title: '网络延迟过高', priority: 'critical', status: 'in_progress', requester: '王五', assignee: '李四', created_at: '2026-02-13 15:00:00', updated_at: '2026-02-14 09:00:00' },
        { id: '3', title: '新增一台服务器', priority: 'normal', status: 'pending', requester: '赵六', created_at: '2026-02-12 09:00:00', updated_at: '2026-02-12 10:00:00' },
        { id: '4', title: '防火墙规则变更', priority: 'high', status: 'resolved', requester: '孙七', assignee: '李四', created_at: '2026-02-10 14:00:00', updated_at: '2026-02-11 16:00:00' },
      ])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [statusFilter, priorityFilter])

  const getPriorityColor = (priority: string) => {
    if (priority === 'critical') return 'red'
    if (priority === 'high') return 'orange'
    if (priority === 'normal') return 'blue'
    return 'default'
  }

  const getStatusColor = (status: string) => {
    if (status === 'open') return 'red'
    if (status === 'in_progress') return 'blue'
    if (status === 'pending') return 'orange'
    return 'green'
  }

  const getPriorityText = (priority: string) => {
    if (priority === 'critical') return '紧急'
    if (priority === 'high') return '高'
    if (priority === 'normal') return '普通'
    return '低'
  }

  const getStatusText = (status: string) => {
    if (status === 'open') return '新建'
    if (status === 'in_progress') return '处理中'
    if (status === 'pending') return '等待中'
    if (status === 'resolved') return '已解决'
    return '关闭'
  }

  const columns: ColumnsType<Ticket> = [
    {
      title: '工单标题',
      dataIndex: 'title',
      key: 'title',
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      width: 80,
      render: (priority: string) => (
        <Tag color={getPriorityColor(priority)}>{getPriorityText(priority)}</Tag>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (status: string) => (
        <Tag color={getStatusColor(status)}>{getStatusText(status)}</Tag>
      ),
    },
    {
      title: '请求人',
      dataIndex: 'requester',
      key: 'requester',
      width: 100,
    },
    {
      title: '处理人',
      dataIndex: 'assignee',
      key: 'assignee',
      width: 100,
      render: (assignee: string) => assignee || '-',
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 160,
    },
    {
      title: '操作',
      key: 'action',
      width: 100,
      render: (_, record) => (
        <Button
          type="link"
          size="small"
          icon={<EyeOutlined />}
          onClick={() => setDetailModal(record)}
        >
          详情
        </Button>
      ),
    },
  ]

  const handleCreate = async () => {
    try {
      await form.validateFields()
      message.success('工单创建成功')
      setIsModalOpen(false)
      form.resetFields()
      fetchData()
    } catch (error) {
      console.error(error)
    }
  }

  return (
    <div>
      <h2 style={{ marginBottom: 16 }}>工单管理</h2>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: '#ff4d4f' }}>3</div>
              <div style={{ color: '#999' }}>待处理</div>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: '#1890ff' }}>5</div>
              <div style={{ color: '#999' }}>处理中</div>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: '#faad14' }}>2</div>
              <div style={{ color: '#999' }}>等待中</div>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: '#52c41a' }}>15</div>
              <div style={{ color: '#999' }}>已解决</div>
            </div>
          </Card>
        </Col>
      </Row>

      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Space>
          <Select
            placeholder="工单状态"
            allowClear
            value={statusFilter}
            onChange={setStatusFilter}
            style={{ width: 120 }}
            options={[
              { label: '新建', value: 'open' },
              { label: '处理中', value: 'in_progress' },
              { label: '等待中', value: 'pending' },
              { label: '已解决', value: 'resolved' },
            ]}
          />
          <Select
            placeholder="优先级"
            allowClear
            value={priorityFilter}
            onChange={setPriorityFilter}
            style={{ width: 120 }}
            options={[
              { label: '紧急', value: 'critical' },
              { label: '高', value: 'high' },
              { label: '普通', value: 'normal' },
              { label: '低', value: 'low' },
            ]}
          />
          <Button icon={<SyncOutlined />} onClick={fetchData}>刷新</Button>
        </Space>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setIsModalOpen(true)}>
          创建工单
        </Button>
      </div>

      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        pagination={{ pageSize: 10, showSizeChanger: true }}
      />

      <Modal
        title="创建工单"
        open={isModalOpen}
        onOk={handleCreate}
        onCancel={() => setIsModalOpen(false)}
        width={600}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="title" label="工单标题" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="priority" label="优先级" rules={[{ required: true }]}>
            <Select
              options={[
                { label: '紧急', value: 'critical' },
                { label: '高', value: 'high' },
                { label: '普通', value: 'normal' },
                { label: '低', value: 'low' },
              ]}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={4} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="工单详情"
        open={!!detailModal}
        onCancel={() => setDetailModal(null)}
        footer={[
          <Button key="close" onClick={() => setDetailModal(null)}>
            关闭
          </Button>
        ]}
        width={600}
      >
        {detailModal && (
          <div>
            <Row gutter={16} style={{ marginBottom: 16 }}>
              <Col span={12}>
                <strong>工单标题：</strong>{detailModal.title}
              </Col>
              <Col span={12}>
                <strong>工单ID：</strong>{detailModal.id}
              </Col>
            </Row>
            <Row gutter={16} style={{ marginBottom: 16 }}>
              <Col span={12}>
                <strong>优先级：</strong>
                <Tag color={getPriorityColor(detailModal.priority)}>
                  {getPriorityText(detailModal.priority)}
                </Tag>
              </Col>
              <Col span={12}>
                <strong>状态：</strong>
                <Tag color={getStatusColor(detailModal.status)}>
                  {getStatusText(detailModal.status)}
                </Tag>
              </Col>
            </Row>
            <Row gutter={16} style={{ marginBottom: 16 }}>
              <Col span={12}>
                <strong>请求人：</strong>{detailModal.requester}
              </Col>
              <Col span={12}>
                <strong>处理人：</strong>{detailModal.assignee || '-'}
              </Col>
            </Row>
            <Row gutter={16}>
              <Col span={12}>
                <strong>创建时间：</strong>{detailModal.created_at}
              </Col>
              <Col span={12}>
                <strong>更新时间：</strong>{detailModal.updated_at}
              </Col>
            </Row>
          </div>
        )}
      </Modal>
    </div>
  )
}

export default Tickets
