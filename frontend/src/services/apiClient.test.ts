// 审计 P2 M1-2 测试补全: apiClient.ts 之前的 0 单元测试。
//
// 注意: typePath helper + AssetDTO 等 type alias 是纯 TypeScript 类型, 没有 runtime 形态。
// 这些靠 tsc --noEmit 验证 (CI 必跑), 不写 vitest。
//
// 本测试只覆盖 runtime 导出: ApiErrorCode 常量 + ApiErrorCodeType union。
import { describe, it, expect } from "vitest"
import { ApiErrorCode, type ApiErrorCodeType } from "./apiClient"

describe("apiClient.ApiErrorCode", () => {
  it("包含所有 8 个业务错误码 (与 backend/apierr.go 对齐)", () => {
    // 防止后端加新错误码时前端遗漏 — 任何缺漏都是契约破裂
    const expected = [
      "bad_request",
      "unauthorized",
      "forbidden",
      "not_found",
      "conflict",
      "internal_error",
      "database_error",
      "validation_failed",
    ]
    expect(Object.values(ApiErrorCode).sort()).toEqual(expected.sort())
  })

  it("每个错误码值是 ApiErrorCodeType union 的成员", () => {
    // 编译期 + runtime 双重检查 — 任何 ApiErrorCode[k] 都能赋值给 ApiErrorCodeType
    const allCodes: ApiErrorCodeType[] = Object.values(ApiErrorCode)
    expect(allCodes.length).toBeGreaterThan(0)
    for (const code of allCodes) {
      const _typed: ApiErrorCodeType = code
      expect(_typed).toBe(code)
    }
  })

  it("与 backend apierr.go 字符串字面量 1:1 对齐", () => {
    // 后端约定 (audit 验证):
    //   bad_request, unauthorized, forbidden, not_found,
    //   conflict, internal_error, database_error, validation_failed
    expect(ApiErrorCode.BadRequest).toBe("bad_request")
    expect(ApiErrorCode.Unauthorized).toBe("unauthorized")
    expect(ApiErrorCode.Forbidden).toBe("forbidden")
    expect(ApiErrorCode.NotFound).toBe("not_found")
    expect(ApiErrorCode.Conflict).toBe("conflict")
    expect(ApiErrorCode.Internal).toBe("internal_error")
    expect(ApiErrorCode.DatabaseError).toBe("database_error")
    expect(ApiErrorCode.ValidationFailed).toBe("validation_failed")
  })
})
