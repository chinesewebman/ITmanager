import { useApiMutation, useApiQuery, queryKeys } from '../hooks/useApiQuery'
import { Button, message } from 'antd'
import { SyncOutlined } from '@ant-design/icons'
import { assetApi } from '../services/api'
import { AssetTable, type Asset } from '../components/AssetTable'
import { AssetFormModal, type AssetFormValues } from '../components/AssetFormModal'
import { AssetFilterBar } from '../components/AssetFilterBar'
import { PageHeader } from '../components/PageHeader'
import { useState, useMemo } from 'react'

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
  const [editing, setEditing] = useState<Asset | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [filter, setFilter] = useState({ keyword: '', assetType: '' })

  // C-P9: React Query 拉取列表（30s 内不重 fetch）
  const { data, isLoading, refetch } = useApiQuery<Asset[]>(
    queryKeys.assets.list(),
    async () => {
      const res: any = await assetApi.list()
      return res?.data?.data?.items ?? []
    },
  )

  // 写操作：创建/更新/删除统一 invalidate 列表
  const createMut = useApiMutation((v: AssetFormValues) => assetApi.create(v), {
    onSuccess: () => {
      message.success('资产已创建')
      setModalOpen(false)
      refetch()
    },
    onError: () => message.error('创建失败'),
  })
  const updateMut = useApiMutation(
    ({ id, values }: { id: string; values: AssetFormValues }) => assetApi.update(id, values),
    {
      onSuccess: () => {
        message.success('资产已更新')
        setModalOpen(false)
        refetch()
      },
      onError: () => message.error('更新失败'),
    },
  )
  const submitting = createMut.isPending || updateMut.isPending

  // 前端过滤
  const filtered = useMemo(() => {
    const list = data ?? MOCK_DATA
    return list.filter((item) => {
      const kw = filter.keyword.toLowerCase()
      const matchKw =
        !kw ||
        item.name.toLowerCase().includes(kw) ||
        item.ip_address.includes(filter.keyword)
      const matchType = !filter.assetType || item.asset_type === filter.assetType
      return matchKw && matchType
    })
  }, [data, filter])

  const handleEdit = (asset: Asset) => {
    setEditing(asset)
    setModalOpen(true)
  }
  const handleCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }
  const handleSubmit = (values: AssetFormValues) => {
    if (editing) {
      updateMut.mutate({ id: editing.id, values })
    } else {
      createMut.mutate(values)
    }
  }

  return (
    <div>
      <PageHeader
        title="资产管理"
        subtitle={`共 ${(data ?? MOCK_DATA).length} 台资产`}
        onCreate={handleCreate}
        createText="添加资产"
        extra={
          <Button icon={<SyncOutlined />} onClick={() => refetch()}>
            刷新
          </Button>
        }
      />
      <div style={{ marginBottom: 16 }}>
        <AssetFilterBar value={filter} onChange={setFilter} typeOptions={TYPE_OPTIONS} />
      </div>
      <AssetTable data={filtered} loading={isLoading} onEdit={handleEdit} onChanged={() => refetch()} />
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
