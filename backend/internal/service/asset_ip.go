package service

import "network-monitor-platform/internal/models"

// M63 (G-UI-AssetIpPersistence): 资产的 IP 存在 `asset_networks`（一个资产 1:N 张网卡），
// 但列表、详情、复盘报告头要的都是「主 IP」**一个值**。判据只有一条：
//
//	先第一个非空 IPv4，否则第一个非空 IPv6；都没有 → nil
//
// 为什么单独一个文件/两个函数，而不是在各自调用点各写一遍 for：
//   - 消费方已经有两个（`assetService` 的 List/Get 投影 `ip_address`、
//     `PostmortemService.fetchIP` 填报告头），说的是同一批数据的同一个地址 ——
//     两份实现漂移的后果是「资产列表显示 A、复盘报告头写 B」，运维拿着两个地址去 ping。
//   - 与 `integration/truncate.go` 同一形态：跨调用点共用的纯判据提出来，
//     调用点只保留各自的 IO 差异（谁查库、空值编码成 null 还是 ""）。
//
// 前置条件：入参必须是**已排序**的网卡（调用方 `ORDER BY created_at ASC, id ASC`，
// 见 assetService.listNetworks 的注释）。没有 ORDER BY 时 Postgres 不保证行序，
// 「第一张」就没有定义 —— 那时选中哪张卡是随机的（T-45）。

// pickPrimaryIP 取主 IP：v4 = 第一张非空 IPv4，v6 = 第一张非空 IPv6；都没有 → nil。
//
// 返回两个指针而不是单值：列表/详情只显示一个（用下面的 primaryIP），但「v4 与 v6 各是什么」
// 在需要并列展示时不必再查一次库。指针指向入参切片的元素，调用方不要再改这些元素。
func pickPrimaryIP(networks []models.AssetNetwork) (v4 *string, v6 *string) {
	for i := range networks {
		if networks[i].IPv4Address != "" {
			v4 = &networks[i].IPv4Address
			break
		}
	}
	for i := range networks {
		if networks[i].IPv6Address != "" {
			v6 = &networks[i].IPv6Address
			break
		}
	}
	return v4, v6
}

// primaryIP 单值投影：有 v4 用 v4，否则用 v6，都没有 → nil（JSON 里是 null）。
//
// 「v4 优先」这条优先级只在 pickPrimaryIP + 这里编码：List/Get 的投影与复盘报告的
// fetchIP 都走这个函数，不各自判一次。
func primaryIP(networks []models.AssetNetwork) *string {
	v4, v6 := pickPrimaryIP(networks)
	if v4 != nil {
		return v4
	}
	return v6
}
