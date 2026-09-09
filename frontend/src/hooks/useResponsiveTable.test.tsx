// useResponsiveTable + MobileCardList smoke test
import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
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

  it('P7：key 用 item.id，列表顺序变化时 input 状态跟随 id 而非位置', () => {
    const { rerender } = render(
      <ConfigProvider>
        <MobileCardList
          data={[{ id: 'a' }, { id: 'b' }]}
          renderCard={(item) => <input aria-label={`card-${item.id}`} defaultValue={item.id} />}
        />
      </ConfigProvider>,
    )

    // 编辑 id='a' 的 input（非受控，value 由 DOM 持有，不受 React 管理）
    fireEvent.change(screen.getByLabelText('card-a'), { target: { value: 'edited' } })

    // 顺序反转：b 在前。key={item.id} 时 a 的 DOM 复用（value 保留）；
    // 若 key={idx}，key=0 复用错位，a 的编辑内容挂到 b 上、a 显示 b 的 defaultValue。
    rerender(
      <ConfigProvider>
        <MobileCardList
          data={[{ id: 'b' }, { id: 'a' }]}
          renderCard={(item) => <input aria-label={`card-${item.id}`} defaultValue={item.id} />}
        />
      </ConfigProvider>,
    )

    expect(screen.getByLabelText('card-a')).toHaveValue('edited')
  })
})
