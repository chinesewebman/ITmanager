import { useEffect, useState } from 'react'
import { Button, message } from 'antd'
import { SyncOutlined } from '@ant-design/icons'
import { assetApi } from '../services/api'
import { AssetTable, type Asset } from '../components/AssetTable'
import { AssetFormModal, type AssetFormValues } from '../components/AssetFormModal'
import { AssetFilterBar } from '../components/AssetFilterBar'
import { PageHeader } from '../components/PageHeader'

// 资产类型筛选项
const TYPE_OPTIONS = [
  { value: 'server', label: '服务器' },
  { value: 'switch', label: '交换机' },
  { value: 'router', label: '路由器' },
  { value: 'firewall', label: '防火墙' },
  { value: 'storage', label: '存储' },
]

// 模拟数据（API 失败时兜底，与旧版行为一致）
const MOCK_DATA: Asset[] = [
  { id: '1', name: 'web-server-01', asset_type: 'server', ip_address: '192.168.1.10', status: 'active', site_name: '机房A', rack_name: 'Rack-01' },
  { id: '2', name: 'db-server-01', asset_type: 'server', ip_address: '192.168.1.11', status: 'active', site_name: '机房A', rack_name: 'Rack-02' },
  { id: '3', name: 'switch-core-01', asset_type: 'switch', ip_address: '192.168.1.1', status: 'active', site_name: '机房A', rack_name: 'Rack-03' },
  { id: '4', name: 'firewall-main', asset_type: 'firewall', ip_address: '192.168.1.254', status: 'active', site_name: '机房B', rack_name: 'Rack-01' },
  { id: '5', name: 'storage-01', asset_type: 'storage', ip_address: '192.168.2.10', status: 'inactive', site_name: '机房B', rack_name: 'Rack-05' },
]

function Assets() {
  const [data, setData] = useState<Asset[]>([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<Asset | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [filter, setFilter] = useState({ keyword: '', assetType: '' })

  const fetchData = async () => {
    setLoading(true)
    try {
      const res: any = await assetApi.list()
      // C-F11: 后端返回 {code, data: {items,total,page,size}}，
      // axios response 在 api 实例基础上再包一层，所以是 res.data.data.items
      setData(res?.data?.data?.items ?? [])
    } catch (error) {
      console.error('获取资产列表失败:', error)
      setData(MOCK_DATA)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [])

  // 前端过滤（搜索 + 类型）
  const filtered = data.filter((item) => {
    const kw = filter.keyword.toLowerCase()
    const matchKw =
      !kw ||
      item.name.toLowerCase().includes(kw) ||
      item.ip_address.includes(filter.keyword)
    const matchType = !filter.assetType || item.asset_type === filter.assetType
    return matchKw && matchType
  })

  const handleEdit = (asset: Asset) => {
    setEditing(asset)
    setModalOpen(true)
  }

  const handleCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }

  const handleSubmit = async (values: AssetFormValues) => {
    setSubmitting(true)
    try {
      if (editing) {
        await assetApi.update(editing.id, values)
        message.success('资产已更新')
      } else {
        await assetApi.create(values)
        message.success('资产已创建')
      }
      setModalOpen(false)
      fetchData()
    } catch (e) {
      message.error(editing ? '更新失败' : '创建失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div>
      <PageHeader
        title="资产管理"
        subtitle={`共 ${data.length} 台资产`}
        onCreate={handleCreate}
        createText="添加资产"
        extra={
          <Button icon={<SyncOutlined />} onClick={fetchData}>
            刷新
          </Button>
        }
      />
      <div style={{ marginBottom: 16 }}>
        <AssetFilterBar value={filter} onChange={setFilter} typeOptions={TYPE_OPTIONS} />
      </div>
      <AssetTable data={filtered} loading={loading} onEdit={handleEdit} onChanged={fetchData} />
      <AssetFormModal
        open={modalOpen}
        editing={editing}
        submitting={submitting}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />
    </div>
  )
}

export default Assets
