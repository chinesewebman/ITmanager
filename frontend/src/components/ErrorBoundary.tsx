import { Component, ErrorInfo, ReactNode } from 'react'
import { Result, Button } from 'antd'

interface ErrorBoundaryProps {
  children: ReactNode
  /** 边界标识，用于出错时定位是哪个 page 挂了 */
  pageName?: string
}

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
}

/**
 * ErrorBoundary（C-F13 留档项）
 * React 18+ 用 class component 抓子树渲染错误，防止单 page 崩溃让整个 app 白屏
 *
 * 用法：
 *   <ErrorBoundary pageName="资产管理">
 *     <Assets />
 *   </ErrorBoundary>
 *
 * 设计要点：
 *   - 只接 render 错误（componentDidCatch），不接 event handler / async 错误
 *   - 用 antd Result 组件显示错误页（跟现有 UI 一致）
 *   - 提供"重试"按钮（重置 state）和"返回首页"按钮
 *   - 控制台打完整堆栈（开发者排错用）
 *   - 不上送 Sentry 等外部服务（避免引入更多依赖）
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { hasError: false, error: null }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    console.error(
      `[ErrorBoundary] ${this.props.pageName || 'unknown page'} 渲染失败:`,
      error,
      errorInfo.componentStack,
    )
  }

  private handleReset = () => {
    this.setState({ hasError: false, error: null })
  }

  private handleGoHome = () => {
    window.location.href = '/'
  }

  render() {
    if (this.state.hasError) {
      const name = this.props.pageName || '当前页面'
      return (
        <Result
          status="error"
          title={`${name}加载失败`}
          subTitle={
            this.state.error?.message
              ? `错误信息: ${this.state.error.message}`
              : '组件渲染过程中发生未捕获的错误'
          }
          extra={[
            <Button key="retry" type="primary" onClick={this.handleReset}>
              重试
            </Button>,
            <Button key="home" onClick={this.handleGoHome}>
              返回首页
            </Button>,
          ]}
        />
      )
    }

    return this.props.children
  }
}

export default ErrorBoundary
