// SeverityTag component test
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import { SeverityTag, SEVERITY_META } from './SeverityTag'

describe('SeverityTag', () => {
  it('渲染 P5 灾难 (magenta)', () => {
    render(<SeverityTag severity={5} />)
    expect(screen.getByText('P5 灾难')).toBeInTheDocument()
  })

  it('渲染 P0 未分类 (default/gray)', () => {
    render(<SeverityTag severity={0} />)
    expect(screen.getByText('P0 未分类')).toBeInTheDocument()
  })

  it('渲染 P4 严重 (red)', () => {
    render(<SeverityTag severity={4} />)
    expect(screen.getByText('P4 严重')).toBeInTheDocument()
  })

  it('自定义 label 覆盖默认', () => {
    render(<SeverityTag severity={2} label="自定义" />)
    expect(screen.getByText('自定义')).toBeInTheDocument()
  })

  it('未知 severity 回退到 P{num}', () => {
    render(<SeverityTag severity={9} />)
    expect(screen.getByText('P9')).toBeInTheDocument()
  })

  it('SEVERITY_META 包含 0-5', () => {
    for (let i = 0; i <= 5; i++) {
      expect(SEVERITY_META[i]).toBeDefined()
    }
  })
})
