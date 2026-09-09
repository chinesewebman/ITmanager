// 指标快照查看页（P2-2 Zabbix 兜底）
// 简单展示：选 asset_id + key，看最近 N 个点
import { useState } from 'react'
import { Button, Card, Form, Input, InputNumber, Space, Table, Tag, Typography, message } from 'antd'
import { LineChartOutlined } from '@ant-design/icons'
import { useApiQuery } from '../hooks/useApiQuery'
import { apiGet } from '../services/api'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { formatDateTime } from '../utils/time'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

const { Text, Paragraph } = Typography

interface MetricSnapshot {
  id?: string
  asset_id: string
  key: string
  value: number
  ts: string
}

// W1：`catch { return MOCK_LATEST }` 已删除。原写法让 isError 恒 false ——
// 接口挂了页面照常画出 5 个虚构的 cpu.user 采样点，运维会据此判断「CPU 正常」。

export function MetricSnapshotList() {
  const [assetId, setAssetId] = useState('')
  const [key, setKey] = useState('cpu.user')
  const [n, setN] = useState(20)
  const [searchParams, setSearchParams] = useState<{ assetId: string; key: string; n: number } | null>(null)

  useDocumentTitle('指标快照')
  const { data, isLoading, isError, error, refetch } = useApiQuery(
    ['metric-snapshots', 'latest', searchParams],
    async () => {
      if (!searchParams) return []
      const items = await apiGet<MetricSnapshot[]>(`/metric-snapshots/latest?asset_id=${searchParams.assetId}&key=${encodeURIComponent(searchParams.key)}&n=${searchParams.n}`)
      return Array.isArray(items) ? items : []
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
      ) : isError ? (
        <ErrorState error={error} onRetry={refetch} compact />
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
            locale={{
              emptyText: (
                <EmptyState
                  title="暂无指标数据"
                  description="该 asset_id + key 在指定窗口内没有采集点"
                  compact
                />
              ),
            }}
            columns={[
              // M2：加前端本地排序。时间按 Date 解析（RFC3339 字符串字典序会因时区偏移错序）；
              // Asset ID/Key 字符串 localeCompare；Value 数值。
              { title: '时间', dataIndex: 'ts', key: 'ts',
                sorter: (a: MetricSnapshot, b: MetricSnapshot) => new Date(a.ts).getTime() - new Date(b.ts).getTime(),
                // W2：原先用无 locale 的 new Date(v).toLocaleString()，与其它页口径不一致
                render: (v: string) => formatDateTime(v) },
              { title: 'Asset ID', dataIndex: 'asset_id', key: 'asset_id', width: 280,
                sorter: (a: MetricSnapshot, b: MetricSnapshot) => a.asset_id.localeCompare(b.asset_id) },
              { title: 'Key', dataIndex: 'key', key: 'key', width: 140,
                sorter: (a: MetricSnapshot, b: MetricSnapshot) => a.key.localeCompare(b.key) },
              { title: 'Value', dataIndex: 'value', key: 'value', width: 120,
                sorter: (a: MetricSnapshot, b: MetricSnapshot) => a.value - b.value,
                render: v => <Tag color={v > 80 ? 'red' : v > 60 ? 'orange' : 'green'}>{v.toFixed(2)}</Tag> },
            ]}
          />
        </>
      )}
    </div>
  )
}

export default MetricSnapshotList
