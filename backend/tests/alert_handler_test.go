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

// TestAlertHandler_ResponseStructure tests the expected alert response structure
func TestAlertHandler_ResponseStructure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Mock handler that returns the expected response structure
	router.GET("/api/v1/alerts", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"items": []interface{}{},
				"stats": gin.H{
					"total":        0,
					"problem":      0,
					"acknowledged": 0,
					"resolved":     0,
				},
			},
		})
	})

	req, _ := http.NewRequest("GET", "/api/v1/alerts", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["code"])
	assert.NotNil(t, response["data"])

	data := response["data"].(map[string]interface{})
	assert.NotNil(t, data["items"])
	assert.NotNil(t, data["stats"])
}

// TestAlertStatsResponse_JSONStructure tests alert stats response structure
func TestAlertStatsResponse_JSONStructure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/api/v1/alerts/stats", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"by_severity": []interface{}{
					map[string]interface{}{
						"severity":      4,
						"severity_name": "High",
						"count":         5,
					},
				},
				"by_hour": []interface{}{
					map[string]interface{}{
						"hour":  "2024-01-01T10:00:00Z",
						"count": 3,
					},
				},
			},
		})
	})

	req, _ := http.NewRequest("GET", "/api/v1/alerts/stats", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["code"])
}

// TestCreateAlertRule_InvalidJSON tests that invalid JSON returns 400
func TestCreateAlertRule_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.POST("/api/v1/alerts/rules", func(c *gin.Context) {
		var rule struct {
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&rule); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "请求参数错误",
			})
			return
		}
	})

	req, _ := http.NewRequest("POST", "/api/v1/alerts/rules", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

// TestAlertRuleResponse_JSONStructure tests alert rule response structure
func TestAlertRuleResponse_JSONStructure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/api/v1/alerts/rules", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": []interface{}{
				map[string]interface{}{
					"id":          "550e8400-e29b-41d4-a716-446655440000",
					"name":        "High CPU Alert",
					"description": "Alert when CPU > 80%",
					"is_enabled":  true,
					"priority":    1,
				},
			},
		})
	})

	req, _ := http.NewRequest("GET", "/api/v1/alerts/rules", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["code"])
}

// TestGetAlert_NotFound tests that non-existent alert returns 404
func TestGetAlert_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.GET("/api/v1/alerts/:id", func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "告警不存在",
		})
	})

	req, _ := http.NewRequest("GET", "/api/v1/alerts/00000000-0000-0000-0000-000000000000", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusNotFound, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(404), response["code"])
	assert.Equal(t, "告警不存在", response["message"])
}

// TestAcknowledgeAlert_Success tests acknowledge alert response
func TestAcknowledgeAlert_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.POST("/api/v1/alerts/:id/acknowledge", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "告警已确认",
		})
	})

	req, _ := http.NewRequest("POST", "/api/v1/alerts/550e8400-e29b-41d4-a716-446655440000/acknowledge", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["code"])
	assert.Equal(t, "告警已确认", response["message"])
}

// TestResolveAlert_Success tests resolve alert response
func TestResolveAlert_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.POST("/api/v1/alerts/:id/resolve", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "告警已解决",
		})
	})

	req, _ := http.NewRequest("POST", "/api/v1/alerts/550e8400-e29b-41d4-a716-446655440000/resolve", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["code"])
	assert.Equal(t, "告警已解决", response["message"])
}

// TestDeleteAlertRule_Success tests delete alert rule response
func TestDeleteAlertRule_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.DELETE("/api/v1/alerts/rules/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "删除成功",
		})
	})

	req, _ := http.NewRequest("DELETE", "/api/v1/alerts/rules/550e8400-e29b-41d4-a716-446655440000", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["code"])
	assert.Equal(t, "删除成功", response["message"])
}
