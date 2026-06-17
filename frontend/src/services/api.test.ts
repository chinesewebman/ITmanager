// P1-审计: services/api.ts + authEvents.ts 单元测试
// 覆盖 401 错误处理 / dispatchAuthLogout 事件 / 网络错误 / 业务 code != 0
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import type { AxiosAdapter } from "axios"
import { AUTH_LOGOUT_EVENT, dispatchAuthLogout, type AuthLogoutDetail } from "./authEvents"

// Mock antd message 防止侧效
vi.mock("antd", () => ({
  message: {
    error: vi.fn(),
    success: vi.fn(),
    info: vi.fn(),
  },
}))

// 动态 import api.ts (在 mock 之后)
async function loadApi() {
  vi.resetModules()
  return await import("./api")
}

describe("authEvents", () => {
  it("dispatchAuthLogout 触发 CustomEvent 带 detail", () => {
    const handler = vi.fn()
    window.addEventListener(AUTH_LOGOUT_EVENT, handler)
    try {
      dispatchAuthLogout({ reason: "401", pathname: "/alerts" })
      expect(handler).toHaveBeenCalledOnce()
      const ev = handler.mock.calls[0][0] as CustomEvent<AuthLogoutDetail>
      expect(ev.detail?.reason).toBe("401")
      expect(ev.detail?.pathname).toBe("/alerts")
    } finally {
      window.removeEventListener(AUTH_LOGOUT_EVENT, handler)
    }
  })

  it("dispatchAuthLogout 无 pathname 时自动用当前 location", () => {
    Object.defineProperty(window, "location", {
      writable: true,
      value: { pathname: "/dashboard", search: "?tab=alerts" },
    })
    const handler = vi.fn()
    window.addEventListener(AUTH_LOGOUT_EVENT, handler)
    try {
      dispatchAuthLogout({ reason: "expired" })
      const ev = handler.mock.calls[0][0] as CustomEvent<AuthLogoutDetail>
      expect(ev.detail?.pathname).toBe("/dashboard?tab=alerts")
    } finally {
      window.removeEventListener(AUTH_LOGOUT_EVENT, handler)
    }
  })
})

describe("api.ts response interceptor", () => {
  let mockAdapter: AxiosAdapter

  beforeEach(async () => {
    vi.clearAllMocks()
    const { default: api } = await loadApi()
    // 用类型断言绕过 AxiosAdapter vs Mock 的类型不匹配
    mockAdapter = vi.fn() as unknown as AxiosAdapter
    api.defaults.adapter = mockAdapter
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it("业务 code != 0 时 message.error + reject", async () => {
    (mockAdapter as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      data: { code: 1, message: "业务错误示例" },
      status: 200,
    })
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    await expect(api.get("/test")).rejects.toThrow()
    const { message } = await import("antd")
    expect(message.error).toHaveBeenCalledWith("业务错误示例")
  })

  it("401 错误 dispatchAuthLogout 而非 window.location.href", async () => {
    const handler = vi.fn()
    window.addEventListener(AUTH_LOGOUT_EVENT, handler)
    try {
      (mockAdapter as unknown as ReturnType<typeof vi.fn>).mockRejectedValue({
        response: { status: 401, data: { message: "Token 过期" } },
      })
      const { default: api } = await loadApi()
      api.defaults.adapter = mockAdapter
      await expect(api.get("/test")).rejects.toBeTruthy()
      // 关键断言: 事件被触发 (P1-审计: 不用 location.href)
      expect(handler).toHaveBeenCalled()
      const { message } = await import("antd")
      expect(message.error).toHaveBeenCalledWith("登录已过期，请重新登录")
    } finally {
      window.removeEventListener(AUTH_LOGOUT_EVENT, handler)
    }
  })

  it("403 错误 message.error('没有权限访问')", async () => {
    (mockAdapter as unknown as ReturnType<typeof vi.fn>).mockRejectedValue({
      response: { status: 403, data: {} },
    })
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    await expect(api.get("/test")).rejects.toBeTruthy()
    const { message } = await import("antd")
    expect(message.error).toHaveBeenCalledWith("没有权限访问")
  })

  it("404 错误 message.error('请求的资源不存在')", async () => {
    (mockAdapter as unknown as ReturnType<typeof vi.fn>).mockRejectedValue({
      response: { status: 404, data: {} },
    })
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    await expect(api.get("/test")).rejects.toBeTruthy()
    const { message } = await import("antd")
    expect(message.error).toHaveBeenCalledWith("请求的资源不存在")
  })

  it("500 错误 message.error('服务器内部错误')", async () => {
    (mockAdapter as unknown as ReturnType<typeof vi.fn>).mockRejectedValue({
      response: { status: 500, data: {} },
    })
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    await expect(api.get("/test")).rejects.toBeTruthy()
    const { message } = await import("antd")
    expect(message.error).toHaveBeenCalledWith("服务器内部错误")
  })

  it("网络错误 (无 response) message.error('网络错误')", async () => {
    (mockAdapter as unknown as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("Network Error")
    )
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    await expect(api.get("/test")).rejects.toBeTruthy()
    const { message } = await import("antd")
    expect(message.error).toHaveBeenCalledWith("网络错误，请检查网络连接")
  })

  it("成功响应 (code=0) 透传无 reject", async () => {
    (mockAdapter as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      data: { code: 0, data: { id: 1 } },
      status: 200,
    })
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    const resp = await api.get("/test")
    expect(resp.data.code).toBe(0)
    expect(resp.data.data.id).toBe(1)
  })
})
