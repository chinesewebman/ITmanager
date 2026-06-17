import { useEffect } from 'react'

/**
 * useDocumentTitle - 同步页面标题到浏览器 tab
 *
 * 用法：
 *   useDocumentTitle('资产管理')   // tab 显示 "资产管理 - 网络运维监控平台"
 *   useDocumentTitle('资产管理', { suffix: false })  // tab 仅显示 "资产管理"
 *
 * 设计要点：
 *   - 组件 unmount 时恢复默认 title (防止 SPA 切页后还残留旧 title)
 *   - 模板后缀 (APP_SUFFIX) 可关闭，方便详情页等需要完整自定义的场景
 *   - 处理 SSR (typeof document) 兼容
 */
const DEFAULT_TITLE = '网络运维监控平台'
const APP_SUFFIX = ' - 网络运维监控平台'

export function useDocumentTitle(title: string, options?: { suffix?: boolean }) {
  const useSuffix = options?.suffix !== false

  useEffect(() => {
    if (typeof document === 'undefined') return
    const previousTitle = document.title
    document.title = useSuffix ? `${title}${APP_SUFFIX}` : title
    return () => {
      document.title = previousTitle
    }
  }, [title, useSuffix])
}

export { DEFAULT_TITLE, APP_SUFFIX }
export default useDocumentTitle
