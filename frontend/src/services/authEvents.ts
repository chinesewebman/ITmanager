// authEvents 全局 auth 事件总线
// 解决：axios 拦截器在 React 树外，不能直接用 useNavigate
// 模式：dispatch custom event → App 监听 + 用 useNavigate 跳转
// 事件载荷: { pathname: string }  (用户当前路径，登录后跳回)

export const AUTH_LOGOUT_EVENT = "auth:logout"

export interface AuthLogoutDetail {
  pathname?: string
  reason?: "401" | "manual" | "expired"
}

// dispatchAuthLogout 触发全局登出事件
export function dispatchAuthLogout(detail: AuthLogoutDetail = {}) {
  // 当前路径（用于登录后跳回）
  if (typeof window !== "undefined" && !detail.pathname) {
    detail.pathname = window.location.pathname + window.location.search
  }
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent<AuthLogoutDetail>(AUTH_LOGOUT_EVENT, { detail }))
  }
}
