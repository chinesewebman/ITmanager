package service

import (
	"context"
	"errors"
	"time"

	"network-monitor-platform/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OncallService 值班 + 升级服务（P1-2）
//
// 数据流：
//   - schedules: 值班组
//   - shifts: 班次（schedule + user + 起止时间）
//   - policies: 升级策略
//   - levels: 升级层级
//
// 核心查询：
//   - GetCurrentOncall(now): 找出当前在班的 user（按 schedule）
//   - ProcessEscalation(alertID, now): 查 policy → 找出每个 level 该通知谁
type OncallService struct {
	db *gorm.DB
}

func NewOncallService(db *gorm.DB) *OncallService {
	return &OncallService{db: db}
}

// ==================== Schedule CRUD ====================

func (s *OncallService) CreateSchedule(ctx context.Context, sched *models.OncallSchedule) error {
	if sched.Name == "" {
		return errors.New("name 不能为空")
	}
	if sched.ID == uuid.Nil {
		sched.ID = uuid.New()
	}
	return s.db.WithContext(ctx).Create(sched).Error
}

func (s *OncallService) ListSchedules(ctx context.Context) ([]models.OncallSchedule, error) {
	var out []models.OncallSchedule
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *OncallService) GetSchedule(ctx context.Context, id uuid.UUID) (*models.OncallSchedule, error) {
	var sched models.OncallSchedule
	if err := s.db.WithContext(ctx).First(&sched, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sched, nil
}

func (s *OncallService) DeleteSchedule(ctx context.Context, id uuid.UUID) error {
	// audit-P1: 两步 DELETE 包事务, 防止 schedule 删了 shifts 残留 (孤儿数据)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&models.OncallSchedule{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Where("schedule_id = ?", id).Delete(&models.OncallShift{}).Error
	})
}

// ==================== Shift CRUD ====================

func (s *OncallService) CreateShift(ctx context.Context, shift *models.OncallShift) error {
	if shift.ScheduleID == uuid.Nil {
		return errors.New("schedule_id 必填")
	}
	if shift.UserID == uuid.Nil {
		return errors.New("user_id 必填")
	}
	if !shift.EndsAt.After(shift.StartsAt) {
		return errors.New("ends_at 必须 > starts_at")
	}
	if shift.ID == uuid.Nil {
		shift.ID = uuid.New()
	}
	return s.db.WithContext(ctx).Create(shift).Error
}

func (s *OncallService) ListShifts(ctx context.Context, scheduleID uuid.UUID) ([]models.OncallShift, error) {
	var out []models.OncallShift
	if err := s.db.WithContext(ctx).
		Where("schedule_id = ?", scheduleID).
		Order("starts_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *OncallService) DeleteShift(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.OncallShift{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ==================== GetCurrentOncall ====================

// GetCurrentOncall 返回现在正在值班的 user
//
// 逻辑：在所有 enabled schedule 中，找 starts_at <= now <= ends_at 的 shift
// 多个 schedule 同时值班时返回多个结果
func (s *OncallService) GetCurrentOncall(ctx context.Context, now time.Time) ([]models.OncallCurrent, error) {
	var rows []struct {
		ShiftID      uuid.UUID
		ScheduleID   uuid.UUID
		ScheduleName string
		UserID       uuid.UUID
		UserName     string
		StartsAt     time.Time
		EndsAt       time.Time
	}
	err := s.db.WithContext(ctx).
		Table("oncall_shifts s").
		Select("s.id as shift_id, s.schedule_id, sc.name as schedule_name, s.user_id, s.user_name, s.starts_at, s.ends_at").
		Joins("JOIN oncall_schedules sc ON sc.id = s.schedule_id").
		Where("s.starts_at <= ? AND s.ends_at >= ? AND sc.enabled = ?", now, now, true).
		Order("s.starts_at ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]models.OncallCurrent, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.OncallCurrent{
			ScheduleID:   r.ScheduleID,
			ScheduleName: r.ScheduleName,
			UserID:       r.UserID,
			UserName:     r.UserName,
			StartsAt:     r.StartsAt,
			EndsAt:       r.EndsAt,
			ShiftID:      r.ShiftID,
		})
	}
	return out, nil
}

// ==================== Escalation Policy CRUD ====================

func (s *OncallService) CreatePolicy(ctx context.Context, policy *models.EscalationPolicy) error {
	if policy.Name == "" {
		return errors.New("name 不能为空")
	}
	if policy.ID == uuid.Nil {
		policy.ID = uuid.New()
	}
	// M4-5 审计 P2 修复: policy + levels 包事务, 防部分写入产生半成品 policy
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(policy).Error; err != nil {
			return err
		}
		for i := range policy.Levels {
			level := &policy.Levels[i]
			if level.PolicyID == uuid.Nil {
				level.PolicyID = policy.ID
			}
			if level.ID == uuid.Nil {
				level.ID = uuid.New()
			}
			if err := tx.Create(level).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *OncallService) ListPolicies(ctx context.Context) ([]models.EscalationPolicy, error) {
	var policies []models.EscalationPolicy
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&policies).Error; err != nil {
		return nil, err
	}
	// 一次性拉所有 levels
	if len(policies) == 0 {
		return policies, nil
	}
	policyIDs := make([]uuid.UUID, 0, len(policies))
	for _, p := range policies {
		policyIDs = append(policyIDs, p.ID)
	}
	var allLevels []models.EscalationLevel
	if err := s.db.WithContext(ctx).
		Where("policy_id IN ?", policyIDs).
		Order("policy_id, level ASC").
		Find(&allLevels).Error; err != nil {
		return nil, err
	}
	// group by policy
	byPolicy := make(map[uuid.UUID][]models.EscalationLevel, len(policies))
	for i := range allLevels {
		l := allLevels[i]
		byPolicy[l.PolicyID] = append(byPolicy[l.PolicyID], l)
	}
	for i := range policies {
		policies[i].Levels = byPolicy[policies[i].ID]
		if policies[i].Levels == nil {
			policies[i].Levels = []models.EscalationLevel{}
		}
	}
	return policies, nil
}

func (s *OncallService) GetPolicy(ctx context.Context, id uuid.UUID) (*models.EscalationPolicy, error) {
	var policy models.EscalationPolicy
	if err := s.db.WithContext(ctx).First(&policy, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.db.WithContext(ctx).
		Where("policy_id = ?", id).
		Order("level ASC").
		Find(&policy.Levels).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func (s *OncallService) DeletePolicy(ctx context.Context, id uuid.UUID) error {
	// M4-5 审计 P2 修复: 删 levels + policy 包事务, 防 levels 残留
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("policy_id = ?", id).Delete(&models.EscalationLevel{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&models.EscalationPolicy{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ==================== ProcessEscalation 升级评估 ====================

// EscalationStep 升级计划中的单个步骤
type EscalationStep struct {
	Level         int
	TargetType    string
	TargetID      string
	TargetName    string
	NotifyMethods []string
	WaitMinutes   int
}

// ProcessEscalation 给定 alert 触发时间 + 当前时间 + policy，返回应该通知谁
//
// 算法：找出所有 level，计算每个 level 的触发时间（triggerTime + 累计 wait_minutes）
// 返回触发时间 <= 当前时间 且尚未通知的 level
//
// 简化版：返回所有 level（实际生产应该持久化已通知状态做去重）
func (s *OncallService) ProcessEscalation(ctx context.Context, policyID uuid.UUID, alertTriggeredAt time.Time, now time.Time) ([]EscalationStep, error) {
	policy, err := s.GetPolicy(ctx, policyID)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return nil, errors.New("policy 已禁用")
	}
	if len(policy.Levels) == 0 {
		return nil, errors.New("policy 无 level 配置")
	}
	steps := make([]EscalationStep, 0, len(policy.Levels))
	for _, lv := range policy.Levels {
		// 解析 notify_methods 逗号分隔
		methods := splitNonEmpty(lv.NotifyMethods, ",")
		// 解析 target name（如果是 schedule 类型，GetCurrentOncall 查 user）
		targetName := lv.TargetID
		if lv.TargetType == "schedule" {
			schedID, err := uuid.Parse(lv.TargetID)
			if err == nil {
				schedule, _ := s.GetSchedule(ctx, schedID)
				if schedule != nil {
					targetName = schedule.Name
				}
			}
		}
		steps = append(steps, EscalationStep{
			Level:         lv.Level,
			TargetType:    lv.TargetType,
			TargetID:      lv.TargetID,
			TargetName:    targetName,
			NotifyMethods: methods,
			WaitMinutes:   lv.WaitMinutes,
		})
	}
	return steps, nil
}

func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	out := []string{}
	cur := ""
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			i += len(sep) - 1
			continue
		}
		cur += string(s[i])
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
