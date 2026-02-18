import { useEffect, useState } from 'react'
import { Table, Tag, Button, Space, Select, Card, Row, Col, message } from 'antd'
import { CheckOutlined, CheckCircleOutlined, SyncOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { alertApi } from '../services/api'

interface Alert {
  id: string
  host: string
  message: string
  severity: number
  severity_name: string
  status: string
  created_at: string
  ack_time?: string
}

function Alerts() {
  const [data, setData] = useState<Alert[]>([])
  const [loading, setLoading] = useState(false)
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [severityFilter, setSeverityFilter] = useState<string>('')
  const [stats, setStats] = useState({ total: 0, problem: 0, acknowledged: 0, resolved: 0 })
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([])

  const fetchData = async () => {
    setLoading(true)
    try {
      const params: { status?: string; severity?: string } = {}
      if (statusFilter) params.status = statusFilter
      if (severityFilter) params.severity = severityFilter

      const res = await alertApi.list(params)
      setData(res.data.data.items || [])
      setStats(res.data.data.stats || { total: 0, problem: 0, acknowledged: 0, resolved: 0 })
    } catch (error) {
      console.error('获取告警列表失败:', error)
      setData([
        { id: '1', host: 'web-server-01', message: 'CPU使用率超过90%', severity: 5, severity_name: '灾难', status: 'problem', created_at: '2026-02-14 10:00:00' },
        { id: '2', host: 'db-server-02', message: '磁盘空间不足', severity: 4, severity_name: '严重', status: 'problem', created_at: '2026-02-14 09:30:00' },
        { id: '3', host: 'switch-core-01', message: '端口状态异常', severity: 3, severity_name: '一般', status: 'acknowledged', created_at: '2026-02-14 08:00:00', ack_time: '2026-02-14 08:30:00' },
        { id: '4', host: 'firewall-main', message: '连接数超阈值', severity: 3, severity_name: '一般', status: 'resolved', created_at: '2026-02-13 20:00:00' },
      ])
      setStats({ total: 15, problem: 8, acknowledged: 3, resolved: 4 })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [statusFilter, severityFilter])

  const getSeverityColor = (severity: number) => {
    if (severity >= 5) return 'red'
    if (severity >= 4) return 'orange'
    if (severity >= 3) return 'yellow'
    return 'blue'
  }

  const getStatusColor = (status: string) => {
    if (status === 'problem') return 'red'
    if (status === 'acknowledged') return 'orange'
    return 'green'
  }

  const getStatusText = (status: string) => {
    if (status === 'problem') return '未处理'
    if (status === 'acknowledged') return '已确认'
    return '已解决'
  }

  const columns: ColumnsType<Alert> = [
    {
      title: '主机',
      dataIndex: 'host',
      key: 'host',
      width: 150,
    },
    {
      title: '告警信息',
      dataIndex: 'message',
      key: 'message',
    },
    {
      title: '级别',
      dataIndex: 'severity_name',
      key: 'severity_name',
      width: 80,
      render: (name: string, record: Alert) => (
        <Tag color={getSeverityColor(record.severity)}>{name}</Tag>
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
      title: '发生时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 160,
    },
    {
      title: '操作',
      key: 'action',
      width: 160,
      render: (_, record) => (
        <Space>
          {record.status === 'problem' && (
            <Button
              type="link"
              size="small"
              icon={<CheckOutlined />}
              onClick={() => handleAcknowledge(record.id)}
            >
              确认
            </Button>
          )}
          {record.status !== 'resolved' && (
            <Button
              type="link"
              size="small"
              icon={<CheckCircleOutlined />}
              onClick={() => handleResolve(record.id)}
            >
              解决
            </Button>
          )}
        </Space>
      ),
    },
  ]

  const handleAcknowledge = async (id: string) => {
    try {
      await alertApi.acknowledge(id)
      message.success('告警已确认')
      fetchData()
    } catch (error) {
      message.error('操作失败')
    }
  }

  const handleResolve = async (id: string) => {
    try {
      await alertApi.resolve(id)
      message.success('告警已解决')
      fetchData()
    } catch (error) {
      message.error('操作失败')
    }
  }

  const handleBatchAcknowledge = async () => {
    if (selectedRowKeys.length === 0) return
    try {
      await Promise.all(
        selectedRowKeys.map(id =>
          alertApi.acknowledge(id as string)
        )
      )
      message.success(`已确认 ${selectedRowKeys.length} 条告警`)
      setSelectedRowKeys([])
      fetchData()
    } catch (error) {
      message.error('批量操作失败')
    }
  }

  return (
    <div>
      <h2 style={{ marginBottom: 16 }}>告警中心</h2>
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: '#1890ff' }}>{stats.total}</div>
              <div style={{ color: '#999' }}>告警总数</div>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: '#ff4d4f' }}>{stats.problem}</div>
              <div style={{ color: '#999' }}>未处理</div>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: '#faad14' }}>{stats.acknowledged}</div>
              <div style={{ color: '#999' }}>已确认</div>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 'bold', color: '#52c41a' }}>{stats.resolved}</div>
              <div style={{ color: '#999' }}>已解决</div>
            </div>
          </Card>
        </Col>
      </Row>

      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Space>
          <Select
            placeholder="告警状态"
            allowClear
            value={statusFilter}
            onChange={setStatusFilter}
            style={{ width: 120 }}
            options={[
              { label: '未处理', value: 'problem' },
              { label: '已确认', value: 'acknowledged' },
              { label: '已解决', value: 'resolved' },
            ]}
          />
          <Select
            placeholder="告警级别"
            allowClear
            value={severityFilter}
            onChange={setSeverityFilter}
            style={{ width: 120 }}
            options={[
              { label: '灾难', value: '5' },
              { label: '严重', value: '4' },
              { label: '一般', value: '3' },
              { label: '警告', value: '2' },
              { label: '信息', value: '1' },
            ]}
          />
          <Button icon={<SyncOutlined />} onClick={fetchData}>刷新</Button>
        </Space>
        <Button
          type="primary"
          disabled={selectedRowKeys.length === 0}
          onClick={handleBatchAcknowledge}
        >
          批量确认 ({selectedRowKeys.length})
        </Button>
      </div>

      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        rowSelection={{ selectedRowKeys, onChange: setSelectedRowKeys }}
        pagination={{ pageSize: 10, showSizeChanger: true }}
      />
    </div>
  )
}

export default Alerts
