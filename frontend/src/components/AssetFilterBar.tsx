import { Input, Select, Space } from 'antd'
import { SearchOutlined } from '@ant-design/icons'

export interface AssetFilterValues {
  keyword: string
  assetType: string
  // M52: G-UI-AssetFilter 加 status 下拉 (后端 AssetFilter.Status 字段已 ship).
  // Bar 是通用受控组件, statusOptions 跟 typeOptions 同模式由父组件传.
  status: string
}

export interface AssetFilterBarProps {
  value: AssetFilterValues
  onChange: (v: AssetFilterValues) => void
  typeOptions: { value: string; label: string }[]
  statusOptions?: { value: string; label: string }[] // 可选: 父不传则不渲染 status Select
}

/**
 * AssetFilterBar - 资产筛选条（搜索 + 类型 [+ 状态]）。
 * 受控组件，由父组件管理 value。
 */
export function AssetFilterBar({ value, onChange, typeOptions, statusOptions }: AssetFilterBarProps) {
  return (
    <Space wrap>
      <Input
        allowClear
        prefix={<SearchOutlined />}
        placeholder="搜索名称 / IP"
        value={value.keyword}
        onChange={(e) => onChange({ ...value, keyword: e.target.value })}
        style={{ width: 240 }}
      />
      <Select
        allowClear
        placeholder="资产类型"
        value={value.assetType || undefined}
        onChange={(v) => onChange({ ...value, assetType: v ?? '' })}
        options={typeOptions}
        style={{ width: 140 }}
      />
      {statusOptions && (
        <Select
          allowClear
          placeholder="状态"
          value={value.status || undefined}
          onChange={(v) => onChange({ ...value, status: v ?? '' })}
          options={statusOptions}
          style={{ width: 120 }}
        />
      )}
    </Space>
  )
}

export default AssetFilterBar
