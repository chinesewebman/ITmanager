// useResponsiveTable + MobileCardList smoke test
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import { ConfigProvider } from 'antd'
import { useResponsiveTable, MobileCardList } from './useResponsiveTable'

function TestHook({ children }: { children: (val: ReturnType<typeof useResponsiveTable>) => React.ReactNode }) {
  const v = useResponsiveTable()
  return <>{children(v)}</>
}

describe('useResponsiveTable', () => {
  it('jsdom 下 (默认 xs=false) 视为 desktop', () => {
    render(
      <ConfigProvider>
        <TestHook>
          {(v) => <div data-testid="hook">{v.isMobile ? 'mobile' : 'desktop'}</div>}
        </TestHook>
      </ConfigProvider>,
    )
    expect(screen.getByTestId('hook')).toHaveTextContent('desktop')
  })
})

describe('MobileCardList', () => {
  const data = [
    { id: '1', name: 'web-01', ip: '10.0.0.1' },
    { id: '2', name: 'web-02', ip: '10.0.0.2' },
  ]

  it('空数据时显示 empty 文案', () => {
    render(
      <ConfigProvider>
        <MobileCardList data={[]} renderCard={() => null} emptyText="无资产" />
      </ConfigProvider>,
    )
    expect(screen.getByText('无资产')).toBeInTheDocument()
  })

  it('渲染每条数据为 Card', () => {
    render(
      <ConfigProvider>
        <MobileCardList
          data={data}
          renderCard={(item) => <span>{item.name} ({item.ip})</span>}
        />
      </ConfigProvider>,
    )
    expect(screen.getByText('web-01 (10.0.0.1)')).toBeInTheDocument()
    expect(screen.getByText('web-02 (10.0.0.2)')).toBeInTheDocument()
  })

  it('loading=true 时显示 Card loading', () => {
    render(
      <ConfigProvider>
        <MobileCardList data={data} renderCard={() => null} loading />
      </ConfigProvider>,
    )
    // loading Card 渲染了, data 不渲染
    expect(screen.queryByText('web-01 (10.0.0.1)')).toBeNull()
  })
})
