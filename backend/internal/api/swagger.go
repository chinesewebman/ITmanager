// Swagger UI 静态服务（不依赖 swag 自动生成）
// 用现成 backend/openapi.yaml 作为 spec source，挂在 /swagger/index.html
package api

import (
	_ "embed"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

//go:embed openapi.yaml
var openAPISpec []byte

// RegisterSwagger 挂 Swagger UI（用 backend/openapi.yaml 作为 spec）
// 访问：GET /swagger/index.html
//
// 设计选择：不用 swaggo/swag 自动生成（30+ handler 加注解工作量不值）
// 维护：编辑 backend/openapi.yaml 后重启服务即生效（spec 是单一 source of truth）
func RegisterSwagger(r *gin.Engine) {
	// gin-swagger WrapHandler 默认读 docs/ 包的 embedded spec
	// 我们用 URL 选项指向 openapi.yaml 的 HTTP 端点（避免 embed 路径冲突）
	r.GET("/swagger/*any", ginSwagger.WrapHandler(
		swaggerFiles.Handler,
		ginSwagger.URL("/openapi.yaml"),
	))

	// 直接暴露 spec（gin embed 注入的 bytes，避免 c.File 依赖 cwd）
	r.GET("/openapi.yaml", func(c *gin.Context) {
		c.Data(200, "application/yaml; charset=utf-8", openAPISpec)
	})
}
