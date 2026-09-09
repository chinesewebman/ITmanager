import { useApiMutation, useApiQuery, queryKeys } from '../hooks/useApiQuery'
import { Alert, Button, Form, Input, message, Modal, Table, Tag } from 'antd'
import { SyncOutlined, ApiOutlined, AimOutlined } from '@ant-design/icons'
import { assetApi, diagnosticApi, postmortemApi, type PingResult, type TracerouteResult, type TracerouteHop } from '../services/api'
import { AssetTable, type Asset } from '../components/AssetTable'
import { StatusTag, statusLabel } from '../components/StatusTag'
import { AssetFormModal, type AssetFormValues } from '../components/AssetFormModal'
import { useResponsiveTable, MobileCardList } from '../hooks/useResponsiveTable'
import { AssetFilterBar } from '../components/AssetFilterBar'
import { ErrorState } from '../components/ErrorState'
import { PageHeader } from '../components/PageHeader'
import { useState, useCallback } from 'react'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

// 资产类型筛选项
const TYPE_OPTIONS = [
  { value: 'server', label: '服务器' },
  { value: 'switch', label: '交换机' },
  { value: 'router', label: '路由器' },
  { value: 'firewall', label: '防火墙' },
  { value: 'storage', label: '存储' },
]

// W1：假数据兜底已删除（原 MOCK_DATA 让 React Query 的 isError 恒为 false，接口失败渲染一屏假资产）。

function Assets() {
  const [editing, setEditing] = useState<Asset | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [filter, setFilter] = useState({ keyword: '', assetType: '' })
  // M3/P5：服务端分页——page/pageSize 由父组件持有；筛选变化时重置回第 1 页
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

  useDocumentTitle('资产管理')
  const { isMobile } = useResponsiveTable()
  // 诊断 modal 状态
  const [diagOpen, setDiagOpen] = useState(false)
  const [diagAsset, setDiagAsset] = useState<Asset | null>(null)
  const [diagKind, setDiagKind] = useState<'ping' | 'traceroute'>('ping')
  const [diagLoading, setDiagLoading] = useState(false)
  // H10：诊断失败态放 Assets 组件层（destroyOnHidden 的 Modal 内 state 会被清空），
  // 失败时弹窗内渲染 Alert + 重试，避免原 catch 空实现导致的「失败全白」。
  const [diagError, setDiagError] = useState<string | null>(null)
  const [pingResult, setPingResult] = useState<PingResult | null>(null)
  const [traceResult, setTraceResult] = useState<TracerouteResult | null>(null)

  // C-P9: React Query 拉取列表（30s 内不重 fetch）
  // M3/P5：服务端分页——keyword/type/page/page_size 全部下沉到后端（后端契约最完整），
  // 前端不再本地过滤（原 filtered 只对已拉取的 20 条过滤，静默漏掉其它页）。
  // W1：fetcher 只做形状归一（items 非数组 → 空数组、total 缺失 → 0），不吞异常、不回落假数据。
  const { data, isLoading, isError, error, refetch } = useApiQuery<{ items: Asset[]; total: number }>(
    queryKeys.assets.list({ ...filter, page, pageSize }),
    async () => {
      const res: any = await assetApi.list({
        page,
        page_size: pageSize,
        keyword: filter.keyword || undefined,
        type: filter.assetType || undefined,
      })
      const body = res?.data?.data
      return { items: Array.isArray(body?.items) ? body.items : [], total: body?.total ?? 0 }
    },
  )

  // B4: 退役 modal 状态 + form
  const [retireModal, setRetireModal] = useState<{ open: boolean; asset: Asset | null }>({ open: false, asset: null })
  const [retireForm] = Form.useForm<{ reason: string }>()
  const [retireSubmitting, setRetireSubmitting] = useState(false)

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
  const handleDiagnose = useCallback(async (asset: Asset, kind: 'ping' | 'traceroute') => {
    if (!asset.ip_address) {
      message.warning('该资产无 IP 地址')
      return
    }
    setDiagAsset(asset)
    setDiagKind(kind)
    setDiagOpen(true)
    setDiagLoading(true)
    setDiagError(null)
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
    } catch (e: any) {
      // H10：错误已由 axios 拦截器 toast，这里再记到组件层 state 供弹窗内 Alert 展示
      setDiagError(e?.message || '诊断失败，请稍后重试')
    } finally {
      setDiagLoading(false)
    }
  }, [])

  // A-2: 下载复盘 PDF
  const handlePostmortem = useCallback(async (asset: Asset) => {
    try {
      const blob = await postmortemApi.downloadReport(asset.id, 30)
      // 从 Content-Disposition 取文件名（如果有）；fallback 用 asset.name
      const safeName = asset.name.replace(/[^a-zA-Z0-9_\-]/g, '_') || 'asset'
      const filename = `postmortem_${safeName}_${new Date().toISOString().slice(0, 10)}.pdf`
      // 创建下载链接
      const url = window.URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      window.URL.revokeObjectURL(url)
      message.success('复盘报告已下载')
    } catch (e) {
      message.error('下载失败')
    }
  }, [])

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

  // M3/P5：服务端分页后 items/total 直接来自后端，不再前端过滤。
  const items = data?.items ?? []
  const total = data?.total ?? 0

  const handleEdit = useCallback((asset: Asset) => {
    setEditing(asset)
    setModalOpen(true)
  }, [])
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

  // B4: 退役 / 恢复 handler
  const handleRetire = useCallback((asset: Asset) => {
    setRetireModal({ open: true, asset })
    retireForm.resetFields()
  }, [retireForm])
  const handleRetireSubmit = async (values: { reason: string }) => {
    if (!retireModal.asset) return
    setRetireSubmitting(true)
    try {
      const res: any = await assetApi.retire(retireModal.asset.id, values.reason || '')
      const releasedIp = res?.data?.data?.released_ip4 || res?.data?.data?.released_ip6
      message.success(
        releasedIp
          ? `已退役 — IP ${releasedIp} 已释放给新设备使用`
          : '已退役',
      )
      setRetireModal({ open: false, asset: null })
      retireForm.resetFields()
      refetch()
    } catch (e) {
      // axios 拦截器已提示
    } finally {
      setRetireSubmitting(false)
    }
  }
  const handleRestore = useCallback(async (asset: Asset) => {
    Modal.confirm({
      title: '恢复退役',
      content: `确认恢复「${asset.name}」？IP 将写回网卡。`,
      okText: '确认恢复',
      cancelText: '取消',
      onOk: async () => {
        try {
          const res: any = await assetApi.restore(asset.id)
          if (res?.data?.code === 0) {
            message.success('已恢复，IP 已写回')
            refetch()
          } else {
            message.error(res?.data?.message || '恢复失败')
          }
        } catch (e) {
          // axios 拦截器已提示
        }
      },
    })
  }, [refetch])

  // M10：副标题原本用未过滤总数，与表格行数不符
  const hasFilter = Boolean(filter.keyword || filter.assetType)

  // P4：稳定回调引用，使 AssetTable 的 React.memo 生效（否则每渲染新建箭头函数，memo 白搭）
  const handlePageChange = useCallback((p: number, ps: number) => { setPage(p); setPageSize(ps) }, [])
  const handleChanged = useCallback(() => { refetch() }, [refetch])

  return (
    <div>
      <PageHeader
        title="资产管理"
        subtitle={`共 ${total} 台资产${hasFilter ? '（已筛选）' : ''}`}
        onCreate={handleCreate}
        createText="添加资产"
        extra={
          <Button icon={<SyncOutlined />} onClick={() => refetch()}>
            刷新
          </Button>
        }
      />
      {isError ? (
        <ErrorState error={error} onRetry={refetch} />
      ) : (
        <>
          <div style={{ marginBottom: 16 }}>
            <AssetFilterBar
              value={filter}
              onChange={(v) => { setFilter(v); setPage(1) }}
              typeOptions={TYPE_OPTIONS}
            />
          </div>
          {isMobile ? (
            <MobileCardList
              data={items}
              loading={isLoading}
              renderCard={(asset: Asset) => (
                <div>
                  <div style={{ fontWeight: 600, fontSize: 16 }}>{asset.name}</div>
                  <div style={{ color: 'var(--ant-color-text-secondary)', fontSize: 12, marginTop: 4 }}>
                    {asset.asset_type} · {asset.ip_address}
                  </div>
                  <div style={{ marginTop: 8 }}>
                    {/* H9：原 `status === 'active' ? '在线' : '离线'` 把 maintenance 误标红「离线」，
                        改走 StatusTag + statusLabel 与桌面端同一口径 */}
                    <StatusTag value={asset.status} label={statusLabel(asset.status)} />
                    {asset.site_name && <Tag>{asset.site_name}</Tag>}
                    {asset.rack_name && <Tag>{asset.rack_name}</Tag>}
                  </div>
                </div>
              )}
            />
          ) : (
            <AssetTable
              data={items}
              loading={isLoading}
              total={total}
              page={page}
              pageSize={pageSize}
              onPageChange={handlePageChange}
              onEdit={handleEdit}
              onChanged={handleChanged}
              onDiagnose={handleDiagnose}
              onPostmortem={handlePostmortem}
              onRetire={handleRetire}
              onRestore={handleRestore}
            />
          )}
        </>
      )}
      <AssetFormModal
        open={modalOpen}
        editing={editing}
        submitting={submitting}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />

      {/* B4: 软退役 modal (填退役原因) */}
      <Modal
        open={retireModal.open}
        title={`软退役：${retireModal.asset?.name || ''}`}
        onCancel={() => {
          setRetireModal({ open: false, asset: null })
          retireForm.resetFields()
        }}
        footer={null}
        width={520}
        okText="确认退役"
        cancelText="取消"
      >
        <div style={{ marginBottom: 16, padding: 12, background: 'var(--ant-color-bg-layout)', borderRadius: 4 }}>
          <div style={{ fontWeight: 600, marginBottom: 6 }}>⚠️ 退役后将发生：</div>
          <ul style={{ margin: 0, paddingLeft: 20, color: 'var(--ant-color-text-secondary)' }}>
            <li>网卡 IPv4/IPv6 字段被清空</li>
            <li>原 IP 保存到 <code>last_known_ip*</code> 字段</li>
            <li>状态改为 <Tag color="orange">已退役</Tag></li>
            <li>新设备可以使用此 IP，不冲突</li>
            <li>历史数据按 hostid 仍可查</li>
          </ul>
        </div>
        <Form form={retireForm} layout="vertical" onFinish={handleRetireSubmit}>
          <Form.Item
            label="退役原因"
            name="reason"
            rules={[{ required: true, message: '请填写退役原因' }]}
          >
            <Input placeholder="如：设备老化下架 / 业务下线 / 替换新设备" />
          </Form.Item>
          <Form.Item style={{ marginBottom: 0 }}>
            <Button type="primary" danger htmlType="submit" loading={retireSubmitting}>
              确认退役
            </Button>
            <Button
              style={{ marginLeft: 8 }}
              onClick={() => {
                setRetireModal({ open: false, asset: null })
                retireForm.resetFields()
              }}
            >
              取消
            </Button>
          </Form.Item>
        </Form>
      </Modal>

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
        {!diagLoading && diagError && (
          <Alert
            type="error"
            showIcon
            message="诊断失败"
            description={diagError}
            action={
              <Button size="small" onClick={() => diagAsset && handleDiagnose(diagAsset, diagKind)}>
                重试
              </Button>
            }
          />
        )}
        {!diagLoading && !diagError && diagKind === 'ping' && pingResult && (
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
        {!diagLoading && !diagError && diagKind === 'traceroute' && traceResult && (
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
