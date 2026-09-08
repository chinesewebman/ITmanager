package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capabilityMatrix 期望矩阵的**独立**声明（故意不引用 capRoles）：
// 测试若与被测实现共用同一份数据，改错了也测不出来。
// 语义见 docs/FIX-PLAN-AUTHZ.md §3.2。
var capabilityMatrix = map[string]map[Capability]bool{
	RoleAdmin: {
		CapRead: true, CapWrite: true, CapManage: true, CapAudit: true, CapIdentity: true,
	},
	RoleOpsAdmin: {
		CapRead: true, CapWrite: true, CapManage: true, CapAudit: true, CapIdentity: false,
	},
	RoleOpsUser: {
		CapRead: true, CapWrite: true, CapManage: false, CapAudit: false, CapIdentity: false,
	},
	RoleAuditor: {
		CapRead: true, CapWrite: false, CapManage: false, CapAudit: true, CapIdentity: false,
	},
	RoleReadonly: {
		CapRead: true, CapWrite: false, CapManage: false, CapAudit: false, CapIdentity: false,
	},
	RoleUser: {
		CapRead: true, CapWrite: false, CapManage: false, CapAudit: false, CapIdentity: false,
	},
	// 遗留别名：与折叠后的权威角色完全同权
	"operator": {
		CapRead: true, CapWrite: true, CapManage: false, CapAudit: false, CapIdentity: false,
	},
	"viewer": {
		CapRead: true, CapWrite: false, CapManage: false, CapAudit: false, CapIdentity: false,
	},
	// fail-safe：未知 / 空角色只能读
	"":        {CapRead: true, CapWrite: false, CapManage: false, CapAudit: false, CapIdentity: false},
	"garbage": {CapRead: true, CapWrite: false, CapManage: false, CapAudit: false, CapIdentity: false},
}

// matrixCapabilities 是测试自己的能力全集（独立于生产代码的 allCapabilities）
var matrixCapabilities = []Capability{CapRead, CapWrite, CapManage, CapAudit, CapIdentity}

// TestCan_矩阵全枚举 逐角色 × 逐能力断言，覆盖 §3.2 的每一格。
func TestCan_矩阵全枚举(t *testing.T) {
	for role, expect := range capabilityMatrix {
		for _, cap := range matrixCapabilities {
			t.Run(string(cap)+"/"+role, func(t *testing.T) {
				assert.Equal(t, expect[cap], Can(role, cap),
					"角色 %q 对能力 %q 的判定与矩阵不符", role, cap)
			})
		}
	}
}

// TestCan_未登记能力一律拒绝 新增能力若忘记登记 capRoles，必须拒绝而不是放行。
func TestCan_未登记能力一律拒绝(t *testing.T) {
	assert.False(t, Can(RoleAdmin, Capability("nonexistent")))
	assert.False(t, Can(RoleAdmin, Capability("")))
}

// TestCan_大小写与空白归一 词表归一必须容忍 " Admin " / "OPS_ADMIN" 这类脏值。
func TestCan_大小写与空白归一(t *testing.T) {
	assert.True(t, Can(" Admin ", CapIdentity), "首尾空白 + 大写应归一为 admin")
	assert.True(t, Can("OPS_ADMIN", CapManage), "大写应归一为 ops_admin")
	assert.True(t, Can(" Operator ", CapWrite), "别名也应先归一再匹配")
}

// TestCan_与CanonicalRole等价 别名不得绕开规范化：
// Can(r,c) 必须恒等于 Can(CanonicalRole(r),c)，否则别名会静默失效。
func TestCan_与CanonicalRole等价(t *testing.T) {
	roles := []string{"admin", "ops_admin", "ops_user", "auditor", "readonly", "user",
		"operator", "viewer", "", "garbage", " Admin ", "OPERATOR"}
	for _, role := range roles {
		for _, cap := range matrixCapabilities {
			assert.Equal(t, Can(role, cap), Can(CanonicalRole(role), cap),
				"角色 %q 的能力 %q 在规范化前后不一致", role, cap)
		}
	}
}

func TestCanonicalRole_映射表(t *testing.T) {
	cases := []struct{ in, want string }{
		{"operator", RoleOpsUser},
		{"viewer", RoleReadonly},
		{" Operator ", RoleOpsUser},
		{"admin", RoleAdmin},
		{"ops_admin", RoleOpsAdmin},
		{"ops_user", RoleOpsUser},
		{"auditor", RoleAuditor},
		{"readonly", RoleReadonly},
		{"user", RoleUser},
		{"", ""},
		{"garbage", "garbage"}, // 未知值原样返回，不猜
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.want, CanonicalRole(c.in))
		})
	}
}

// TestIsKnownRole_只认权威词表 写入路径（cmd/set-role）靠它拦住错别字角色。
func TestIsKnownRole_只认权威词表(t *testing.T) {
	for _, r := range []string{"admin", "ops_admin", "ops_user", "auditor", "readonly", "user"} {
		assert.True(t, IsKnownRole(r), "%q 是权威角色", r)
	}
	for _, r := range []string{"operator", "viewer"} {
		assert.True(t, IsKnownRole(r), "%q 是遗留别名，折叠后合法", r)
	}
	for _, r := range []string{"", "garbage", "superuser", "ADMIN_ROLE"} {
		assert.False(t, IsKnownRole(r), "%q 不应通过校验", r)
	}
}

func TestCapabilities_按固定顺序返回(t *testing.T) {
	cases := []struct {
		role string
		want []Capability
	}{
		{RoleAdmin, []Capability{CapRead, CapWrite, CapManage, CapAudit, CapIdentity}},
		{RoleOpsAdmin, []Capability{CapRead, CapWrite, CapManage, CapAudit}},
		{RoleOpsUser, []Capability{CapRead, CapWrite}},
		{RoleAuditor, []Capability{CapRead, CapAudit}},
		{RoleReadonly, []Capability{CapRead}},
		{RoleUser, []Capability{CapRead}},
		{"operator", []Capability{CapRead, CapWrite}},
		{"", []Capability{CapRead}},
	}
	for _, c := range cases {
		t.Run(c.role, func(t *testing.T) {
			assert.Equal(t, c.want, Capabilities(c.role))
		})
	}
}

// TestCapabilities_与Can一致 下发给前端的能力集必须与实际鉴权判定逐项一致。
func TestCapabilities_与Can一致(t *testing.T) {
	for role := range capabilityMatrix {
		got := map[Capability]bool{}
		for _, cap := range Capabilities(role) {
			got[cap] = true
		}
		for _, cap := range matrixCapabilities {
			assert.Equal(t, Can(role, cap), got[cap], "角色 %q 能力 %q 下发值与判定不符", role, cap)
		}
	}
}

// capabilityProbe 跑一次请求，返回状态码。
func capabilityProbe(t *testing.T, role string, cap Capability) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/probe",
		func(c *gin.Context) { c.Set("role", role) },
		RequireCapability(cap),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	return w.Code
}

func TestRequireCapability_放行与拒绝(t *testing.T) {
	cases := []struct {
		role string
		cap  Capability
		want int
	}{
		{RoleAdmin, CapIdentity, http.StatusOK},
		{RoleOpsAdmin, CapManage, http.StatusOK},
		{RoleOpsAdmin, CapIdentity, http.StatusForbidden},
		{RoleOpsUser, CapWrite, http.StatusOK},
		{RoleOpsUser, CapManage, http.StatusForbidden},
		{RoleAuditor, CapAudit, http.StatusOK},
		{RoleAuditor, CapWrite, http.StatusForbidden},
		{RoleReadonly, CapRead, http.StatusOK},
		{RoleReadonly, CapWrite, http.StatusForbidden},
		{"", CapRead, http.StatusOK},
		{"", CapWrite, http.StatusForbidden},
		{"operator", CapWrite, http.StatusOK},
		{"viewer", CapWrite, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.role+"/"+string(c.cap), func(t *testing.T) {
			assert.Equal(t, c.want, capabilityProbe(t, c.role, c.cap))
		})
	}
}

// TestRequireCapability_拒绝时不继续 403 后 handler 不得被执行（防 c.Next() 漏写）。
func TestRequireCapability_拒绝时不继续(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	r := gin.New()
	r.GET("/probe",
		func(c *gin.Context) { c.Set("role", RoleReadonly) },
		RequireCapability(CapManage),
		func(c *gin.Context) { called = true; c.Status(http.StatusOK) },
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))

	require.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, called, "被拒后 handler 不应执行")
}
