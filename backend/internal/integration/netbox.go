package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"network-monitor-platform/internal/config"

	"github.com/google/uuid"
)

// NetBoxClient NetBox 客户端
type NetBoxClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewNetBoxClient 创建 NetBox 客户端
func NewNetBoxClient(cfg *config.NetboxConfig) *NetBoxClient {
	return &NetBoxClient{
		baseURL: cfg.URL,
		token:   cfg.Token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Device NetBox 设备
type NetBoxDevice struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	DeviceType struct {
		ID   int    `json:"id"`
		Slug string `json:"slug"`
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

// SyncDevices 从 NetBox 同步设备
func (c *NetBoxClient) SyncDevices() ([]NetBoxDevice, error) {
	url := fmt.Sprintf("%s/api/dcim/devices/", c.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Token %s", c.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NetBox 返回错误状态码: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		Results []NetBoxDevice `json:"results"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return result.Results, nil
}

// NetBoxAsset 将 NetBox 设备转换为资产
type NetBoxAsset struct {
	Name         string  `json:"name"`
	AssetType    string  `json:"asset_type"`
	Status       string  `json:"status"`
	SiteID       *uuid.UUID `json:"site_id"`
	SiteName     string  `json:"site_name"`
	RackID       *uuid.UUID `json:"rack_id"`
	RackName     string  `json:"rack_name"`
	Brand        string  `json:"brand"`
	Model        string  `json:"model"`
	SN           string  `json:"sn"`
	NetboxID     *int    `json:"netbox_id"`
	Source       string  `json:"source"`
}

// ConvertToAsset 转换为资产格式
func (d *NetBoxDevice) ConvertToAsset() *NetBoxAsset {
	asset := &NetBoxAsset{
		Name:      d.Name,
		Source:    "netbox",
		NetboxID:  &d.ID,
		Status:    "active",
	}

	// 根据设备角色判断类型
	switch d.DeviceRole.Slug {
	case "server":
		asset.AssetType = "server"
	case "switch", "router", "firewall":
		asset.AssetType = "network"
	default:
		asset.AssetType = "other"
	}

	// 设置品牌和型号
	asset.Brand = d.DeviceType.Slug
	asset.Model = d.DeviceType.Model
	asset.SN = d.SerialNumber

	// 设置站点名称
	asset.SiteName = d.Site.Name

	return asset
}
