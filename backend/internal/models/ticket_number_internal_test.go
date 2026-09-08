package models

import "testing"

// TestSeqLabel 覆盖工单号序号标签的进位边界（缺陷 D-2 的根因修复点）。
// 原实现用全表 Count()%26，第 27 张工单会退回 'A'，撞 ticket_number 唯一索引。
func TestSeqLabel(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "A"},
		{1, "B"},
		{24, "Y"},
		{25, "Z"},
		{26, "AA"}, // 原实现在这里退回 "A"
		{27, "AB"},
		{51, "AZ"},
		{52, "BA"},
		{701, "ZZ"},
		{702, "AAA"},
		{-1, "A"}, // 负值按 0 处理，不产生越界字节
	}
	for _, c := range cases {
		if got := seqLabel(c.n); got != c.want {
			t.Errorf("seqLabel(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestSeqLabel_单调递增 保证 0..2000 无重复（进位实现错误会产生撞号）。
func TestSeqLabel_单调递增(t *testing.T) {
	seen := make(map[string]bool, 2001)
	for n := int64(0); n <= 2000; n++ {
		s := seqLabel(n)
		if seen[s] {
			t.Fatalf("seqLabel(%d) = %q 与前序重复", n, s)
		}
		seen[s] = true
	}
}
