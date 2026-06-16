package models

import (
	"time"

	"github.com/google/uuid"
)

// MetricSnapshot 指标快照（Zabbix / 自定义采集器兜底）
// 时序数据，TimescaleDB 优化（生产）；sqlite 测试用 TEXT DATETIME
type MetricSnapshot struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	AssetID   uuid.UUID `json:"asset_id" gorm:"type:uuid;index;not null"`
	Key       string    `json:"key" gorm:"size:100;index;not null"` // cpu.user / mem.used / disk.io / net.rx ...
	Value     float64   `json:"value" gorm:"not null"`
	TS        time.Time `json:"ts" gorm:"index;not null"` // 指标采集时间
	CreatedAt time.Time `json:"created_at"`
}

// TableName override
func (MetricSnapshot) TableName() string {
	return "metric_snapshots"
}
