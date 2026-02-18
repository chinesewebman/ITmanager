import { useEffect, useState } from 'react'
import { Table, Tag, Button, Space, Input, Select, Modal, Form, message } from 'antd'
import { PlusOutlined, SearchOutlined, EditOutlined, DeleteOutlined, SyncOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { assetApi } from '../services/api'

interface Asset {
  id: string
  name: string
  asset_type: string
  ip_address: string
  status: string
  site_name?: string
  rack_name?: string
}

function Assets() {
  const [data, setData] = useState<Asset[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([])
  const [searchText, setSearchText] = useState('')
  const [typeFilter, setTypeFilter] = useState<string>('')
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [form] = Form.useForm()

  const fetchData = async () => {
    setLoading(true)
    try {
      const res = await assetApi.list()
      setData(res.data.data.items || [])
    } catch (error) {
      console.error('获取资产列表失败:', error)
      // 模拟数据
      setData([
        { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
        { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.11', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
        { id: '3', name: 'switch-core-01', asset_type: 'switch', ip_address: '192.168.1.1', status: 'active', site_name: '机房A', rack_name: 'Rack-03' },
        { id: '4', name: 'firewall-main', asset_type: 'firewall', ip_address: '192.168.1.254', status: 'active', site_name: '机房B', rack_name: 'Rack-01' },
        { id: '5', name: 'storage-01', asset_type: 'storage', ip_address: '192.168.2.10', status: 'inactive', site_name: '机房B', rack_name: 'Rack-05' },
      ])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [])

  const columns: ColumnsType<Asset> = [
    {
      title: '资产名称',
      dataIndex: 'name',
      key: 'name',
      width: 180,
    },
    {
      title: '类型',
      dataIndex: 'asset_type',
      key: 'asset_type',
      width: 100,
      render: (type: string) => {
        const colorMap: Record<string, string> = {
          server: 'blue',
          switch: 'green',
          router: 'cyan',
          firewall: 'red',
          storage: 'purple',
        }
        return <Tag color={colorMap[type] || 'default'}>{type}</Tag>
      },
    },
    {
      title: 'IP地址',
      dataIndex: 'ip_address',
      key: 'ip_address',
      width: 140,
    },
    {
      title: '机房',
      dataIndex: 'site_name',
      key: 'site_name',
      width: 100,
    },
    {
      title: '机柜',
      dataIndex: 'rack_name',
      key: 'rack_name',
      width: 100,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (status: string) => (
        <Tag color={status === 'active' ? 'green' : 'default'}>
          {status === 'active' ? '在线' : '离线'}
        </Tag>
      ),
    },
    {
      title: '操作',
      key: 'action',
      width: 120,
      render: () => (
        <Space size="small">
          <Button type="link" size="small" icon={<EditOutlined />} />
          <Button type="link" size="small" danger icon={<DeleteOutlined />} />
        </Space>
      ),
    },
  ]

  const filteredData = data.filter(item => {
    const matchSearch = item.name.toLowerCase().includes(searchText.toLowerCase()) ||
      item.ip_address.includes(searchText)
    const matchType = !typeFilter || item.asset_type === typeFilter
    return matchSearch && matchType
  })

  const handleAdd = () => {
    form.resetFields()
    setIsModalOpen(true)
  }

  const handleSubmit = async () => {
    try {
      await form.validateFields()
      message.success('添加资产成功')
      setIsModalOpen(false)
      fetchData()
    } catch (error) {
      console.error(error)
    }
  }

  return (
    <div>
      <h2 style={{ marginBottom: 16 }}>资产管理</h2>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Space>
          <Input
            placeholder="搜索资产名称或IP"
            prefix={<SearchOutlined />}
            value={searchText}
            onChange={e => setSearchText(e.target.value)}
            style={{ width: 200 }}
          />
          <Select
            placeholder="选择类型"
            allowClear
            value={typeFilter}
            onChange={setTypeFilter}
            style={{ width: 120 }}
            options={[
              { label: '服务器', value: 'server' },
              { label: '交换机', value: 'switch' },
              { label: '路由器', value: 'router' },
              { label: '防火墙', value: 'firewall' },
              { label: '存储', value: 'storage' },
            ]}
          />
          <Button icon={<SyncOutlined />} onClick={fetchData}>刷新</Button>
        </Space>
        <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
          添加资产
        </Button>
      </div>
      <Table
        columns={columns}
        dataSource={filteredData}
        rowKey="id"
        loading={loading}
        rowSelection={{ selectedRowKeys, onChange: setSelectedRowKeys }}
        pagination={{ pageSize: 10, showSizeChanger: true, showTotal: (total) => `共 ${total} 条` }}
      />

      <Modal
        title="添加资产"
        open={isModalOpen}
        onOk={handleSubmit}
        onCancel={() => setIsModalOpen(false)}
        width={600}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="资产名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="asset_type" label="资产类型" rules={[{ required: true }]}>
            <Select
              options={[
                { label: '服务器', value: 'server' },
                { label: '交换机', value: 'switch' },
                { label: '路由器', value: 'router' },
                { label: '防火墙', value: 'firewall' },
                { label: '存储', value: 'storage' },
              ]}
            />
          </Form.Item>
          <Form.Item name="ip_address" label="IP地址" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="site_id" label="机房">
            <Select
              options={[
                { label: '机房A', value: '1' },
                { label: '机房B', value: '2' },
                { label: '机房C', value: '3' },
              ]}
            />
          </Form.Item>
          <Form.Item name="rack_id" label="机柜">
            <Select
              options={[
                { label: 'Rack-01', value: '1' },
                { label: 'Rack-02', value: '2' },
                { label: 'Rack-03', value: '3' },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

export default Assets
