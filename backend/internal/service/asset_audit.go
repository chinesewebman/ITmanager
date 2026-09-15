package service

import (
	"context"
	"sort"

	"gorm.io/gorm"
)

// IPConflictRow 一条跨资产同 IP 冲突. Count = 同一 IP 出现在多少 network 行 (跨资产合计).
type IPConflictRow struct {
	IP       string
	Kind     string // "ipv4" / "ipv6"
	Count    int
	AssetIDs []string
	Networks []string
}

// AuditIPConflicts 跨资产同 IP 巡检 (M70 loop 第 2 cycle).
//
// 范围: SELECT 跨资产同 ipv4_address / ipv6_address, 输出冲突报告. 不自动修.
//
// 业务: M68/M69 守卫只在写入时检查. 历史数据可能存在跨资产同 IP 情况.
// 这个函数是 M70 一次性巡检工具的核心, 让操作者拍决策 (删 / 改 / 接受).
func (s *assetService) AuditIPConflicts(ctx context.Context) ([]IPConflictRow, error) {
	var all []IPConflictRow
	for _, kind := range []string{"ipv4", "ipv6"} {
		col := "ipv4_address"
		if kind == "ipv6" {
			col = "ipv6_address"
		}
		var grouped []struct {
			IP    string
			Count int
		}
		// GROUP BY 同 IP, HAVING >= 2 = 至少 2 个 network 行共用. 跨资产同 IP 必满足.
		// 空串 ('') 被 WHERE 排掉, 这是守卫的隐含语义: 没填的 IP 不参与比对.
		// tx 不用: 巡检不要求 atomic, 单条 SELECT, 与业务写入解耦.
		err := s.db.WithContext(ctx).Raw(
			"SELECT "+col+" AS ip, COUNT(*) AS count "+
				"FROM asset_networks "+
				"WHERE "+col+" <> '' "+
				"GROUP BY "+col+" "+
				"HAVING COUNT(*) >= 2 "+
				"ORDER BY count DESC, "+col+" ASC",
		).Scan(&grouped).Error
		if err != nil {
			return nil, err
		}
		for _, g := range grouped {
			var networkRows []struct {
				ID      string
				AssetID string
			}
			err := s.db.WithContext(ctx).Raw(
				"SELECT id, asset_id FROM asset_networks WHERE "+col+" = ? ORDER BY asset_id, id",
				g.IP,
			).Scan(&networkRows).Error
			if err != nil {
				return nil, err
			}
			assetSet := map[string]bool{}
			networks := []string{}
			for _, n := range networkRows {
				assetSet[n.AssetID] = true
				networks = append(networks, n.ID)
			}
			assetIDs := []string{}
			for a := range assetSet {
				assetIDs = append(assetIDs, a)
			}
			sort.Strings(assetIDs)
			sort.Strings(networks)
			all = append(all, IPConflictRow{
				IP:       g.IP,
				Kind:     kind,
				Count:    g.Count,
				AssetIDs: assetIDs,
				Networks: networks,
			})
		}
	}
	return all, nil
}

// 防止 go vet 报 unused import 当 audit_audit_test.go 不存在时
var _ = gorm.ErrRecordNotFound
