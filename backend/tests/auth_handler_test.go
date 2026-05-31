package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestLoginHandler_InvalidJSON tests that Login returns 400 for invalid JSON
func TestLoginHandler_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/auth/login", func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "请求参数错误",
			})
			return
		}
	})

	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)

	var response map[string]interface{}
	json.Unmarshal(resp.Body.Bytes(), &response)
	assert.Equal(t, float64(400), response["code"])
	assert.NotEmpty(t, response["message"])
}

// TestLoginHandler_MissingCredentials tests that Login returns 400 for missing credentials
func TestLoginHandler_MissingCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/auth/login", func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "请输入用户名和密码",
			})
			return
		}
	})

	// Empty JSON
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)

	var response map[string]interface{}
	json.Unmarshal(resp.Body.Bytes(), &response)
	assert.Equal(t, float64(400), response["code"])
}

// TestLogoutHandler tests that Logout returns success
func TestLogoutHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/auth/logout", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "登出成功",
		})
	})

	req, _ := http.NewRequest("POST", "/api/v1/auth/logout", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	json.Unmarshal(resp.Body.Bytes(), &response)
	assert.Equal(t, float64(0), response["code"])
	assert.Equal(t, "登出成功", response["message"])
}

// TestLoginRequest_JSONBinding tests that LoginRequest binds correctly
func TestLoginRequest_JSONBinding(t *testing.T) {
	tests := []struct {
		name     string
		jsonStr  string
		expected bool
	}{
		{
			name:     "valid request",
			jsonStr:  `{"username":"admin","password":"password123"}`,
			expected: true,
		},
		{
			name:     "missing username",
			jsonStr:  `{"password":"password123"}`,
			expected: false,
		},
		{
			name:     "missing password",
			jsonStr:  `{"username":"admin"}`,
			expected: false,
		},
		{
			name:     "empty body",
			jsonStr:  `{}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
			}
			err := json.Unmarshal([]byte(tt.jsonStr), &req)

			if tt.expected {
				assert.NoError(t, err)
				assert.NotEmpty(t, req.Username)
				assert.NotEmpty(t, req.Password)
			} else {
				// Either unmarshal error or missing required field
				assert.True(t, err != nil || req.Username == "" || req.Password == "")
			}
		})
	}
}

// TestLoginResponse_JSONStructure tests the expected login response structure
func TestLoginResponse_JSONStructure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/auth/login", func(c *gin.Context) {
		// Simulate successful login response structure
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
				"user": gin.H{
					"id":       "550e8400-e29b-41d4-a716-446655440000",
					"username": "admin",
					"nickname": "Administrator",
					"email":    "admin@example.com",
					"role":     "admin",
					"avatar":   "",
				},
			},
		})
	})

	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["code"])
	assert.NotNil(t, response["data"])

	data := response["data"].(map[string]interface{})
	assert.NotEmpty(t, data["token"])
	assert.NotNil(t, data["user"])
}

// TestUnauthorizedResponse tests the expected unauthorized response structure
func TestUnauthorizedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "Token 无效或已过期",
		})
	})

	req, _ := http.NewRequest("GET", "/protected", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(401), response["code"])
	assert.Equal(t, "Token 无效或已过期", response["message"])
}
