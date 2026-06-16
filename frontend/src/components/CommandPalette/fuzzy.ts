// 简单 fuzzy 搜索（小改进 #3：Cmd+K 全局搜索）
// 算法：子序列匹配 + 连续 / 起始位置加权
//   - query 的每个字符必须在 text 中按顺序出现
//   - 连续匹配的字符加权重（避免 "cde" 匹配分散字符）
//   - 起始位置匹配的字符额外加分（"web" 偏好匹配 "web-server" 而非 "switch-web"）
//   - query 完全匹配 text → 最高分
// 不引第三方库（fuse.js 12KB gzip；本实现 ~50 行，覆盖 90% 场景）

export interface FuzzyResult<T> {
  item: T;
  score: number;
}

export function fuzzySearch<T>(
  query: string,
  items: T[],
  getText: (item: T) => string,
): FuzzyResult<T>[] {
  if (!query.trim()) return items.map((item) => ({ item, score: 0 }));
  const q = query.toLowerCase();
  const results: FuzzyResult<T>[] = [];
  for (const item of items) {
    const score = scoreMatch(q, getText(item).toLowerCase());
    if (score > 0) results.push({ item, score });
  }
  results.sort((a, b) => b.score - a.score);
  return results;
}

function scoreMatch(query: string, text: string): number {
  if (!query) return 0;
  // 完整包含（大小写不敏感）→ 高分
  if (text.includes(query)) {
    // 起始包含更高分
    return text.startsWith(query) ? 100 : 60;
  }
  // 子序列匹配
  let qi = 0;
  let score = 0;
  let lastMatch = -2;
  for (let ti = 0; ti < text.length && qi < query.length; ti++) {
    if (text[ti] === query[qi]) {
      // 连续 bonus
      if (ti === lastMatch + 1) score += 5;
      else score += 1;
      // 起始 bonus
      if (ti === 0) score += 3;
      lastMatch = ti;
      qi++;
    }
  }
  return qi === query.length ? score : 0;
}
