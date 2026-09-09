import { useState } from 'react'
import { Table, Button, Space, Popconfirm, message } from 'antd'
import { EditOutlined, DeleteOutlined, ApiOutlined, AimOutlined, FilePdfOutlined, StopOutlined, RollbackOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { assetApi } from '../services/api'
import { StatusTag, statusLabel } from './StatusTag'
import { EmptyState } from './EmptyState'

export interface Asset {
  id: string
  name: string
  asset_type: string
  ip_address: string
  status: string
  site_name?: string
  rack_name?: string
  // B4: 软退役字段
  last_known_ip4?: string | null
  last_known_ip6?: string | null
  retired_at?: string | null
  retired_reason?: string | null
}

// M2：IPv4 按八位组数值排序（字典序会把 192.168.1.10 排在 192.168.1.2 前）；
// 空值排最后；非 IPv4（IPv6 / 非法串）回落 localeCompare。
function ipCompare(a?: string, b?: string): number {
  const na = (a ?? '').trim()
  const nb = (b ?? '').trim()
  if (!na && !nb) return 0
  if (!na) return 1
  if (!nb) return -1
  const pa = na.split('.')
  const pb = nb.split('.')
  const isV4 = (p: string[]) => p.length === 4 && p.every((s) => /^\d+$/.test(s))
  if (isV4(pa) && isV4(pb)) {
    for (let i = 0; i < 4; i++) {
      const d = Number(pa[i]) - Number(pb[i])
      if (d !== 0) return d
    }
    return 0
  }
  return na.localeCompare(nb)
}

export interface AssetTableProps {
  data: Asset[]
  loading: boolean
  onEdit: (asset: Asset) => void
  onChanged: () => void
  onDiagnose?: (asset: Asset, kind: 'ping' | 'traceroute') => void
  onPostmortem?: (asset: Asset) => void
  // B4: 退役/恢复按钮回调 (optional, 不传则不渲染)
  onRetire?: (asset: Asset) => void
  onRestore?: (asset: Asset) => void
  rowSelection?: {
    selectedRowKeys: React.Key[]
    onChange: (keys: React.Key[]) => void
  }
  // M3/P5: 服务端分页受控。total 传入时启用受控分页（current/pageSize/onChange 由父组件持有），
  // 否则回落到 antd 内部分页（前端假分页，仅兼容旧调用方）。
  total?: number
  page?: number
  pageSize?: number
  onPageChange?: (page: number, pageSize: number) => void
}

/**
 * AssetTable - 资产列表展示 + 行内编辑/删除/退役。
 * 父组件持有数据状态和表单弹窗状态。
 */
export function AssetTable({ data, loading, onEdit, onChanged, onDiagnose, onPostmortem, onRetire, onRestore, rowSelection, total, page, pageSize, onPageChange }: AssetTableProps) {
  const [deletingId, setDeletingId] = useState<string | null>(null)

  const handleDelete = async (id: string) => {
    setDeletingId(id)
    try {
      await assetApi.delete(id)
      message.success('删除成功')
      onChanged()
    } catch (e) {
      message.error('删除失败')
    } finally {
      setDeletingId(null)
    }
  }

  const columns: ColumnsType<Asset> = [
    // M2：加前端本地排序。名称/类型/机房/机柜/状态字符串 localeCompare；IP 走 ipCompare
    // 八位组数值序（字典序会错序）；机房/机柜可能为空，空值 ?? ''。
    { title: '资产名称', dataIndex: 'name', key: 'name', width: 180, sorter: (a, b) => a.name.localeCompare(b.name) },
    {
      title: '类型',
      dataIndex: 'asset_type',
      key: 'asset_type',
      width: 100,
      sorter: (a, b) => a.asset_type.localeCompare(b.asset_type),
      render: (t: string) => <StatusTag value={t} />,
    },
    { title: 'IP 地址', dataIndex: 'ip_address', key: 'ip_address', width: 140, sorter: (a, b) => ipCompare(a.ip_address, b.ip_address) },
    { title: '机房', dataIndex: 'site_name', key: 'site_name', width: 100, sorter: (a, b) => (a.site_name ?? '').localeCompare(b.site_name ?? '') },
    { title: '机柜', dataIndex: 'rack_name', key: 'rack_name', width: 100, sorter: (a, b) => (a.rack_name ?? '').localeCompare(b.rack_name ?? '') },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 140,
      sorter: (a, b) => a.status.localeCompare(b.status),
      render: (s: string, r: Asset) => {
        // B4: 退役特殊显示 — 状态 + 历史 IP 提示
        if (s === 'retired') {
          const historyIp = r.last_known_ip4 || r.last_known_ip6 || ''
          return (
            <span>
              <StatusTag value="retired" label="已退役" />
              {historyIp && (
                <span style={{ marginLeft: 6, fontSize: 11, color: 'var(--ant-color-text-secondary)' }}>
                  原 IP: {historyIp}
                </span>
              )}
            </span>
          )
        }
        return <StatusTag value={s} label={statusLabel(s)} />
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 360,
      fixed: 'right',
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" icon={<EditOutlined />} onClick={() => onEdit(record)}>
            编辑
          </Button>
          {onDiagnose && (
            <>
              <Button
                type="link"
                size="small"
                icon={<ApiOutlined />}
                onClick={() => onDiagnose(record, 'ping')}
                disabled={!record.ip_address}
                title={!record.ip_address ? '无 IP 地址' : 'Ping 探活'}
              >
                Ping
              </Button>
              <Button
                type="link"
                size="small"
                icon={<AimOutlined />}
                onClick={() => onDiagnose(record, 'traceroute')}
                disabled={!record.ip_address}
                title={!record.ip_address ? '无 IP 地址' : 'Traceroute 网络路径'}
              >
                Trace
              </Button>
            </>
          )}
          {onPostmortem && (
            <Button
              type="link"
              size="small"
              icon={<FilePdfOutlined />}
              onClick={() => onPostmortem(record)}
              title="下载资产复盘 PDF 报告"
            >
              复盘
            </Button>
          )}
          {/* B4: 退役 / 恢复 (按状态二选一) */}
          {onRetire && record.status !== 'retired' && (
            <Button
              type="link"
              size="small"
              icon={<StopOutlined />}
              onClick={() => onRetire(record)}
              title="软退役：IP 释放给新设备，历史按 hostid 仍可查"
            >
              退役
            </Button>
          )}
          {onRestore && record.status === 'retired' && (
            <Button
              type="link"
              size="small"
              icon={<RollbackOutlined />}
              onClick={() => onRestore(record)}
              title="恢复退役：IP 写回网卡"
            >
              恢复
            </Button>
          )}
          <Popconfirm
            title="确认删除该资产？"
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => handleDelete(record.id)}
          >
            <Button
              type="link"
              size="small"
              danger
              icon={<DeleteOutlined />}
              loading={deletingId === record.id}
            >
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <Table<Asset>
      rowKey="id"
      columns={columns}
      dataSource={data}
      loading={loading}
      rowSelection={rowSelection}
      scroll={{ x: 1000 }}
      pagination={
        total !== undefined
          ? {
              current: page ?? 1,
              pageSize: pageSize ?? 20,
              total,
              showSizeChanger: true,
              showTotal: (t) => `共 ${t} 条`,
              pageSizeOptions: ['10', '20', '50', '100'],
              onChange: (p, ps) => onPageChange?.(p, ps),
            }
          : {
              showSizeChanger: true,
              showTotal: (t) => `共 ${t} 条`,
              pageSizeOptions: ['10', '20', '50', '100'],
            }
      }
      locale={{
        emptyText: (
          <EmptyState
            title="暂无资产"
            description="点击新建或导入，开始管理资产"
            compact
          />
        ),
      }}
    />
  )
}

export default AssetTable
