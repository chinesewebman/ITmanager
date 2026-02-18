import { useEffect, useState } from 'react'
import { Card, Row, Col, Select, Spin, Tooltip, Modal } from 'antd'
import axios from 'axios'

interface Site {
  id: string
  name: string
}

interface Rack {
  id: string
  name: string
  site_id: string
  total_units: number
  used_units: number
}

interface RackDevice {
  id: string
  name: string
  asset_type: string
  rack_position: number
  status: string
  alert_count: number
}

function Racks() {
  const [sites, setSites] = useState<Site[]>([])
  const [racks, setRacks] = useState<Rack[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedSite, setSelectedSite] = useState<string>('')
  const [selectedRack, setSelectedRack] = useState<Rack | null>(null)
  const [rackDevices, setRackDevices] = useState<RackDevice[]>([])

  const fetchSites = async () => {
    try {
      const res = await axios.get('/api/sites')
      setSites(res.data.data || [])
    } catch (error) {
      setSites([
        { id: '1', name: '机房A' },
        { id: '2', name: '机房B' },
        { id: '3', name: '机房C' },
      ])
    }
  }

  const fetchRacks = async (siteId: string) => {
    setLoading(true)
    try {
      const res = await axios.get(`/api/racks?site_id=${siteId}`)
      setRacks(res.data.data || [])
    } catch (error) {
      setRacks([
        { id: '1', name: 'Rack-01', site_id: siteId, total_units: 42, used_units: 20 },
        { id: '2', name: 'Rack-02', site_id: siteId, total_units: 42, used_units: 15 },
        { id: '3', name: 'Rack-03', site_id: siteId, total_units: 42, used_units: 25 },
      ])
    } finally {
      setLoading(false)
    }
  }

  const fetchRackDevices = async (rackId: string) => {
    try {
      const res = await axios.get(`/api/racks/${rackId}/devices`)
      setRackDevices(res.data.data || [])
    } catch (error) {
      setRackDevices([
        { id: '1', name: 'server-01', asset_type: 'server', rack_position: 42, status: 'green', alert_count: 0 },
        { id: '2', name: 'server-02', asset_type: 'server', rack_position: 38, status: 'green', alert_count: 0 },
        { id: '3', name: 'switch-01', asset_type: 'switch', rack_position: 36, status: 'red', alert_count: 2 },
        { id: '4', name: 'patch-panel', asset_type: 'other', rack_position: 34, status: 'green', alert_count: 0 },
        { id: '5', name: 'server-03', asset_type: 'server', rack_position: 30, status: 'green', alert_count: 0 },
      ])
    }
  }

  useEffect(() => {
    fetchSites()
  }, [])

  useEffect(() => {
    if (selectedSite) {
      fetchRacks(selectedSite)
    }
  }, [selectedSite])

  useEffect(() => {
    if (selectedRack) {
      fetchRackDevices(selectedRack.id)
    }
  }, [selectedRack])

  const getStatusColor = (status: string) => {
    if (status === 'red') return '#ff4d4f'
    if (status === 'yellow') return '#faad14'
    return '#52c41a'
  }

  return (
    <div>
      <h2 style={{ marginBottom: 16 }}>机房机柜</h2>
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col>
          <Select
            placeholder="选择机房"
            value={selectedSite}
            onChange={setSelectedSite}
            style={{ width: 200 }}
            options={sites.map(s => ({ label: s.name, value: s.id }))}
          />
        </Col>
      </Row>

      {loading ? (
        <div style={{ textAlign: 'center', padding: 100 }}>
          <Spin size="large" />
        </div>
      ) : (
        <Row gutter={16}>
          {racks.map(rack => (
            <Col span={6} key={rack.id}>
              <Card
                hoverable
                title={rack.name}
                extra={<span style={{ color: '#999' }}>{rack.used_units}/{rack.total_units}U</span>}
                onClick={() => setSelectedRack(rack)}
                style={{
                  borderColor: selectedRack?.id === rack.id ? '#1890ff' : '#d9d9d9'
                }}
              >
                <div style={{
                  height: 300,
                  background: '#f5f5f5',
                  borderRadius: 4,
                  padding: 8,
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 2
                }}>
                  {Array.from({ length: 10 }).map((_, i) => {
                    const unit = 42 - i * 4
                    return (
                      <div
                        key={i}
                        style={{
                          flex: 1,
                          background: i < Math.floor(rack.used_units / 4) ? '#1890ff' : '#e8e8e8',
                          borderRadius: 2,
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          fontSize: 10,
                          color: i < Math.floor(rack.used_units / 4) ? '#fff' : '#999'
                        }}
                      >
                        {unit}U
                      </div>
                    )
                  })}
                </div>
              </Card>
            </Col>
          ))}
        </Row>
      )}

      <Modal
        title={selectedRack ? `${selectedRack.name} 设备列表` : '机柜设备'}
        open={!!selectedRack}
        onCancel={() => setSelectedRack(null)}
        footer={null}
        width={600}
      >
        <Row gutter={[16, 16]}>
          {rackDevices.map(device => (
            <Col span={24} key={device.id}>
              <Card size="small">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <div>
                    <strong>{device.name}</strong>
                    <span style={{ marginLeft: 8, color: '#999' }}>{device.asset_type}</span>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <Tooltip title={`位置: ${device.rack_position}U`}>
                      <span style={{ color: '#1890ff' }}>{device.rack_position}U</span>
                    </Tooltip>
                    <span
                      style={{
                        width: 12,
                        height: 12,
                        borderRadius: '50%',
                        background: getStatusColor(device.status)
                      }}
                    />
                    {device.alert_count > 0 && (
                      <span style={{ color: '#ff4d4f', fontSize: 12 }}>
                        {device.alert_count} 告警
                      </span>
                    )}
                  </div>
                </div>
              </Card>
            </Col>
          ))}
        </Row>
      </Modal>
    </div>
  )
}

export default Racks
