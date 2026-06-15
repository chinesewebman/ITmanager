package integration

import (
	"gorm.io/gorm/clause"
)

// buildUpsertClause 构造 ON CONFLICT 子句：冲突时按 updateCols 更新。
// uniqueCol = 唯一键列名（如 "netbox_id"、"external_id"）。
// updateCols = 冲突后要 UPDATE 的列（如 Name、UpdatedAt、Status）。
//
// 跨方言：PostgreSQL/SQLite 都支持 ON CONFLICT(col) DO UPDATE SET ...
// MySQL 走 INSERT ... ON DUPLICATE KEY UPDATE（gorm 自动翻译）。
func buildUpsertClause(uniqueCol string, updateCols ...string) clause.OnConflict {
	updates := make(map[string]interface{}, len(updateCols))
	for _, c := range updateCols {
		updates[c] = clause.Expr{SQL: "EXCLUDED." + c}
	}
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: uniqueCol}},
		DoUpdates: clause.Assignments(updates),
	}
}
