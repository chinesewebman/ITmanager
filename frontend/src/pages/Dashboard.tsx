import { useEffect, useState } from 'react'
import { Card, Row, Col, Statistic, List, Tag, Spin } from 'antd'
import { ArrowDownOutlined, AlertOutlined, DesktopOutlined, BuildOutlined, HomeOutlined } from '@ant-design/icons'
import ReactECharts from 'echarts-for-react'
import { dashboardApi } from '../services/api'

interface DashboardStats {
  assets: number
  alerts: number
  tickets: number
  sites: number
  machines: number
  networks: number
}

interface AlertTrend {
  date: string
  count: number
}

function Dashboard() {
  const [loading, setLoading] = useState(true)
  const [stats, setStats] = useState<DashboardStats>({
    assets: 0,
    alerts: 0,
    tickets: 0,
    sites: 0,
    machines: 0,
    networks: 0,
  })
  const [trends, setTrends] = useState<AlertTrend[]>([])

  useEffect(() => {
    fetchDashboardData()
  }, [])

  const fetchDashboardData = async () => {
    try {
      const [statsRes, trendsRes] = await Promise.all([
        dashboardApi.getStats(),
        dashboardApi.getTrends(),
      ])
      setStats(statsRes.data.data)
      setTrends(trendsRes.data.data.alert_trends)
    } catch (error) {
      console.error('获取仪表盘数据失败:', error)
      // 使用模拟数据
      setStats({
        assets: 156,
        alerts: 8,
        tickets: 23,
        sites: 3,
        machines: 45,
        networks: 12,
      })
      setTrends([
        { date: '2026-02-08', count: 10 },
        { date: '2026-02-09', count: 15 },
        { date: '2026-02-10', count: 8 },
        { date: '2026-02-11', count: 12 },
        { date: '2026-02-12', count: 5 },
        { date: '2026-02-13', count: 7 },
        { date: '2026-02-14', count: 3 },
      ])
    } finally {
      setLoading(false)
    }
  }

  const chartOption = {
    title: { text: '告警趋势', left: 'center' },
    tooltip: { trigger: 'axis' },
    xAxis: {
      type: 'category',
      data: trends.map(t => t.date),
    },
    yAxis: { type: 'value' },
    series: [{
      data: trends.map(t => t.count),
      type: 'line',
      smooth: true,
      areaStyle: { opacity: 0.3 },
      itemStyle: { color: '#1890ff' },
    }],
  }

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 100 }}>
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div>
      <h2 style={{ marginBottom: 24 }}>仪表盘</h2>
      <Row gutter={16}>
        <Col span={6}>
          <Card>
            <Statistic
              title="资产总数"
              value={stats.assets}
              prefix={<DesktopOutlined />}
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="活跃告警"
              value={stats.alerts}
              prefix={<AlertOutlined />}
              valueStyle={{ color: '#ff4d4f' }}
              suffix={
                <span style={{ fontSize: 14, color: '#52c41a', marginLeft: 8 }}>
                  <ArrowDownOutlined /> 5
                </span>
              }
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="待处理工单"
              value={stats.tickets}
              prefix={<BuildOutlined />}
              valueStyle={{ color: '#faad14' }}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="机房数量"
              value={stats.sites}
              prefix={<HomeOutlined />}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
      </Row>

      <Row gutter={16} style={{ marginTop: 16 }}>
        <Col span={16}>
          <Card title="告警趋势">
            <ReactECharts option={chartOption} style={{ height: 300 }} />
          </Card>
        </Col>
        <Col span={8}>
          <Card title="最近告警" style={{ height: 350 }}>
            <List
              dataSource={[
                { id: 1, host: 'web-server-01', message: 'CPU使用率超过90%', severity: 'critical', time: '10分钟前' },
                { id: 2, host: 'db-server-02', message: '磁盘空间不足', severity: 'warning', time: '30分钟前' },
                { id: 3, host: 'switch-core-01', message: '端口状态异常', severity: 'error', time: '1小时前' },
                { id: 4, host: 'firewall-main', message: '连接数超阈值', severity: 'warning', time: '2小时前' },
              ]}
              renderItem={(item: any) => (
                <List.Item>
                  <List.Item.Meta
                    title={item.host}
                    description={
                      <div>
                        <span>{item.message}</span>
                        <div>
                          <Tag color={item.severity === 'critical' ? 'red' : 'orange'} style={{ marginTop: 4 }}>
                            {item.severity}
                          </Tag>
                          <span style={{ color: '#999', fontSize: 12, marginLeft: 8 }}>{item.time}</span>
                        </div>
                      </div>
                    }
                  />
                </List.Item>
              )}
            />
          </Card>
        </Col>
      </Row>
    </div>
  )
}

export default Dashboard
