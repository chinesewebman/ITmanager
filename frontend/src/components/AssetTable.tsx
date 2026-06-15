import { useState } from 'react'
import { Table, Button, Space, Popconfirm, message } from 'antd'
import { EditOutlined, DeleteOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { assetApi } from '../services/api'
import { StatusTag } from './StatusTag'

export interface Asset {
  id: string
  name: string
  asset_type: string
  ip_address: string
  status: string
  site_name?: string
  rack_name?: string
}

export interface AssetTableProps {
  data: Asset[]
  loading: boolean
  onEdit: (asset: Asset) => void
  onChanged: () => void
  rowSelection?: {
    selectedRowKeys: React.Key[]
    onChange: (keys: React.Key[]) => void
  }
}

/**
 * AssetTable - 资产列表展示 + 行内编辑/删除。
 * 父组件持有数据状态和表单弹窗状态。
 */
export function AssetTable({ data, loading, onEdit, onChanged, rowSelection }: AssetTableProps) {
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
    { title: '资产名称', dataIndex: 'name', key: 'name', width: 180 },
    {
      title: '类型',
      dataIndex: 'asset_type',
      key: 'asset_type',
      width: 100,
      render: (t: string) => <StatusTag value={t} />,
    },
    { title: 'IP 地址', dataIndex: 'ip_address', key: 'ip_address', width: 140 },
    { title: '机房', dataIndex: 'site_name', key: 'site_name', width: 100 },
    { title: '机柜', dataIndex: 'rack_name', key: 'rack_name', width: 100 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (s: string) => <StatusTag value={s} label={s === 'active' ? '在线' : '离线'} />,
    },
    {
      title: '操作',
      key: 'actions',
      width: 160,
      fixed: 'right',
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" icon={<EditOutlined />} onClick={() => onEdit(record)}>
            编辑
          </Button>
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
      pagination={{
        showSizeChanger: true,
        showTotal: (t) => `共 ${t} 条`,
        pageSizeOptions: ['10', '20', '50', '100'],
      }}
    />
  )
}

export default AssetTable
