package middleware

import (
	"strings"

	"network-monitor-platform/internal/apierr"

	"github.com/gin-gonic/gin"
)

// 角色词表（权威口径 = migrations/000001_init.up.sql 的 roles.code + 000013 的兜底值 user）。
//
// 引入新角色必须同步以下位置。**注意：没有任何测试能自动发现「漏登记的新角色」**，
// 下面的清单靠人工执行（roles_test.go / routes_integration_test.go 的期望都是硬编码的）：
//  1. roles 表加行（新迁移；兜底角色 user 例外，它只在 users.role 里）
//  2. 这里加常量 + 写进 knownRoles
//  3. capRoles 里显式声明它能做什么（不登记 = 只能读）
//  4. roles_test.go 的 capabilityMatrix
//  5. internal/api/routes_integration_test.go 的 matrixRoles
//  6. openapi.yaml 的 User.role enum + frontend/src/types/index.ts 的 union
//
// 详见 docs/FIX-PLAN-AUTHZ.md §3.1。
const (
	RoleAdmin    = "admin"
	RoleOpsAdmin = "ops_admin"
	RoleOpsUser  = "ops_user"
	RoleAuditor  = "auditor"
	RoleReadonly = "readonly"
	RoleUser     = "user"
)

// Capability 能力档位。语义见 docs/FIX-PLAN-AUTHZ.md §3.2。
type Capability string

const (
	// CapRead 是地板：任何已认证身份都放行（含未知/空角色）。
	// 它不在 capRoles 里 —— 单独在 Can 里处理。
	CapRead     Capability = "read"
	CapWrite    Capability = "write"
	CapManage   Capability = "manage"
	CapAudit    Capability = "audit"
	CapIdentity Capability = "identity"
)

// legacyRoleAliases 设计期遗留词表（cmd/seed、openapi、前端类型曾用），只读入不写出。
// 保留是为了存量 dev 库与未过期 JWT（24h）不静默降权；v4 清理。
var legacyRoleAliases = map[string]string{
	"operator": RoleOpsUser,
	"viewer":   RoleReadonly,
}

// capRoles 能力矩阵的唯一实现：能力 → 允许的角色。
var capRoles = map[Capability][]string{
	CapWrite:    {RoleAdmin, RoleOpsAdmin, RoleOpsUser},
	CapManage:   {RoleAdmin, RoleOpsAdmin},
	CapAudit:    {RoleAdmin, RoleOpsAdmin, RoleAuditor},
	CapIdentity: {RoleAdmin},
}

// knownRoles 权威词表的全部取值（含只读地板角色 readonly/user）。
// 用途：写入路径（cmd/set-role）校验，避免把错别字写进 users.role 后静默降权。
var knownRoles = map[string]bool{
	RoleAdmin: true, RoleOpsAdmin: true, RoleOpsUser: true,
	RoleAuditor: true, RoleReadonly: true, RoleUser: true,
}

// IsKnownRole 判断角色是否属于权威词表（先折叠遗留别名）。
func IsKnownRole(role string) bool {
	return knownRoles[CanonicalRole(role)]
}

// CanonicalRole 把遗留别名折叠到权威词表；未知值原样返回（由 Can 按最小权限处理）。
func CanonicalRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	if canonical, ok := legacyRoleAliases[r]; ok {
		return canonical
	}
	return r
}

// Can 判定角色是否具备某能力。内部先过 CanonicalRole，保证
// Can(role, cap) == Can(CanonicalRole(role), cap)。
//
// 空/未知角色 fail-safe 到只读地板：只放行 CapRead，绝不放行写/删/凭据。
// 未登记的能力一律拒绝（新增能力必须显式登记到 capRoles）。
func Can(role string, cap Capability) bool {
	if cap == CapRead {
		return true
	}
	allowed, ok := capRoles[cap]
	if !ok {
		return false
	}
	canonical := CanonicalRole(role)
	for _, a := range allowed {
		if canonical == a {
			return true
		}
	}
	return false
}

// allCapabilities 能力全集（顺序即下发顺序）。新增能力必须同时登记到 capRoles，
// 否则 Can 一律拒绝、这里也就不会下发。
var allCapabilities = []Capability{CapRead, CapWrite, CapManage, CapAudit, CapIdentity}

// Capabilities 返回角色具备的全部能力，顺序固定（read 在前）。
// 供 GET /api/auth/me 下发，避免前端复制一份矩阵后与后端漂移。
func Capabilities(role string) []Capability {
	out := make([]Capability, 0, len(allCapabilities))
	for _, c := range allCapabilities {
		if Can(role, c) {
			out = append(out, c)
		}
	}
	return out
}

// RequireCapability 按能力档位鉴权。必须走 Can（不得自行比较角色字面量），
// 否则遗留别名会静默失效而测试仍可能全绿。
func RequireCapability(cap Capability) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Can(c.GetString("role"), cap) {
			apierr.Forbidden(c, "权限不足")
			c.Abort()
			return
		}
		c.Next()
	}
}
