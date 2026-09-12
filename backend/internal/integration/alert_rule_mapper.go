// Package integration - alert_rule_mapper
//
// M38-B: triggerid → rule_id 映射查询 helper。
//
// 设计：
//   - 单条 triggerid 查表（PK 索引，毫秒级返回）
//   - 返回 *uuid.UUID 指针，让调用方在 nil 分支走「未映射 → fallback」语义
//   - DB 错 → 也走 fallback（与 worker 的 fallback 行为一致：不漏告警）
//
// 抽成 package-level 函数而不是 method on IntegrationService 是为了两点：
//  1. SyncFromZabbix 与 worker.handleAlertEvent 都用（Round 5 + Round 8 单测都覆盖）
//  2. 单测可独立 mock（不引入 IntegrationService 全栈）
package integration

import (
	"context"
	"log"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LookupRuleIDByTrigger M38-B: 按 triggerid 查 rule_id
//
// 返回值语义：
//   - (uuidPtr=nil, err=nil)  → 映射表里没有（运维未配 / 已删）→ fallback 全启用 channels
//   - (uuidPtr=&X, err=nil)   → 命中 → 走过滤路径
//   - (nil, err)              → DB 异常：fallback 不漏告警；log warn 让运维修
//
// 实现细节：
//   - 直接走 PK 索引 SELECT，注意 alert_rule_trigger_map.triggerid 是 PK 而非外键；
//     没有 JOIN 需求，所有字段本地化。
//   - 不读 AlertRule 行：worker 端已经会按 RuleID 拿 NotifyChannels/NotifyUsers
//     （避免 N+1 + 透传时一致性问题）
func LookupRuleIDByTrigger(ctx context.Context, db *gorm.DB, triggerid string) (*uuid.UUID, error) {
	if triggerid == "" {
		return nil, nil
	}
	var ruleID uuid.UUID
	err := db.WithContext(ctx).
		Table("alert_rule_trigger_map").
		Select("rule_id").
		Where("triggerid = ?", triggerid).
		Scan(&ruleID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		log.Printf("[M38-B LookupRuleIDByTrigger] triggerid=%s lookup failed: %v → fallback", triggerid, err)
		return nil, err
	}
	return &ruleID, nil
}
