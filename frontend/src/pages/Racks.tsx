import { Select, Spin, Modal } from 'antd'
import { siteApi, rackApi } from '../services/api'
import { PageHeader } from '../components/PageHeader'
import { RackGrid, type Rack } from '../components/RackGrid'
import { RackDeviceList, type RackDevice } from '../components/RackDeviceList'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { useApiQuery, queryKeys } from '../hooks/useApiQuery'
import { useState } from 'react'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

interface Site {
  id: string
  name: string
}

/** 接口列表形状归一：非数组（形状变了 / 后端返回 null）一律当空，不回落 mock。 */
function asArray<T>(v: unknown): T[] {
  return Array.isArray(v) ? (v as T[]) : []
}

// W1：`res?.data?.data ?? MOCK_SITES`（站点）、`?? mockRacks(...)`（机柜）、
// `?? mockDevices()`（设备）三处兜底已删除。原写法在接口失败或形状变化时
// 静默显示虚构的机房/机柜/设备，运维会在真实机房里对着假机柜排查。

function Racks() {
  useDocumentTitle('机房机柜')
  const [selectedSite, setSelectedSite] = useState<string>('')
  const [selectedRack, setSelectedRack] = useState<Rack | null>(null)
  // C-P9: 站点列表用 React Query（极少变化，缓存 5min）
  const {
    data: sitesData,
    isError: sitesIsError,
    error: sitesError,
    refetch: sitesRefetch,
  } = useApiQuery<Site[]>(
    queryKeys.racks.all,
    async () => {
      const res: any = await siteApi.list()
      return asArray<Site>(res?.data?.data)
    },
    { staleTime: 5 * 60_000 },
  )
  const sites = sitesData ?? []

  // 机柜列表按 site 隔离
  const {
    data: racksData,
    isLoading,
    isError: racksIsError,
    error: racksError,
    refetch: racksRefetch,
  } = useApiQuery<Rack[]>(
    ['racks', 'list', selectedSite],
    async () => {
      const res: any = await rackApi.list({ site_id: selectedSite })
      return asArray<Rack>(res?.data?.data)
    },
    { enabled: !!selectedSite, staleTime: 30_000 },
  )
  const racks = racksData ?? []

  // 设备列表按 rack 隔离
  const {
    data: devicesData,
    isLoading: devicesLoading,
    isError: devicesIsError,
    error: devicesError,
    refetch: devicesRefetch,
  } = useApiQuery<RackDevice[]>(
    queryKeys.racks.devices(selectedRack?.id ?? ''),
    async () => {
      const res: any = await rackApi.getDevices(selectedRack!.id)
      return asArray<RackDevice>(res?.data?.data)
    },
    { enabled: !!selectedRack, staleTime: 30_000 },
  )
  const devices = devicesData ?? []

  return (
    <div>
      <PageHeader title="机房机柜" subtitle="可视化数据中心机柜布局与设备状态" />
      {/* 站点列表失败时不能只留一个空下拉——先给出错误态和重试 */}
      {sitesIsError ? (
        <ErrorState error={sitesError} onRetry={sitesRefetch} compact />
      ) : (
        <div style={{ marginBottom: 16 }}>
          <Select
            placeholder="选择机房"
            value={selectedSite || undefined}
            onChange={setSelectedSite}
            style={{ width: 200 }}
            options={sites.map((s) => ({ label: s.name, value: s.id }))}
          />
        </div>
      )}

      {!selectedSite ? (
        <EmptyState
          title="请先选择机房"
          description="选择机房后展示该机房的机柜布局"
        />
      ) : racksIsError ? (
        <ErrorState error={racksError} onRetry={racksRefetch} />
      ) : isLoading ? (
        <div style={{ textAlign: 'center', padding: 100 }}>
          <Spin size="large" />
        </div>
      ) : racks.length === 0 ? (
        <EmptyState preset="no-racks" />
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
        {devicesIsError ? (
          <ErrorState error={devicesError} onRetry={devicesRefetch} compact />
        ) : devicesLoading ? (
          <div style={{ textAlign: 'center', padding: 40 }}>
            <Spin />
          </div>
        ) : devices.length === 0 ? (
          <EmptyState title="该机柜暂无设备" description="机柜里还没有录入设备" compact />
        ) : (
          <RackDeviceList devices={devices} />
        )}
      </Modal>
    </div>
  )
}

export default Racks
