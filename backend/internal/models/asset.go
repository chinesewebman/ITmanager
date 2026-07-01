package models

import (
	"time"

	"github.com/google/uuid"
)

// Asset 资产
type Asset struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Name      string    `json:"name" gorm:"size:255;not null"`
	AssetTag  string    `json:"asset_tag" gorm:"size:100;index"`
	SN        string    `json:"sn" gorm:"size:100;index"`        // 序列号
	AssetType string    `json:"asset_type" gorm:"size:50;index"` // server, switch, router, firewall, storage
	Brand     string    `json:"brand" gorm:"size:100"`           // 品牌
	Model     string    `json:"model" gorm:"size:100"`           // 型号

	// 位置信息
	SiteID       *uuid.UUID `json:"site_id" gorm:"type:uuid"` // 机房
	SiteName     string     `json:"site_name" gorm:"size:100"`
	RackID       *uuid.UUID `json:"rack_id" gorm:"type:uuid"` // 机柜
	RackName     string     `json:"rack_name" gorm:"size:50"`
	RackPosition string     `json:"rack_position" gorm:"size:50"` // U位

	// 采购信息
	PurchaseDate  *time.Time `json:"purchase_date"`
	WarrantyEnd   *time.Time `json:"warranty_end"`
	Vendor        string     `json:"vendor" gorm:"size:255"`
	VendorContact string     `json:"vendor_contact" gorm:"size:255"`

	// 状态
	Status      string     `json:"status" gorm:"size:20;index;default:active"` // active, offline, maintenance, retired
	OnlineTime  *time.Time `json:"online_time"`
	OfflineTime *time.Time `json:"offline_time"`

	// B4: 软退役存档 — 退役时把 IP 移到 last_known_ip*, 然后清空 AssetNetwork.IP*
	// 详见 docs/TRAPS.md T-* + migrations/000011_asset_retire.up.sql
	LastKnownIP4  *string    `json:"last_known_ip4" gorm:"size:45"`
	LastKnownIP6  *string    `json:"last_known_ip6" gorm:"size:45"`
	RetiredAt     *time.Time `json:"retired_at" gorm:"index"`
	RetiredReason *string    `json:"retired_reason" gorm:"type:text"`
	RetiredBy     *uuid.UUID `json:"retired_by" gorm:"type:uuid"` // FK users.id (soft, no DB constraint)

	// 业务信息
	BusinessUnit string `json:"business_unit" gorm:"size:100"`
	ServiceName  string `json:"service_name" gorm:"size:100"`
	Tags         string `json:"tags" gorm:"type:jsonb"`          // JSON
	CustomFields string `json:"custom_fields" gorm:"type:jsonb"` // JSON

	// NetBox 关联
	NetBoxID *int   `json:"netbox_id" gorm:"index"`
	Source   string `json:"source" gorm:"size:50"` // netbox, zabbix, manual

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *Asset) TableName() string {
	return "assets"
}

// AssetNetwork 网络接口
type AssetNetwork struct {
	ID            uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	AssetID       uuid.UUID `json:"asset_id" gorm:"type:uuid;not null;index"`
	InterfaceName string    `json:"interface_name" gorm:"size:50;not null"`
	InterfaceType string    `json:"interface_type" gorm:"size:20"` // ethernet, fiber
	MACAddress    string    `json:"mac_address" gorm:"size:17;index"`
	IPv4Address   string    `json:"ipv4_address" gorm:"size:45;index"`
	IPv4Netmask   string    `json:"ipv4_netmask" gorm:"size:45"`
	IPv6Address   string    `json:"ipv_address" gorm:"size:45"`
	Speed         int       `json:"speed"`                        // Mbps
	Duplex        string    `json:"duplex" gorm:"size:20"`        // full, half
	Status        string    `json:"status" gorm:"size:20"`        // up, down, unknown
	ConnectedTo   string    `json:"connected_to" gorm:"size:255"` // 连接的设备
	ConnectedPort string    `json:"connected_port" gorm:"size:50"`
	Purpose       string    `json:"purpose" gorm:"size:50"` // mgmt, service
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (n *AssetNetwork) TableName() string {
	return "asset_networks"
}

// Rack 机柜
type Rack struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SiteID    uuid.UUID `json:"site_id" gorm:"type:uuid;not null;index"`
	SiteName  string    `json:"site_name" gorm:"size:100"`
	Name      string    `json:"name" gorm:"size:50;not null"`
	TotalU    int       `json:"total_u" gorm:"default:42"` // 总U位
	MaxWeight int       `json:"max_weight"`                // kg
	Floor     string    `json:"floor" gorm:"size:20"`
	Row       string    `json:"row" gorm:"size:20"`
	Column    string    `json:"column" gorm:"size:20"`
	Status    string    `json:"status" gorm:"size:20;default:active"`

	// NetBox 关联
	NetBoxID *int `json:"netbox_id" gorm:"index"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r *Rack) TableName() string {
	return "racks"
}

// Site 机房
type Site struct {
	ID           uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Name         string    `json:"name" gorm:"size:100;not null"`
	Code         string    `json:"code" gorm:"size:50;uniqueIndex"`
	Province     string    `json:"province" gorm:"size:50"`
	City         string    `json:"city" gorm:"size:50"`
	Address      string    `json:"address" gorm:"type:text"`
	Contact      string    `json:"contact" gorm:"size:100"`
	ContactPhone string    `json:"contact_phone" gorm:"size:50"`
	Tier         string    `json:"tier" gorm:"size:20"` // T1, T2, T3, T4
	IsActive     bool      `json:"is_active" gorm:"default:true"`

	// NetBox 关联
	NetBoxID *int `json:"netbox_id" gorm:"index"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Site) TableName() string {
	return "sites"
}
