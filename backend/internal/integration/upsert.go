package integration

import (
	"gorm.io/gorm/clause"
)

// buildUpsertClause 构造 ON CONFLICT 子句：冲突时按 updateCols 更新。
//
// 参数一律是 **DB 列名**（snake_case，如 "net_box_id"、"name"、"updated_at"），
// 不是 Go 字段名。传 Go 字段名（"Name"）在 PG 上是硬错误
// `42703 column "Name" does not exist`，但在 sqlite 上会因列名解析大小写不敏感而
// **静默成功**（`"Name"` 命中 `name`）—— 所以单测必须断言渲染出的 SQL 字符串，
// 只靠 sqlite 功能测试挡不住这类回归（见 upsert_test.go / docs/FIX-PLAN-NETBOX-UPSERT.md §1.3）。
//
// uniqueCol 上必须有 UNIQUE 约束或唯一索引，否则 PG 报 `42P10 no unique or exclusion
// constraint matching the ON CONFLICT specification`。
//
// updateCols 为空时走 DO NOTHING：不能留空 DoUpdates —— gorm 会渲染 `SET "id"="id"`，
// 在 PG 上 `ON CONFLICT DO UPDATE` 的 SET 表达式里 id 同时存在于目标表与 excluded，
// 直接报 `42702 column reference "id" is ambiguous`（实测）。
func buildUpsertClause(uniqueCol string, updateCols ...string) clause.OnConflict {
	oc := clause.OnConflict{Columns: []clause.Column{{Name: uniqueCol}}}
	if len(updateCols) == 0 {
		oc.DoNothing = true
		return oc
	}
	oc.DoUpdates = clause.AssignmentColumns(updateCols)
	return oc
}
