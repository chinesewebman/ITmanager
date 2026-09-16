package integration

// 同步结果键的跨语言契约常量。
//
// 历史背景 (TODO.md G-41, M27/B 收尾自查发现)：
//   后端 integration/service.go 的 results = map[string]int{"zabbix": count, "zabbix_truncated": truncated}
//   与前端 Settings.tsx 的 res?.data?.data?.synced?.zabbix_truncated ?? 0
//   各写一遍字面量 —— 改名/拼写漂移会静默失效：前端读不到 → ?? 0 兜底 →
//   UI 永远显示「未截断」，运维错过 6000-1=5999 条丢告警的真相。
//
// 本文件把这类「跨语言裸字符串约定」固化成包级常量 + 跨语言契约测试
// (sync_keys_test.go) 守住「两端必须漂移同步 + 不可写回裸字符串」。
//
// 同族 (glpi_skipped / netbox / glpi / *_field_truncations) 仍是裸字符串，
// 沿用 TODO.md G-57 登记不修 —— 属 M33 §6 残余 (data.synced 自由形态的 schema
// 收口范围)，本 round scope 仅 zabbix_truncated。
const (
	// KeyZabbixTruncated 是 data.synced 字典里的 Zabbix 源侧 0/1 截断标志。
	//
	// 语义 (M27/D-6 + M33/D-6)：
	//   - 0 = 源侧进行中告警数 ≤ zabbixTriggerLimit，全部入库
	//   - 1 = 源侧超过 zabbixTriggerLimit，被截断（实际请求了 limit+1 让边界可判定）
	//
	// 这是**标志**（不是条数）—— UI 文案必须**不**插值这个数
	// (Settings.tsx handleSyncZabbix 注释详)。失败分支不写该键
	// (与 glpi_skipped 同形，前端 ?? 0 兜住)。
	//
	// 契约：值必须与 frontend/src/services/syncKeys.ts 的 SYNC_KEY_ZABBIX_TRUNCATED 一致。
	// 契约测试：sync_keys_test.go TestKeyZabbixTruncated_CrossLangWithTS + TestKeyZabbixTruncated_InOpenAPISync。
	KeyZabbixTruncated = "zabbix_truncated"
)
