import { Input, Select, Space } from 'antd'
import { SearchOutlined } from '@ant-design/icons'

export interface AssetFilterValues {
  keyword: string
  assetType: string
}

export interface AssetFilterBarProps {
  value: AssetFilterValues
  onChange: (v: AssetFilterValues) => void
  typeOptions: { value: string; label: string }[]
}

/**
 * AssetFilterBar - 资产筛选条（搜索 + 类型）。
 * 受控组件，由父组件管理 value。
 */
export function AssetFilterBar({ value, onChange, typeOptions }: AssetFilterBarProps) {
  return (
    <Space>
      <Input
        allowClear
        prefix={<SearchOutlined />}
        placeholder="搜索名称 / 资产标签 / SN"
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
    </Space>
  )
}

export default AssetFilterBar
