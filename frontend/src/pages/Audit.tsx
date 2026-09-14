// M49 G-UI-Audit — 审计日志页（admin / ops_admin / auditor）。
//
// 为什么这张页存在：后端 `/api/audit-logs` 自 M22 就在（`audit_handler.go:26`），但前端
// 零入口 —— 管理员想知道「谁什么时候改了某资产/某工单/某用户」只能自己 curl。
//
// 契约（**读后端实体，不猜**）：
//   - 路由是 `/api/audit-logs`（`routes.go:286`，protected + canAudit），不是 /audit/logs。
//   - 过滤参数只有 4 个：user_id（精确 uuid）/ action（精确）/ method（精确）/ path（**前缀**）。
//     **没有** since/until / entity_type / entity_id —— 时间区间与对象过滤是后端不支持的，
//     前端不做「拿一页数据再本地过滤」的假过滤（那会漏掉未加载的行，比没有更危险）。
//   - 分页是 **cursor 式**（`limit` + 响应里的 `next_cursor`），**没有 total / page / page_size**。
//     故分页 UI 是上一页/下一页（cursor 栈），不是页码跳转。
//   - 审计行**不含请求体**：只记 method / path / status / error_msg。详情抽屉展示的是整行
//     JSON，不是「payload 的 diff」—— 不要把它渲染成「改了什么字段」的样子。
//
// 词表不硬编码：操作人 / action 的下拉项从**已加载的行**动态累积（后端没有枚举接口，
// action 列是自由字符串 `varchar(50)`）。method 例外 —— HTTP 方法集合是协议固定的。
import { useState } from 'react'
import { Button, Drawer, Input, Select, Space, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ReloadOutlined } from '@ant-design/icons'
import { auditApi } from '../services/api'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { PageHeader } from '../components/PageHeader'
import { queryKeys, useApiQuery } from '../hooks/useApiQuery'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { formatDateTime } from '../utils/time'
import type { AuditEvent, AuditListParams } from '../types'

/** 每页条数（后端的 limit 就是「本页最多几条」，没有 total 可供对齐）。 */
export const PAGE_SIZE = 30

/**
 * 词表采样条数：后端**没有** action/user 枚举接口（`action` 是自由字符串 `varchar(50)`），
 * 下拉项只能从数据里取。取最近这一批而不是「当前页」是必须的 —— 用当前页会让
 * 「筛到只剩一条」之后下拉里只剩那一个选项，用户再也切不回去。
 * 代价：更早出现、最近 VOCAB_LIMIT 条里没再出现的取值不在下拉里（仍可用路径前缀等条件定位）。
 */
const VOCAB_LIMIT = 100

/** HTTP 方法：协议固定集合，不是业务词表，可以硬编码。 */
const METHOD_OPTIONS = ['GET', 'POST', 'PUT', 'DELETE'].map((m) => ({ label: m, value: m }))

/**
 * 行级校验（外部数据边界）：id/action/created_at 是后端必写列，缺任何一个就说明这行不是
 * 审计行（或契约变了），宁可不渲染也不渲染半条。其余列在 UI 里都有缺省值。
 */
function isAuditEvent(v: unknown): v is AuditEvent {
  if (typeof v !== 'object' || v === null) return false
  if (!('id' in v && 'action' in v && 'created_at' in v)) return false
  return typeof v.id === 'string' && typeof v.action === 'string' && typeof v.created_at === 'string'
}

/** 后端响应信封 `{code, data:{items, next_cursor?}}`（openapi AuditLogList）。 */
type AuditListEnvelope = { data?: { data?: { items?: unknown; next_cursor?: unknown } } }

/** 解包 cursor 分页信封。items 逐条过 isAuditEvent。 */
async function fetchAudit(
  params: AuditListParams,
): Promise<{ items: AuditEvent[]; nextCursor?: string }> {
  // 边界断言：auditApi.list 的 axios 返回类型是全仓统一的 any 形状，这里一次收到已知契约上，
  // 真正的形状校验留给下面的运行时判据（items 逐条、next_cursor 判 typeof）。
  const res = (await auditApi.list(params)) as AuditListEnvelope
  const rawItems = res.data?.data?.items
  const rawCursor = res.data?.data?.next_cursor
  return {
    items: Array.isArray(rawItems) ? rawItems.filter(isAuditEvent) : [],
    nextCursor: typeof rawCursor === 'string' ? rawCursor : undefined,
  }
}

/**
 * 状态码配色：2xx 绿 / 4xx 橙 / 5xx 红 / 0（GORM 零值，未写入）灰。
 * 单独成函数是因为这条 4 档阈值是审计页的读图规则，内联进 JSX 会看不出边界含义。
 */
function statusColor(status?: number): string {
  if (!status) return 'default'
  if (status < 300) return 'green'
  return status < 500 ? 'orange' : 'red'
}

function Audit() {
  const [userId, setUserId] = useState<string>('')
  const [action, setAction] = useState<string>('')
  const [method, setMethod] = useState<string>('')
  const [pathPrefix, setPathPrefix] = useState<string>('')
  // cursor 栈：空 = 第一页；栈顶 = 当前页的 cursor。后端无 total，页码只能这样来。
  const [cursorStack, setCursorStack] = useState<string[]>([])
  const [detail, setDetail] = useState<AuditEvent | null>(null)

  useDocumentTitle('审计日志')

  const cursor = cursorStack.length ? cursorStack[cursorStack.length - 1] : undefined
  // 内联字面量（不用 useMemo）：与 Tickets/Alerts 同款——react-query 对 queryKey 做结构化
  // 哈希，值不变即命中同一缓存，引用稳定性由它保证。写成命名 interface 反而会因缺索引
  // 签名而无法传给 queryKeys 的 Record<string, unknown> 形参。
  const filters = {
    user_id: userId || undefined,
    action: action || undefined,
    method: method || undefined,
    path: pathPrefix || undefined,
    cursor,
    limit: PAGE_SIZE,
  }

  const { data, isLoading, isError, error, refetch } = useApiQuery(
    queryKeys.audit.list(filters),
    () => fetchAudit(filters),
  )

  // 词表（不加任何筛选，与服务端缓存 30s 共享一次请求）
  const { data: vocabulary } = useApiQuery(queryKeys.audit.vocabulary(), () =>
    fetchAudit({ limit: VOCAB_LIMIT }),
  )

  const items = data?.items ?? []
  const nextCursor = data?.nextCursor
  const hasFilter = Boolean(userId || action || method || pathPrefix)

  // 词表 ∪ 当前页：当前页可能出现词表采样窗口之外的新取值（翻得够深时），并上它才不会
  // 出现「明明看见了却选不了」。两者都是字符串，去重交给 Set。
  const vocabRows = vocabulary?.items ?? []
  const actionOptions = [...new Set([...vocabRows, ...items].map((e) => e.action))].sort()
  // 操作人：value 是 user_id（后端按 id 精确过滤），label 用行内的用户名快照。
  // 未认证行 user_id 为 null → 不成为可选项（无从过滤）。
  const actorIds = [...new Set([...vocabRows, ...items].map((e) => e.user_id ?? ''))].filter(
    (id) => id !== '',
  )
  const actorName = (id: string) =>
    [...vocabRows, ...items].find((e) => e.user_id === id)?.username || `${id.slice(0, 8)}…`

  /** 筛选/dropdown 是「另一批数据」：必须回第一页，否则拿着旧 cursor 会翻到半截。 */
  const resetToFirstPage = () => setCursorStack([])

  const columns: ColumnsType<AuditEvent> = [
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 180,
      // 后端固定 `ORDER BY created_at DESC, id DESC` —— 最新在前由**服务端**保证，
      // 因此不加前端 sorter（只能排当前 30 条，会让人以为看到了全局排序）。
      render: (v: string) => formatDateTime(v),
    },
    {
      title: '操作人',
      key: 'actor',
      width: 160,
      render: (_, r) => r.username || (r.user_id ? `${r.user_id.slice(0, 8)}…` : '匿名'),
    },
    {
      title: '动作',
      dataIndex: 'action',
      key: 'action',
      width: 160,
      render: (v: string) => (v ? <Tag color="blue">{v}</Tag> : '—'),
    },
    { title: '对象类型', dataIndex: 'resource', key: 'resource', width: 120 },
    {
      title: '对象 ID',
      dataIndex: 'resource_id',
      key: 'resource_id',
      width: 120,
      render: (v?: string | null) => (v ? <Typography.Text code>{v.slice(0, 8)}</Typography.Text> : '—'),
    },
    {
      title: '摘要',
      key: 'summary',
      render: (_, r) => (
        <Space size={4} wrap>
          <Tag>{r.method || '—'}</Tag>
          <Typography.Text style={{ fontSize: 12 }}>{r.path || '—'}</Typography.Text>
          <Tag color={statusColor(r.status)}>{r.status || '—'}</Tag>
          {r.error_msg ? (
            <Typography.Text type="danger" style={{ fontSize: 12 }}>
              {r.error_msg.length > 60 ? `${r.error_msg.slice(0, 60)}…` : r.error_msg}
            </Typography.Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: '操作',
      key: 'op',
      width: 80,
      render: (_, r) => (
        <Button type="link" size="small" onClick={() => setDetail(r)}>
          详情
        </Button>
      ),
    },
  ]

  return (
    <div>
      <PageHeader
        title="审计日志"
        subtitle="谁什么时候改了什么"
        extra={
          <Button icon={<ReloadOutlined />} onClick={() => refetch()}>
            刷新
          </Button>
        }
      />

      <div style={{ marginBottom: 16 }}>
        <Space wrap>
          <Select
            placeholder="操作人"
            allowClear
            value={userId || undefined}
            onChange={(v) => {
              setUserId(v ?? '')
              resetToFirstPage()
            }}
            style={{ width: 200 }}
            options={actorIds.map((id) => ({ label: actorName(id), value: id }))}
          />
          <Select
            placeholder="动作"
            allowClear
            value={action || undefined}
            onChange={(v) => {
              setAction(v ?? '')
              resetToFirstPage()
            }}
            style={{ width: 180 }}
            options={actionOptions.map((a) => ({ label: a, value: a }))}
          />
          <Select
            placeholder="方法"
            allowClear
            value={method || undefined}
            onChange={(v) => {
              setMethod(v ?? '')
              resetToFirstPage()
            }}
            style={{ width: 120 }}
            options={METHOD_OPTIONS}
          />
          <Input
            placeholder="路径前缀，如 /api/assets（回车）"
            allowClear
            // M56: 键盘流用户进页即能打字
            autoFocus
            defaultValue={pathPrefix}
            style={{ width: 260 }}
            onPressEnter={(e) => {
              setPathPrefix((e.target as HTMLInputElement).value.trim())
              resetToFirstPage()
            }}
          />
        </Space>
      </div>

      {isError ? (
        <ErrorState error={error} onRetry={refetch} />
      ) : (
        <>
          <Table
            rowKey="id"
            columns={columns}
            dataSource={items}
            loading={isLoading}
            pagination={false}
            size="small"
            locale={{
              emptyText: hasFilter ? (
                <EmptyState preset="no-search-result" title="暂无匹配" compact />
              ) : (
                <EmptyState title="暂无审计事件" description="还没有可展示的操作留痕" compact />
              ),
            }}
          />
          <div
            style={{
              display: 'flex',
              justifyContent: 'flex-end',
              alignItems: 'center',
              gap: 8,
              marginTop: 12,
            }}
          >
            <Typography.Text type="secondary">
              第 {cursorStack.length + 1} 页 · 本页 {items.length} 条
            </Typography.Text>
            <Button
              disabled={cursorStack.length === 0}
              onClick={() => setCursorStack((s) => s.slice(0, -1))}
            >
              上一页
            </Button>
            <Button
              // 没有 next_cursor 即到底（不是「本页不满」）——契约如此，不要用它算总页数
              disabled={!nextCursor}
              onClick={() => nextCursor && setCursorStack((s) => [...s, nextCursor])}
            >
              下一页
            </Button>
          </div>
        </>
      )}

      <Drawer
        title="审计事件详情"
        open={Boolean(detail)}
        onClose={() => setDetail(null)}
        width={640}
      >
        {/* 审计行不含请求体：这里就是「完整的一行」，管理员看的就是这些字段 */}
        <pre
          data-testid="audit-event-json"
          style={{
            margin: 0,
            fontSize: 12,
            lineHeight: 1.6,
            background: 'var(--ant-color-fill-quaternary)',
            padding: 12,
            borderRadius: 6,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-all',
          }}
        >
          {detail ? JSON.stringify(detail, null, 2) : ''}
        </pre>
      </Drawer>
    </div>
  )
}

export default Audit
