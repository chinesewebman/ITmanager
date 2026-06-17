import { useApiMutation, useApiQuery, queryKeys } from '../hooks/useApiQuery'
import { Button, message, Modal, Table, Tag } from 'antd'
import { SyncOutlined, ApiOutlined, AimOutlined } from '@ant-design/icons'
import { assetApi, diagnosticApi, type PingResult, type TracerouteResult, type TracerouteHop } from '../services/api'
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
  // 诊断 modal 状态
  const [diagOpen, setDiagOpen] = useState(false)
  const [diagAsset, setDiagAsset] = useState<Asset | null>(null)
  const [diagKind, setDiagKind] = useState<'ping' | 'traceroute'>('ping')
  const [diagLoading, setDiagLoading] = useState(false)
  const [pingResult, setPingResult] = useState<PingResult | null>(null)
  const [traceResult, setTraceResult] = useState<TracerouteResult | null>(null)

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

  // 触发诊断（ping 或 traceroute）
  const handleDiagnose = async (asset: Asset, kind: 'ping' | 'traceroute') => {
    if (!asset.ip_address) {
      message.warning('该资产无 IP 地址')
      return
    }
    setDiagAsset(asset)
    setDiagKind(kind)
    setDiagOpen(true)
    setDiagLoading(true)
    setPingResult(null)
    setTraceResult(null)
    try {
      if (kind === 'ping') {
        const res: any = await diagnosticApi.ping(asset.ip_address, 4)
        setPingResult(res?.data?.data)
      } else {
        const res: any = await diagnosticApi.traceroute(asset.ip_address, 20)
        setTraceResult(res?.data?.data)
      }
    } catch (e) {
      // 错误已由 axios 拦截器提示
    } finally {
      setDiagLoading(false)
    }
  }

  // traceroute 表格列
  const traceColumns = [
    { title: '跳数', dataIndex: 'hop', key: 'hop', width: 60 },
    {
      title: '主机',
      dataIndex: 'host',
      key: 'host',
      render: (_: string, r: TracerouteHop) =>
        r.lossed ? <Tag color="red">* * *</Tag> : (r.host || r.ip || '—'),
    },
    {
      title: 'IP',
      dataIndex: 'ip',
      key: 'ip',
      width: 140,
      render: (ip: string, r: TracerouteHop) => (r.lossed ? '—' : ip || '—'),
    },
    {
      title: 'RTT',
      dataIndex: 'rtts',
      key: 'rtts',
      render: (rtts: string[] | undefined, r: TracerouteHop) =>
        r.lossed ? '—' : (rtts && rtts.length > 0 ? rtts.join(' / ') : '—'),
    },
  ]

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
      <AssetTable
        data={filtered}
        loading={isLoading}
        onEdit={handleEdit}
        onChanged={() => refetch()}
        onDiagnose={handleDiagnose}
      />
      <AssetFormModal
        open={modalOpen}
        editing={editing}
        submitting={submitting}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />

      {/* A-1: 网络诊断 modal（ping / traceroute 结果） */}
      <Modal
        open={diagOpen}
        title={
          <span>
            {diagKind === 'ping' ? <ApiOutlined /> : <AimOutlined />}
            &nbsp;{diagKind === 'ping' ? 'Ping 探活' : 'Traceroute 网络路径'} — {diagAsset?.name} ({diagAsset?.ip_address})
          </span>
        }
        onCancel={() => setDiagOpen(false)}
        footer={<Button onClick={() => setDiagOpen(false)}>关闭</Button>}
        width={diagKind === 'traceroute' ? 720 : 520}
        destroyOnHidden
      >
        {diagLoading && <div style={{ padding: 24, textAlign: 'center' }}>执行中…</div>}
        {!diagLoading && diagKind === 'ping' && pingResult && (
          <div>
            <p>
              <b>目标：</b>{pingResult.host} &nbsp; <b>包：</b>{pingResult.transmitted} 发送 / {pingResult.received} 接收 &nbsp; <b>丢包率：</b>
              <span style={{ color: pingResult.loss_percent > 0 ? 'red' : 'green' }}>{pingResult.loss_percent}%</span>
            </p>
            {pingResult.received > 0 ? (
              <p>
                <b>RTT：</b>min {pingResult.min_ms} ms / avg {pingResult.avg_ms} ms / max {pingResult.max_ms} ms
                {pingResult.stddev_ms != null && ` / stddev ${pingResult.stddev_ms} ms`}
              </p>
            ) : (
              <p style={{ color: 'red' }}>无响应，目标不可达</p>
            )}
            <p style={{ color: '#999', fontSize: 12 }}>耗时 {pingResult.duration_ms} ms</p>
          </div>
        )}
        {!diagLoading && diagKind === 'traceroute' && traceResult && (
          <div>
            <p>
              <b>目标：</b>{traceResult.host} &nbsp; <b>最大跳数：</b>{traceResult.max_hops} &nbsp;
              <b>到达：</b>
              <span style={{ color: traceResult.reached ? 'green' : 'red' }}>{traceResult.reached ? '是' : '否'}</span> &nbsp;
              <b>耗时：</b>{traceResult.duration_ms} ms
            </p>
            <Table<TracerouteHop>
              rowKey="hop"
              dataSource={traceResult.hops}
              columns={traceColumns}
              pagination={false}
              size="small"
            />
          </div>
        )}
      </Modal>
    </div>
  )
}

export default Assets
