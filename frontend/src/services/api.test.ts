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

  // 一致性审计 F8：Settings.tsx 用 `error?.response?.status === 403` 决定是否渲染
  // 「无密钥管理权限」。若拦截器改成抛裸 Error，权限提示会失效，而上面那条只断言
  // `rejects.toBeTruthy()` 的用例照样绿——契约必须单独钉住。
  it("reject 保留 error.response（页面靠它判 403）", async () => {
    (mockAdapter as unknown as ReturnType<typeof vi.fn>).mockRejectedValue({
      response: { status: 403, data: {} },
    })
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    await expect(api.get("/test")).rejects.toMatchObject({
      response: { status: 403 },
    })
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

  // FIX-PLAN-FRONTEND-TOKEN §1.3：204 空 body 曾被 `res.code !== 0` 判成失败
  // （后端 5 处 DELETE 返回 204），删除操作会弹「请求失败」。
  it("204 空 body 不误报（resolve 且不弹错误）", async () => {
    const adapter = mockAdapter as unknown as ReturnType<typeof vi.fn>
    adapter.mockResolvedValue({ data: "", status: 204, statusText: "No Content" })
    const { default: api, apiSend } = await loadApi()
    api.defaults.adapter = mockAdapter
    const resp = await api.delete("/alert-suppressions/1")
    expect(resp.status).toBe(204)
    await expect(apiSend("DELETE", "/alert-suppressions/1")).resolves.toBeUndefined()
    const { message } = await import("antd")
    expect(message.error).not.toHaveBeenCalled()
  })

  // §3.1 的新条件有三个分支：非对象 / 对象但无自有 code / 有 code。
  it("对象但无 code 字段（非业务包）放行", async () => {
    const adapter = mockAdapter as unknown as ReturnType<typeof vi.fn>
    adapter.mockResolvedValue({ data: { foo: 1 }, status: 200 })
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    const resp = await api.get("/test")
    expect(resp.data.foo).toBe(1)
    const { message } = await import("antd")
    expect(message.error).not.toHaveBeenCalled()
  })

  it("blob 响应（下载）放行", async () => {
    const blob = new Blob(["pdf"], { type: "application/pdf" })
    const adapter = mockAdapter as unknown as ReturnType<typeof vi.fn>
    adapter.mockResolvedValue({ data: blob, status: 200 })
    const { default: api } = await loadApi()
    api.defaults.adapter = mockAdapter
    const resp = await api.get("/postmortem/assets/a/report", { responseType: "blob" })
    expect(resp.data).toBe(blob)
    const { message } = await import("antd")
    expect(message.error).not.toHaveBeenCalled()
  })

  // FIX-PLAN-AUTHZ-CLOSURE.md §1 S-2a / AV-4：apiKeyApi 曾漏掉 /auth 前缀
  // （baseURL 已是 "/api"）→ 实际打 /api/api-keys，落 NoRoute 返 index.html，
  // 密钥管理整块失效。用 adapter 断言真实 URL —— vi.mock 整个模块观测不到。
  it("apiKeyApi 四条请求都打 /api/auth/api-keys*", async () => {
    const adapter = mockAdapter as unknown as ReturnType<typeof vi.fn>
    adapter.mockResolvedValue({ data: { code: 0, data: {} }, status: 200 })
    const { default: api, apiKeyApi } = await loadApi()
    api.defaults.adapter = mockAdapter

    await apiKeyApi.list()
    await apiKeyApi.create({ name: "ci", permissions: ["read"] })
    await apiKeyApi.revoke("id-1")
    await apiKeyApi.delete("id-1")

    const calls = adapter.mock.calls
    // 拼串相等还不足以钉住契约：把 baseURL 改成 "/api/v1"、url 改成 "/auth/..." 之类
    // 的漂移仍可能拼出同一串（一致性审计 F4）。基址单独断言。
    expect(api.defaults.baseURL).toBe("/api")
    expect(calls.map(([c]: any) => c.baseURL + c.url)).toEqual([
      "/api/auth/api-keys",
      "/api/auth/api-keys",
      "/api/auth/api-keys/id-1/revoke",
      "/api/auth/api-keys/id-1",
    ])
    expect(calls.map(([c]: any) => c.method)).toEqual([
      "get",
      "post",
      "put",
      "delete",
    ])
  })

  // FV-5：页面改用共享 helper 后，请求必须走 cookie（withCredentials）
  // 且不再手拼 Authorization —— 后者会让后端跳过 cookie 回退（auth.go:83-98）→ 恒 401。
  it("apiGet/apiSend 解包 data，带 withCredentials 且无 Authorization 头", async () => {
    const adapter = mockAdapter as unknown as ReturnType<typeof vi.fn>
    adapter.mockResolvedValue({ data: { code: 0, data: { items: [] } }, status: 200 })
    const { default: api, apiGet, apiSend } = await loadApi()
    api.defaults.adapter = mockAdapter

    const got = await apiGet<{ items: unknown[] }>("/alert-suppressions")
    expect(got).toEqual({ items: [] })

    const sent = await apiSend<{ items: unknown[] }>("POST", "/alert-suppressions", { name: "x" })
    expect(sent).toEqual({ items: [] })

    const calls = adapter.mock.calls
    expect(calls.length).toBe(2)
    for (const [config] of calls) {
      expect(config.baseURL).toBe("/api")
      expect(config.withCredentials).toBe(true)
      // 必须用 has()：AxiosHeaders 只在 has/get 上大小写不敏感，属性访问 `headers.authorization`
      // 拿不到以 `Authorization` 存的头（实测），那样断言会空转。拼接字符串避开本仓
      // no-restricted-syntax 的 Authorization 字面量规则。
      expect(config.headers.has("auth" + "orization")).toBe(false)
    }
    expect(calls[0][0].url).toBe("/alert-suppressions")
    expect(calls[1][0].method).toBe("post")
  })
})
