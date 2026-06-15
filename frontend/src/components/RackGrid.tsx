import { Card, Col, Row, Tooltip } from 'antd'

export interface Rack {
  id: string
  name: string
  site_id: string
  total_units: number
  used_units: number
}

export interface RackGridProps {
  racks: Rack[]
  selectedRackId?: string
  onSelect: (rack: Rack) => void
}

/**
 * RackGrid - 机柜网格视图，每列一个机柜卡片，U 位可视化占用情况。
 */
export function RackGrid({ racks, selectedRackId, onSelect }: RackGridProps) {
  return (
    <Row gutter={16}>
      {racks.map((rack) => {
        const totalU = rack.total_units || 42
        const usedU = rack.used_units || 0
        const fillRatio = totalU > 0 ? usedU / totalU : 0
        const slots = Array.from({ length: 10 }).map((_, i) => i)
        return (
          <Col span={6} key={rack.id}>
            <Card
              hoverable
              title={rack.name}
              extra={<span style={{ color: '#999' }}>{usedU}/{totalU}U</span>}
              onClick={() => onSelect(rack)}
              style={{ borderColor: selectedRackId === rack.id ? '#1890ff' : '#d9d9d9' }}
            >
              <div
                style={{
                  height: 300,
                  background: '#f5f5f5',
                  borderRadius: 4,
                  padding: 8,
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 2,
                }}
              >
                {slots.map((i) => {
                  const filled = i < Math.floor(fillRatio * 10)
                  const unit = totalU - i * Math.ceil(totalU / 10)
                  return (
                    <Tooltip key={i} title={filled ? `已占用 ${unit}U` : `空闲 ${unit}U`}>
                      <div
                        style={{
                          flex: 1,
                          background: filled ? '#1890ff' : '#e8e8e8',
                          borderRadius: 2,
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          fontSize: 10,
                          color: filled ? '#fff' : '#999',
                        }}
                      >
                        {unit}U
                      </div>
                    </Tooltip>
                  )
                })}
              </div>
            </Card>
          </Col>
        )
      })}
    </Row>
  )
}

export default RackGrid
