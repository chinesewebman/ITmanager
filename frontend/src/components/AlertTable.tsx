import { Button, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { StatusTag } from './StatusTag'

export interface Alert {
  id: string
  host: string
  message: string
  severity: number
  severity_name: string
  status: 'problem' | 'acknowledged' | 'resolved' | string
  created_at: string
  ack_time?: string
}

export interface AlertTableProps {
  data: Alert[]
  loading: boolean
  onAck: (id: string) => Promise<void> | void
  onResolve: (id: string) => Promise<void> | void
  // C-P6: 批量勾选（undefined 时不开启）
  selectedIds?: string[]
  onSelectionChange?: (ids: string[]) => void
}

const SEVERITY_COLOR: Record<number, string> = {
  5: 'red',
  4: 'orange',
  3: 'yellow',
  2: 'blue',
  1: 'default',
}

export function AlertTable({ data, loading, onAck, onResolve, selectedIds, onSelectionChange }: AlertTableProps) {
  const columns: ColumnsType<Alert> = [
    { title: '主机', dataIndex: 'host', key: 'host', width: 150 },
    { title: '告警信息', dataIndex: 'message', key: 'message' },
    {
      title: '级别',
      dataIndex: 'severity_name',
      key: 'severity_name',
      width: 80,
      render: (name: string, record: Alert) => (
        <StatusTag value={String(record.severity)} color={SEVERITY_COLOR[record.severity]} label={name} />
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (s: string) => <StatusTag value={s} />,
    },
    { title: '触发时间', dataIndex: 'created_at', key: 'created_at', width: 160 },
    {
      title: '操作',
      key: 'action',
      width: 200,
      fixed: 'right',
      render: (_, record) => (
        <Space>
          {record.status === 'problem' && (
            <>
              <Button type="link" size="small" onClick={() => onAck(record.id)}>
                确认
              </Button>
              <Button type="link" size="small" onClick={() => onResolve(record.id)}>
                解决
              </Button>
            </>
          )}
          {record.status === 'acknowledged' && (
            <Button type="link" size="small" onClick={() => onResolve(record.id)}>
              解决
            </Button>
          )}
        </Space>
      ),
    },
  ]

  return (
    <Table<Alert>
      rowKey="id"
      columns={columns}
      dataSource={data}
      loading={loading}
      scroll={{ x: 1000 }}
      pagination={{ showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      rowSelection={
        onSelectionChange
          ? {
              selectedRowKeys: selectedIds ?? [],
              onChange: (keys) => onSelectionChange(keys as string[]),
            }
          : undefined
      }
    />
  )
}

export default AlertTable
