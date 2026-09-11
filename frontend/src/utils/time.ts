// 时间格式化唯一出口（FIX-PLAN-UI-PERF §W2）。
//
// 此前四种口径并存：AlertTable 原样渲染后端 RFC3339、Settings 用无 locale 的
// toLocaleString()、AssetTimeline 用 zh-CN + hour12:false、Oncall 用 zh-CN 默认
// hour12。同一个事件在不同页显示不同格式，排障对齐时间线时会被误导。
//
// dayjs 早已在 package.json 依赖里（antd 自带），此前全仓零使用——这里用它，
// 不新增任何包。
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import relativeTime from 'dayjs/plugin/relativeTime'

dayjs.locale('zh-cn')
dayjs.extend(relativeTime)

/** 空值/非法时间的统一占位符（不要显示 Invalid Date 或空字符串）。 */
export const EMPTY_TIME = '—'

/** 绝对时间：'YYYY-MM-DD HH:mm:ss'（本地时区）。 */
export function formatDateTime(iso?: string | null): string {
  if (!iso) return EMPTY_TIME
  const d = dayjs(iso)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm:ss') : EMPTY_TIME
}

/** 相对时间：'3 分钟前' / '2 天前'。 */
export function formatRelativeTime(iso?: string | null): string {
  if (!iso) return EMPTY_TIME
  const d = dayjs(iso)
  return d.isValid() ? d.fromNow() : EMPTY_TIME
}
