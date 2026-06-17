package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"network-monitor-platform/internal/config"
	"network-monitor-platform/internal/httpx"

	"github.com/google/uuid"
)

// NetBoxClient NetBox 客户端（C-P7：走 httpx，集成 retry/熔断/metrics/ctx）。
type NetBoxClient struct {
	c *httpx.Client
}

// NewNetBoxClient 创建 NetBox 客户端。
func NewNetBoxClient(cfg *config.NetboxConfig, m httpx.MetricsRecorder) *NetBoxClient {
	hcfg := httpx.DefaultConfig(cfg.URL)
	hcfg.HeaderName = "Authorization"
	hcfg.HeaderValue = fmt.Sprintf("Token %s", cfg.Token)
	hcfg.Timeout = 30 * time.Second // 同步任务允许更久
	return &NetBoxClient{c: httpx.New(hcfg, "netbox", m)}
}

// Device NetBox 设备
type NetBoxDevice struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	DeviceType  struct {
		ID    int    `json:"id"`
		Slug  string `json:"slug"`
		Model string `json:"model"`
	} `json:"device_type"`
	DeviceRole struct {
		ID   int    `json:"id"`
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"device_role"`
	Site struct {
		ID   int    `json:"id"`
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"site"`
	Status struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"status"`
	PrimaryIP    string `json:"primary_ip"`
	SerialNumber string `json:"serial_number"`
	Comments     string `json:"comments"`
}

// NetBoxPagination NetBox API 分页响应
type NetBoxPagination struct {
	Count    int `json:"count"`
	Next     any `json:"next"`
	Previous any `json:"previous"`
}

// SyncDevices C-P7：ctx 透传。P1-审计：分页循环直到拉完（NetBox 默认 page_size=50）
func (c *NetBoxClient) SyncDevices(ctx context.Context) ([]NetBoxDevice, error) {
	const pageSize = 100 // 单页 100 条，平衡 QPS 和 DB 写放大
	var all []NetBoxDevice
	offset := 0

	for {
		// NetBox 分页参数: ?limit=N&offset=N
		path := fmt.Sprintf("/api/dcim/devices/?limit=%d&offset=%d", pageSize, offset)
		body, _, err := c.c.Do(ctx, "GET", path, nil)
		if err != nil {
			return nil, fmt.Errorf("NetBox SyncDevices (offset=%d): %w", offset, err)
		}
		var result struct {
			Count   int            `json:"count"`
			Next    string         `json:"next"`
			Results []NetBoxDevice `json:"results"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("NetBox 解析失败 (offset=%d): %w", offset, err)
		}

		all = append(all, result.Results...)

		// 终止条件：当前页不足 pageSize 或累计达 count
		if len(result.Results) < pageSize || len(all) >= result.Count {
			break
		}
		offset += pageSize

		// 安全护栏：避免死循环（NetBox 异常返回）
		if offset > 100000 {
			return nil, fmt.Errorf("NetBox 分页异常：offset=%d > 100000", offset)
		}
	}

	return all, nil
}

// NetBoxAsset 将 NetBox 设备转换为资产
type NetBoxAsset struct {
	Name      string
	AssetType string
	Status    string
	SiteID    *uuid.UUID
	SiteName  string
	RackID    *uuid.UUID
	RackName  string
	Brand     string
	Model     string
	SN        string
	NetboxID  *int
	Source    string
}

// ConvertToAsset 转换为资产格式
func (d *NetBoxDevice) ConvertToAsset() *NetBoxAsset {
	asset := &NetBoxAsset{
		Name:     d.Name,
		Source:   "netbox",
		NetboxID: &d.ID,
		Status:   "active",
	}

	switch d.DeviceRole.Slug {
	case "server":
		asset.AssetType = "server"
	case "switch", "router", "firewall":
		asset.AssetType = "network"
	default:
		asset.AssetType = "other"
	}

	asset.Brand = d.DeviceType.Slug
	asset.Model = d.DeviceType.Model
	asset.SN = d.SerialNumber
	asset.SiteName = d.Site.Name

	return asset
}
