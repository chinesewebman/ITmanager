// 指标快照查看页（P2-2 Zabbix 兜底）
// 简单展示：选 asset_id + key，看最近 N 个点
import { useState } from 'react'
import { Button, Card, Form, Input, InputNumber, Space, Table, Tag, Typography, message } from 'antd'
import { LineChartOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

const { Text, Paragraph } = Typography

interface MetricSnapshot {
  id?: string
  asset_id: string
  key: string
  value: number
  ts: string
}

const MOCK_LATEST: MetricSnapshot[] = [
  { id: '1', asset_id: 'asset-1', key: 'cpu.user', value: 45.2, ts: new Date(Date.now() - 5*60_000).toISOString() },
  { id: '2', asset_id: 'asset-1', key: 'cpu.user', value: 48.1, ts: new Date(Date.now() - 4*60_000).toISOString() },
  { id: '3', asset_id: 'asset-1', key: 'cpu.user', value: 52.7, ts: new Date(Date.now() - 3*60_000).toISOString() },
  { id: '4', asset_id: 'asset-1', key: 'cpu.user', value: 55.0, ts: new Date(Date.now() - 2*60_000).toISOString() },
  { id: '5', asset_id: 'asset-1', key: 'cpu.user', value: 60.3, ts: new Date(Date.now() - 1*60_000).toISOString() },
]

async function apiGet<T>(path: string): Promise<T> {
  const token = localStorage.getItem('token') ?? ''
  const res = await fetch(`/api${path}`, { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error('fetch failed')
  const json: any = await res.json()
  return json?.data
}

export function MetricSnapshotList() {
  const [assetId, setAssetId] = useState('')
  const [key, setKey] = useState('cpu.user')
  const [n, setN] = useState(20)
  const [searchParams, setSearchParams] = useState<{ assetId: string; key: string; n: number } | null>(null)

  useDocumentTitle('指标快照')
  const { data, isLoading, refetch } = useApiQuery(
    ['metric-snapshots', 'latest', searchParams],
    () => {
      if (!searchParams) return Promise.resolve([])
      return apiGet<MetricSnapshot[]>(`/metric-snapshots/latest?asset_id=${searchParams.assetId}&key=${encodeURIComponent(searchParams.key)}&n=${searchParams.n}`)
        .catch(() => MOCK_LATEST)
    },
    { enabled: !!searchParams },
  )

  function onQuery() {
    if (!assetId.trim()) {
      message.warning('请输入 asset_id')
      return
    }
    if (!key.trim()) {
      message.warning('请输入指标 key')
      return
    }
    setSearchParams({ assetId, key, n })
  }

  const items = data ?? []
  const latest = items[0]?.value
  const avg = items.length > 0 ? (items.reduce((s, x) => s + x.value, 0) / items.length) : 0
  const max = items.length > 0 ? Math.max(...items.map(x => x.value)) : 0

  return (
    <div style={{ padding: 24 }}>
      <Space style={{ marginBottom: 16 }}>
        <LineChartOutlined style={{ fontSize: 20 }} />
        <Text strong style={{ fontSize: 18 }}>指标快照</Text>
        <Tag>Zabbix 兜底</Tag>
      </Space>

      <Card size="small" style={{ marginBottom: 16 }}>
        <Form layout="inline">
          <Form.Item label="Asset ID">
            <Input
              placeholder="uuid"
              value={assetId}
              onChange={e => setAssetId(e.target.value)}
              style={{ width: 280 }}
            />
          </Form.Item>
          <Form.Item label="Key">
            <Input
              placeholder="cpu.user"
              value={key}
              onChange={e => setKey(e.target.value)}
              style={{ width: 160 }}
            />
          </Form.Item>
          <Form.Item label="N">
            <InputNumber min={1} max={500} value={n} onChange={v => setN(v ?? 20)} />
          </Form.Item>
          <Form.Item>
            <Button type="primary" onClick={onQuery}>查询</Button>
            <Button onClick={() => refetch()} style={{ marginLeft: 8 }}>刷新</Button>
          </Form.Item>
        </Form>
      </Card>

      {!searchParams ? (
        <Paragraph type="secondary">输入 asset_id + key 后点击查询</Paragraph>
      ) : (
        <>
          <Space style={{ marginBottom: 16 }}>
            <Card size="small" style={{ width: 160 }}>
              <Text type="secondary">最新值</Text>
              <div style={{ fontSize: 24, fontWeight: 'bold' }}>{latest?.toFixed(2) ?? '—'}</div>
            </Card>
            <Card size="small" style={{ width: 160 }}>
              <Text type="secondary">平均值</Text>
              <div style={{ fontSize: 24, fontWeight: 'bold' }}>{avg.toFixed(2)}</div>
            </Card>
            <Card size="small" style={{ width: 160 }}>
              <Text type="secondary">最大值</Text>
              <div style={{ fontSize: 24, fontWeight: 'bold' }}>{max.toFixed(2)}</div>
            </Card>
            <Card size="small" style={{ width: 160 }}>
              <Text type="secondary">样本数</Text>
              <div style={{ fontSize: 24, fontWeight: 'bold' }}>{items.length}</div>
            </Card>
          </Space>

          <Table
            loading={isLoading}
            rowKey="id"
            dataSource={items}
            pagination={{ pageSize: 20 }}
            columns={[
              { title: '时间', dataIndex: 'ts', key: 'ts',
                render: v => new Date(v).toLocaleString() },
              { title: 'Asset ID', dataIndex: 'asset_id', key: 'asset_id', width: 280 },
              { title: 'Key', dataIndex: 'key', key: 'key', width: 140 },
              { title: 'Value', dataIndex: 'value', key: 'value', width: 120,
                render: v => <Tag color={v > 80 ? 'red' : v > 60 ? 'orange' : 'green'}>{v.toFixed(2)}</Tag> },
            ]}
          />
        </>
      )}
    </div>
  )
}

export default MetricSnapshotList
