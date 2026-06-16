/**
 * Type-safe API helpers
 *
 * 从 backend/internal/api/openapi.yaml 自动生成的 types 在 api.types.ts
 * 这个文件用 helper + type guard 让 services/api.ts 中的 any 调用
 * 逐步迁移到强类型（保持向后兼容）。
 *
 * 用法：
 *   import { typePath } from './api.types'
 *   const body: typePath<'/assets', 'post', 'requestBody'> = { name: '...', type: '...', status: '...' }
 */

import type { components, paths } from './api.types'

// ==================== Schema 类型别名 ====================

/** Asset 资产对象（components.schemas.Asset） */
export type AssetDTO = components['schemas']['Asset']

/** Alert 告警对象 */
export type AlertDTO = components['schemas']['Alert']

/** Ticket 工单对象 */
export type TicketDTO = components['schemas']['Ticket']

/** Rack 机柜对象 */
export type RackDTO = components['schemas']['Rack']

/** Site 机房对象 */
export type SiteDTO = components['schemas']['Site']

/** NotificationChannel 通知渠道 */
export type NotificationChannelDTO = components['schemas']['NotificationChannel']

/** 统一响应包装 { code, message, data } */
export type ApiResponse<T> = {
  code: number
  message: string
  data: T
}

// ==================== Path-based 类型助手 ====================

/**
 * typePath<P, M, K> — 从 paths 提取指定 path + method 的指定字段类型
 * 例子：typePath<'/assets', 'get', 'parameters'> → query 参数类型
 */
export type typePath<
  P extends keyof paths,
  M extends keyof paths[P],
  K extends 'parameters' | 'responses' | 'requestBody',
> = paths[P][M] extends { [k in K]: infer R } ? R : never

// ==================== 常用 type-safe 调用 ====================

/** POST /auth/login request body */
export type LoginRequest = typePath<'/auth/login', 'post', 'requestBody'> extends { content: infer C }
  ? C extends { 'application/json': infer B }
    ? B
    : never
  : never

/** GET /assets query params */
export type AssetListParams = typePath<'/assets', 'get', 'parameters'> extends { query?: infer Q }
  ? Q
  : never

/** GET /alerts query params */
export type AlertListParams = typePath<'/alerts', 'get', 'parameters'> extends { query?: infer Q }
  ? Q
  : never

// ==================== 错误类型（与 apierr 包契约对齐） ====================

/** 统一错误响应 — 与 backend/apierr.go.ErrorResponse 保持一致 */
export type ApiError = {
  code: string // 'bad_request' | 'unauthorized' | 'not_found' | 'database_error' | ...
  message: string
  trace_id?: string
}

/** 业务错误码常量（与 backend/apierr.go.Code* 对齐） */
export const ApiErrorCode = {
  BadRequest: 'bad_request',
  Unauthorized: 'unauthorized',
  Forbidden: 'forbidden',
  NotFound: 'not_found',
  Conflict: 'conflict',
  Internal: 'internal_error',
  DatabaseError: 'database_error',
  ValidationFailed: 'validation_failed',
} as const
export type ApiErrorCodeType = (typeof ApiErrorCode)[keyof typeof ApiErrorCode]

// ==================== Migration 标记 ====================

/**
 * 已迁移到 type-safe 的方法（示范）：
 * - assetApi.list (query params 用 AssetListParams)
 * - alertApi.list (query params 用 AlertListParams)
 * - authApi.login (body 用 LoginRequest)
 *
 * 迁移原则：保留原 method signature 不变，type alias 作为 opt-in 增强。
 * 调用方可以继续用 `params: any`，但 IDE 会在 type 已知时给出提示。
 */
export const MIGRATION_STATUS = {
  assetApi_list: 'typed (AssetListParams)',
  alertApi_list: 'typed (AlertListParams)',
  authApi_login: 'typed (LoginRequest)',
  // TODO: 全部方法迁移
} as const
