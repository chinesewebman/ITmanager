// Package integration - alert_rule_mapper
//
// M38-B: triggerid → rule_id 映射查询 helper。
//
// 设计：
//   - 单条 triggerid 查表（PK 索引，毫秒级返回）
//   - 返回 *uuid.UUID 指针，让调用方在 nil 分支走「未映射 → fallback」语义
//   - DB 错 → 也走 fallback（与 worker 的 fallback 行为一致：不漏告警）
//
// 抽成 struct 而不是 free function 是为了两点：
//  1. SyncFromZabbix 与 worker.handleAlertEvent 都用 (Round 5 + Round 8 单测都覆盖)
//  2. 单测可独立构造 mapper (不引入 IntegrationService 全栈)；参数简化 → 排查 stack 简单
//
// UNIQUE(triggerid, source) 设计下，同一 triggerid 在 zabbix / manual 两条 source
// 行下可以指向不同 rule_id（生产侧 Zabbix 同步 vs 人工修正并存）。查询接口固定
// 带 source 参数；不传 source 视为「任意一行」—— 用 SQL `LIMIT 1` 取第一条，
// 不保证 stable order (与历史上「一条 triggerid 一条 rule」的语义尽量对齐)。

package integration

import (
	"context"
	"log"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AlertRuleMapper 按 triggerid 查 rule_id
//
// 用法：
//
//	m := NewAlertRuleMapper(db)
//	ruleID, err := m.LookupRuleIDByTriggerID(ctx, "12345", "zabbix")
type AlertRuleMapper struct {
	db *gorm.DB
}

// NewAlertRuleMapper 构造 mapper
func NewAlertRuleMapper(db *gorm.DB) *AlertRuleMapper {
	return &AlertRuleMapper{db: db}
}

// LookupRuleIDByTriggerID 按 (triggerid, source) 查 rule_id
//
// 返回值语义：
//   - (uuidPtr=nil, err=nil)  → 映射表里没有（运维未配 / 已删 / source 不匹配） → fallback
//   - (uuidPtr=&X, err=nil)   → 命中 → 走过滤路径
//   - (nil, err)              → DB 异常：fallback 不漏告警；log warn 让运维修
//
// 实现细节：
//   - 直接走 UNIQUE(triggerid, source) 索引, 一次 SELECT，无 JOIN。
//   - 不读 AlertRule 行：worker 端已经会按 RuleID 拿 NotifyChannels/NotifyUsers
//     （避免 N+1 + 透传时一致性问题）。
//   - source 空字符串 → SELECT triggerid=? AND source=” LIMIT 1（schema CHECK 允许
//     source=”, 不会过滤掉 — 这种用法保留是为「运维留一行手动修正不写 source」时
//     不至于误命中 zabbix 的同名 triggerid）。
func (m *AlertRuleMapper) LookupRuleIDByTriggerID(ctx context.Context, triggerid, source string) (*uuid.UUID, error) {
	if triggerid == "" {
		return nil, nil
	}
	var ruleIDStr string
	q := m.db.WithContext(ctx).
		Table("alert_rule_trigger_map").
		Select("rule_id").
		Where("triggerid = ?", triggerid)
	if source != "" {
		q = q.Where("source = ?", source)
	}
	err := q.Limit(1).Scan(&ruleIDStr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		log.Printf("[M38-B AlertRuleMapper] triggerid=%s source=%s lookup failed: %v → fallback", triggerid, source, err)
		return nil, err
	}
	if ruleIDStr == "" {
		return nil, nil // Scan 没找到行, 但不报错 — 视同 miss
	}
	parsed, parseErr := uuid.Parse(ruleIDStr)
	if parseErr != nil {
		log.Printf("[M38-B AlertRuleMapper] triggerid=%s source=%s bad uuid=%q: %v → fallback", triggerid, source, ruleIDStr, parseErr)
		return nil, nil
	}
	return &parsed, nil
}

// LookupRuleIDByTrigger 兼容老接口 (source 固定为 zabbix, 与历史 SyncFromZabbix 一致)
// Round 5 的 SyncFromZabbix 走这条；Round 8 单测走 LookupRuleIDByTriggerID。
//
// 为什么还保留：commit 5 当时已用此接口 + GORM Scan 的语义能在 triggerid 不存在时返 ErrRecordNotFound
// 但表是空表 (no row) 时 Scan 不报错只是 ruleID 留零值——需要单独「结果非零值」判断。
// Round 8 测试覆盖走 source-filtered 的新接口, 这里保留旧接口不破 commit 5 既有调用。
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
	if ruleID == uuid.Nil {
		return nil, nil // Scan 没找到行, 但不报错 — 视同 miss
	}
	return &ruleID, nil
}
