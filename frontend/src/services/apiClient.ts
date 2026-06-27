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

// ==================== 迁移状态说明 ====================
//
// v2.1.1 (P1-frontend audit): 原 MIGRATION_STATUS 常量已删除。
// 旧常量硬编码 "assetApi_list: typed" 等, 但 api.ts 里 90% 方法签名仍是
// (data: any) (assetApi.create/update, alertRuleApi.*, ticketApi.*, notificationApi.*)。
// 假装"已迁移到位"会误导后来人 — 调用方看不出哪些是真 typed 哪些仍是 any。
//
// 真实迁移计划（v3.0 大版本 + openapi-typescript 自动化）：
//   - 把 api.ts 所有 `data: any` 替换成具体 schema type
//     (e.g. AssetCreateRequest, TicketCreateRequest from api.types.ts)
//   - 用 gen:api 脚本自动生成 typed client, 避免手维护漂移
//   - 估计影响 ~200 处签名变更 + 30+ 调用方调整, 需要独立大版本
//
// 当前策略：保留 apiClient.ts 的 typePath helper + 部分已迁移方法 (assetApi.list,
// alertApi.list, authApi.login) 作为示范, 调用方仍可用 `any` 但 IDE 在 type
// 已知处会提示。完整迁移追踪见 v3.0 roadmap。
