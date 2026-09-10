package models

import (
	"time"

	"github.com/google/uuid"
)

// TicketHistory 工单经手历史 —— 一行 = 一次请求里一个字段的一次变更。
//
// 建表理由与形状取舍见 migrations/000025_ticket_history.up.sql 的头部注释
// （以及 docs/FIX-PLAN-TICKET-HISTORY.md）。这里只记**代码层要守的不变式**：
//
//   - **append-only**：本表只 INSERT，没有任何 UPDATE/DELETE 路径。
//   - 同一次请求产生的 N 行共享同一个 BatchID —— 没有它，UI 只能靠「created_at 相同」
//     猜分组，同秒的两次操作会并成一次。
//   - Kind=created 的出生行 FieldName 为 NULL（那一次改的不是某个字段，而是「这张票存在了」）。
//   - OldValue/NewValue 是**文本快照**：时间戳按 RFC3339 存，超长文本截断后带标记。
//   - ActorID 是**裸 UUID、无外键**（同 audit_logs.resource_id）：这是唯一现实的
//     「历史插入失败」来源，而失败策略是「与 UPDATE 同事务、失败即整单回滚」——
//     建了 FK，带外删号后未过期 token 仍带 user_id（middleware/auth.go 只信 claims、
//     不复查用户存在），该用户此后每次改工单都会 500。
//
// ⚠️ TicketID 用 ON DELETE CASCADE：工单被硬删时历史随之消失。当前全仓**没有**工单
// 删除端点，故不可达；**将来若新增工单删除端点，必须先重新评估这里**。
//
// 本表**没有 UpdatedAt** —— 既是 append-only 的语义（没有「最后修改」这回事），
// 也顺带避开 GORM 对 map 更新无条件补 updated_at 的行为。
type TicketHistory struct {
	ID        uuid.UUID  `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TicketID  uuid.UUID  `json:"ticket_id" gorm:"type:uuid;not null;index"`
	BatchID   uuid.UUID  `json:"batch_id" gorm:"type:uuid;not null"`
	Kind      string     `json:"kind" gorm:"size:20;not null"` // created | updated
	FieldName *string    `json:"field_name" gorm:"size:50"`    // kind=updated 时非空
	OldValue  *string    `json:"old_value" gorm:"type:text"`
	NewValue  *string    `json:"new_value" gorm:"type:text"`
	ActorID   *uuid.UUID `json:"actor_id" gorm:"type:uuid"` // 裸 UUID，无 FK，见上
	ActorName string     `json:"actor_name" gorm:"size:100"`
	Source    string     `json:"source" gorm:"size:20"` // 开放值：manual/email/api/glpi/alert/zabbix
	RequestID string     `json:"request_id" gorm:"size:50"`
	CreatedAt time.Time  `json:"created_at"`
}

// TableName 必须显式写：GORM 默认把 TicketHistory 复数化成 ticket_histories，
// 与迁移建的 ticket_history 不符 → 运行时报 relation does not exist。
// （全仓 19 个模型都显式实现了 TableName，这里是同一个理由。）
func (TicketHistory) TableName() string {
	return "ticket_history"
}

// 历史行的 Kind 取值。
const (
	TicketHistoryKindCreated = "created"
	TicketHistoryKindUpdated = "updated"
)
