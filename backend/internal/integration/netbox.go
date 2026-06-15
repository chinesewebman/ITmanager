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

// SyncDevices C-P7：ctx 透传。
func (c *NetBoxClient) SyncDevices(ctx context.Context) ([]NetBoxDevice, error) {
	body, _, err := c.c.Do(ctx, "GET", "/api/dcim/devices/", nil)
	if err != nil {
		return nil, fmt.Errorf("NetBox SyncDevices: %w", err)
	}
	var result struct {
		Results []NetBoxDevice `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("NetBox 解析失败: %w", err)
	}
	return result.Results, nil
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
