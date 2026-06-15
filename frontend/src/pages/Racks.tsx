import { useEffect, useState } from 'react'
import { Select, Spin, Modal } from 'antd'
import { siteApi, rackApi } from '../services/api'
import { PageHeader } from '../components/PageHeader'
import { RackGrid, type Rack } from '../components/RackGrid'
import { RackDeviceList, type RackDevice } from '../components/RackDeviceList'

interface Site {
  id: string
  name: string
}

const MOCK_SITES: Site[] = [
  { id: '1', name: '机房A' },
  { id: '2', name: '机房B' },
  { id: '3', name: '机房C' },
]

function mockRacks(siteId: string): Rack[] {
  return [
    { id: `${siteId}-r1`, name: 'Rack-01', site_id: siteId, total_units: 42, used_units: 20 },
    { id: `${siteId}-r2`, name: 'Rack-02', site_id: siteId, total_units: 42, used_units: 15 },
    { id: `${siteId}-r3`, name: 'Rack-03', site_id: siteId, total_units: 42, used_units: 25 },
  ]
}

function mockDevices(): RackDevice[] {
  return [
    { id: '1', name: 'server-01', asset_type: 'server', rack_position: 42, health_status: 'green', alert_count: 0 },
    { id: '2', name: 'server-02', asset_type: 'server', rack_position: 38, health_status: 'green', alert_count: 0 },
    { id: '3', name: 'switch-01', asset_type: 'switch', rack_position: 36, health_status: 'red', alert_count: 2 },
    { id: '4', name: 'patch-panel', asset_type: 'other', rack_position: 34, health_status: 'green', alert_count: 0 },
    { id: '5', name: 'server-03', asset_type: 'server', rack_position: 30, health_status: 'green', alert_count: 0 },
  ]
}

function Racks() {
  const [sites, setSites] = useState<Site[]>([])
  const [selectedSite, setSelectedSite] = useState<string>('')
  const [racks, setRacks] = useState<Rack[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedRack, setSelectedRack] = useState<Rack | null>(null)
  const [devices, setDevices] = useState<RackDevice[]>([])

  const loadSites = async () => {
    try {
      // 走 services/api.ts 拦截器，自动带 token (C-F9 修复)
      const res: any = await siteApi.list()
      setSites(res?.data?.data || [])
    } catch {
      setSites(MOCK_SITES)
    }
  }

  const loadRacks = async (siteId: string) => {
    setLoading(true)
    try {
      const res: any = await rackApi.list({ site_id: siteId })
      setRacks(res?.data?.data || [])
    } catch {
      setRacks(mockRacks(siteId))
    } finally {
      setLoading(false)
    }
  }

  const loadDevices = async (rackId: string) => {
    try {
      const res: any = await rackApi.getDevices(rackId)
      setDevices(res?.data?.data || [])
    } catch {
      setDevices(mockDevices())
    }
  }

  useEffect(() => {
    loadSites()
  }, [])

  useEffect(() => {
    if (selectedSite) {
      loadRacks(selectedSite)
    }
  }, [selectedSite])

  useEffect(() => {
    if (selectedRack) {
      loadDevices(selectedRack.id)
    }
  }, [selectedRack])

  return (
    <div>
      <PageHeader title="机房机柜" subtitle="可视化数据中心机柜布局与设备状态" />
      <div style={{ marginBottom: 16 }}>
        <Select
          placeholder="选择机房"
          value={selectedSite || undefined}
          onChange={setSelectedSite}
          style={{ width: 200 }}
          options={sites.map((s) => ({ label: s.name, value: s.id }))}
        />
      </div>

      {loading ? (
        <div style={{ textAlign: 'center', padding: 100 }}>
          <Spin size="large" />
        </div>
      ) : (
        <RackGrid racks={racks} selectedRackId={selectedRack?.id} onSelect={setSelectedRack} />
      )}

      <Modal
        title={selectedRack ? `${selectedRack.name} 设备列表` : '机柜设备'}
        open={!!selectedRack}
        onCancel={() => setSelectedRack(null)}
        footer={null}
        width={600}
        destroyOnClose
      >
        <RackDeviceList devices={devices} />
      </Modal>
    </div>
  )
}

export default Racks
