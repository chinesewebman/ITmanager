import { Card, Col, Row, Tag, Tooltip } from 'antd'

export interface RackDevice {
  id: string
  name: string
  asset_type: string
  rack_position: number
  health_status: string // green/yellow/red
  alert_count: number
}

export interface RackDeviceListProps {
  devices: RackDevice[]
}

const HEALTH_COLOR: Record<string, string> = {
  green: '#52c41a',
  yellow: '#faad14',
  red: '#ff4d4f',
}

/**
 * RackDeviceList - 机柜内设备列表（弹窗内容）。
 */
export function RackDeviceList({ devices }: RackDeviceListProps) {
  return (
    <Row gutter={[16, 16]}>
      {devices.map((d) => (
        <Col span={24} key={d.id}>
          <Card size="small">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <strong>{d.name}</strong>
                <Tag style={{ marginLeft: 8 }}>{d.asset_type}</Tag>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <Tooltip title={`位置: ${d.rack_position}U`}>
                  <span style={{ color: '#1890ff' }}>{d.rack_position}U</span>
                </Tooltip>
                <span
                  style={{
                    width: 12,
                    height: 12,
                    borderRadius: '50%',
                    background: HEALTH_COLOR[d.health_status] || HEALTH_COLOR.green,
                  }}
                />
                {d.alert_count > 0 && (
                  <span style={{ color: '#ff4d4f', fontSize: 12 }}>{d.alert_count} 告警</span>
                )}
              </div>
            </div>
          </Card>
        </Col>
      ))}
    </Row>
  )
}

export default RackDeviceList
