// M61 G-User-AdminManagement — 用户管理页（仅 admin，identity 能力）。
//
// 为什么这张页存在：后端 `/api/users` 此前**只有 GET**（`routes.go` 的 users 组），
// admin 想让离职员工登不进来、想把某人提成 ops_admin，只能自己进库 UPDATE ——
// 无校验、无审计、无人知道谁改过。M61 补齐了写端点，这张页是它们的门面。
//
// 契约（**读后端实现，不猜**；openapi.yaml 已同步声明）：
//   - 列表 `GET /users?page&page_size` → `{code, data:{items, total, page, page_size}}`
//     （**不是**裸数组：openapi 的 UserList 在 M61 一并改了，见 CHANGELOG）
//   - 状态 `PATCH /users/:id/status` body `{status}`，可写值**只有** active / inactive。
//     没有 `locked` —— 鉴权侧（JWT / API Key / 登录）只拦 inactive，写 locked 不拦任何
//     请求，给管理员一个静默无效的开关比不给更糟。
//   - 角色 `PATCH /users/:id/role` body `{role}`，词表 5 值（遗留别名 operator/viewer
//     由服务端折叠，前端只提供词表值）。
//   - 三条写端点都是**严格请求体**：未知键 400。所以这里逐字段显式传，绝不透传整行。
//   - 403 的两种情形不是「参数写错」而是策略：自我禁用 / 降级最后一名可登录管理员。
//     页面据 403 回滚并原样显示服务端原因（那是最能说清原因的一手信息）。
//
// **不做删除按钮**：账号走 status=inactive，不硬删 —— 审计要求保留操作历史
// （audit_logs.user_id 取值来自 users，硬删后历史里的操作人再也查不到是谁）。
// PII 脱敏是另一件事（TODO 已登记）。
import { useEffect, useMemo, useState } from 'react'
import { Button, Popconfirm, Select, Space, Switch, Table, Tag, Tooltip, Typography, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { KeyOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQueryClient } from '@tanstack/react-query'
import {
  ASSIGNABLE_ROLES,
  userApi,
  type AssignableRole,
  type UserStatus,
} from '../services/api'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { PageHeader } from '../components/PageHeader'
import { queryKeys, useApiMutation, useApiQuery } from '../hooks/useApiQuery'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { formatDateTime } from '../utils/time'

/** 每页条数（后端 `page_size`，service 层夹在 1..500）。 */
export const PAGE_SIZE = 20

/** 行模型：列表渲染真正用到的列。`status`/`role` 是字符串而非联合 —— 库列是裸
 * VARCHAR，存量行可能带词表外的值（如 locked / operator），渲染层要有兜底。 */
export interface UserRow {
  id: string
  username: string
  nickname?: string
  email?: string
  role: string
  status: string
  last_login?: string | null
}

/** 角色 → 中文名。**只用于展示**，判权一律走后端下发的 capabilities；缺省回落原值
 * （词表外/新增角色时显示原字符串，好过显示空白）。 */
const ROLE_LABEL: Record<string, string> = {
  admin: '超级管理员',
  ops_admin: '运维管理员',
  ops_user: '运维人员',
  auditor: '审计员',
  readonly: '只读用户',
  user: '只读用户',
}

const ROLE_OPTIONS: { value: string; label: string }[] = ASSIGNABLE_ROLES.map((r) => ({
  value: r,
  label: `${ROLE_LABEL[r]} (${r})`,
}))

/**
 * 某一行的角色下拉项。存量库里可能出现词表外的值（设计期别名 `operator`、迁移前写入的
 * 任意串）—— 那种值必须**出现在选项里且禁用**，否则 antd 只显示原串而用户看不出
 * 「这不是可选项」；服务端出站虽已折叠，前端不做假设（行级校验只保证它是字符串）。
 */
function roleOptionsFor(current: string): { value: string; label: string; disabled?: boolean }[] {
  if (ASSIGNABLE_ROLES.includes(current as AssignableRole)) return ROLE_OPTIONS
  return [{ value: current, label: `${ROLE_LABEL[current] ?? current} (${current})`, disabled: true }, ...ROLE_OPTIONS]
}

/**
 * 行级校验（外部数据边界）：id/username/role/status 是后端必写列，缺任何一个就说明
 * 这行不是用户行（或契约变了）。宁可不渲染，也不要渲染出一个「点不动」的空行 ——
 * 那种行点 Switch 会把 undefined 当状态发给后端。
 */
function isUserRow(v: unknown): v is UserRow {
  if (typeof v !== 'object' || v === null) return false
  const o = v as Record<string, unknown>
  return (
    typeof o.id === 'string' &&
    typeof o.username === 'string' &&
    typeof o.role === 'string' &&
    typeof o.status === 'string'
  )
}

/** 后端信封 `{data:{data:{items,total}}}`（openapi UserList）。 */
type UserListEnvelope = { data?: { data?: { items?: unknown; total?: unknown } } }

async function fetchUsers(params: { page: number; page_size: number }) {
  const res = (await userApi.list(params)) as UserListEnvelope
  const raw = res.data?.data?.items
  const total = res.data?.data?.total
  return {
    items: Array.isArray(raw) ? raw.filter(isUserRow) : [],
    total: typeof total === 'number' ? total : 0,
  }
}

/** 行上待生效的乐观改动（按字段记，避免「改状态」把「刚改的角色」一起抹掉）。 */
type RowPatch = { status?: UserStatus; role?: AssignableRole }

/** 从 mutation 变量里取出「这次改动前的样子」，失败时按字段精确回滚。 */
type PatchVars = { id: string; prev?: RowPatch }

/** 服务端错误里最接近「为什么」的那句话（拦截器已 toast 通用文案，这里补具体原因）。 */
function reasonOf(e: unknown, fallback: string): string {
  const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message
  return typeof msg === 'string' && msg ? msg : fallback
}

function Users() {
  const [page, setPage] = useState(1)
  // 按 user id 记乐观值：只在「请求在飞 / 刚落库但列表还没重取」这段窗口需要它。
  const [overrides, setOverrides] = useState<Record<string, RowPatch>>({})
  // 待确认的改动（Popconfirm 的受控 open 由它驱动）：确认前不动任何状态，取消即丢弃。
  const [pending, setPending] = useState<
    { id: string; kind: 'status'; next: UserStatus } | { id: string; kind: 'role'; next: AssignableRole } | null
  >(null)

  useDocumentTitle('用户管理')
  const qc = useQueryClient()

  const { data, isLoading, isError, error, refetch, dataUpdatedAt } = useApiQuery(
    queryKeys.users.list({ page, page_size: PAGE_SIZE }),
    () => fetchUsers({ page, page_size: PAGE_SIZE }),
  )

  // 列表数据刷新到位即清乐观值。**必须清**：留着会盖住外部（另一个 admin）的改动 ——
  // 本页的乐观值只是「我这次请求的结果」，服务端才是真相。
  useEffect(() => {
    setOverrides({})
  }, [dataUpdatedAt])

  const total = data?.total ?? 0

  // 渲染用行 = 服务端数据 + 乐观覆盖（只剩改变过的字段）。
  // `data?.items ?? []` 写在 memo 内：写成外面的 `const items = … ?? []` 会让
  // 依赖数组每次渲染都变（`??` 每次都造新数组）→ memo 永不命中，顺着往下
  // 让整张表的 columns 也跟着重建（P4 memo 化的收益全丢）。
  const rows = useMemo(
    () => (data?.items ?? []).map((u) => ({ ...u, ...overrides[u.id] })),
    [data, overrides],
  )

  const applyPatch = (vars: PatchVars & { patch: RowPatch }) =>
    setOverrides((prev) => ({ ...prev, [vars.id]: { ...prev[vars.id], ...vars.patch } }))

  /** 失败回滚：恢复**这次改动前**的字段值（不是无脑清空 —— 行上可能还有上一笔已成功
   * 但列表尚未重取的改动）。 */
  const rollback = (vars: PatchVars) =>
    setOverrides((prev) => {
      const next = { ...prev }
      const restored: RowPatch = { ...vars.prev }
      if (restored.status === undefined && restored.role === undefined) delete next[vars.id]
      else next[vars.id] = restored
      return next
    })

  const statusMut = useApiMutation(
    (vars: PatchVars & { next: UserStatus }) => userApi.updateStatus(vars.id, vars.next),
    {
      onSuccess: (res, vars) => {
        // 用**服务端回执**校正乐观值（词表归一/未来加派生列时，乐观值与真值可能不同）
        const got = (res as { data?: { data?: { status?: unknown } } })?.data?.data?.status
        applyPatch({ id: vars.id, patch: { status: (typeof got === 'string' ? got : vars.next) as UserStatus } })
        message.success('状态已更新')
        void qc.invalidateQueries({ queryKey: queryKeys.users.all })
      },
      onError: (e, vars) => {
        rollback(vars)
        message.error(reasonOf(e, '状态更新失败'))
      },
    },
  )

  const roleMut = useApiMutation(
    (vars: PatchVars & { next: AssignableRole }) => userApi.updateRole(vars.id, vars.next),
    {
      onSuccess: (res, vars) => {
        const got = (res as { data?: { data?: { role?: unknown } } })?.data?.data?.role
        applyPatch({ id: vars.id, patch: { role: (typeof got === 'string' ? got : vars.next) as AssignableRole } })
        message.success('角色已更新')
        void qc.invalidateQueries({ queryKey: queryKeys.users.all })
      },
      onError: (e, vars) => {
        rollback(vars)
        message.error(reasonOf(e, '角色更新失败'))
      },
    },
  )

  // 「强制改密」走 PUT /users/:id 的第三个字段（**不是** admin 设新密码：全仓没有
  // admin 重置他人密码的端点，按一个做不到的名字做按钮就是在骗运维 —— B1-1 的死表单
  // 教训）。置位后该账号下次登录会带 must_change_password=true，前端强制跳改密页。
  const forceChangeMut = useApiMutation(
    (vars: PatchVars) => userApi.update(vars.id, { must_change_password: true }),
    {
      onSuccess: () => {
        message.success('已置为下次登录必须改密')
        void qc.invalidateQueries({ queryKey: queryKeys.users.all })
      },
      onError: (e) => message.error(reasonOf(e, '操作失败')),
    },
  )

  const confirmPending = () => {
    const p = pending
    setPending(null)
    if (!p) return
    const prev = overrides[p.id]
    if (p.kind === 'status') {
      applyPatch({ id: p.id, patch: { status: p.next } }) // 乐观：先翻，失败再回滚
      statusMut.mutate({ id: p.id, next: p.next, prev })
      return
    }
    applyPatch({ id: p.id, patch: { role: p.next } })
    roleMut.mutate({ id: p.id, next: p.next, prev })
  }

  const columns: ColumnsType<UserRow> = useMemo(
    () => [
      {
        title: '用户名',
        dataIndex: 'username',
        render: (v: string, row) => (
          <Space direction="vertical" size={0}>
            <Typography.Text strong>{v}</Typography.Text>
            {row.nickname ? (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {row.nickname}
              </Typography.Text>
            ) : null}
          </Space>
        ),
      },
      {
        title: '邮箱',
        dataIndex: 'email',
        render: (v: string) => v || '—',
      },
      {
        title: '角色',
        dataIndex: 'role',
        render: (v: string, row) => (
          <Popconfirm
            // 受控 open：选中新角色**先确认**再落库。不确认就不发请求、不改任何状态。
            open={pending?.id === row.id && pending.kind === 'role'}
            title="修改角色"
            description={
              <>
                把 <b>{row.username}</b> 的角色改为「{ROLE_LABEL[pending?.kind === 'role' ? pending.next : v] ?? v}」？
                <br />
                改完立即生效（对方无需重新登录即可获得/失去能力）。
              </>
            }
            okText="确认"
            cancelText="取消"
            onConfirm={confirmPending}
            onCancel={() => setPending(null)}
            onOpenChange={(open) => {
              if (!open) setPending(null)
            }}
          >
            <Select
              size="small"
              style={{ minWidth: 168 }}
              value={v}
              data-testid={`user-role-${row.id}`}
              options={roleOptionsFor(v)}
              onChange={(next: string) => {
                // 只把词表内的目标值送入确认流程：禁用项点不动，但仍做一次判据
                //（下拉项随行数据变化，不能只靠 disabled 兜住）。
                if (next === v || !ASSIGNABLE_ROLES.includes(next as AssignableRole)) return
                setPending({ id: row.id, kind: 'role', next: next as AssignableRole })
              }}
            />
          </Popconfirm>
        ),
      },
      {
        title: '状态',
        dataIndex: 'status',
        render: (v: string, row) => {
          const active = v === 'active'
          const disabled = v !== 'active' && v !== 'inactive' // locked 等存量值：只展示
          return (
            <Space>
              <Popconfirm
                open={pending?.id === row.id && pending.kind === 'status'}
                title={active ? '禁用账号' : '启用账号'}
                description={
                  active ? (
                    <>
                      禁用 <b>{row.username}</b>？该账号的登录会话与 API Key 立即失效
                      （最长 30s 生效）。
                    </>
                  ) : (
                    <>
                      启用 <b>{row.username}</b>？该账号可立即重新登录。
                    </>
                  )
                }
                okText="确认"
                okButtonProps={active ? { danger: true } : undefined}
                cancelText="取消"
                onConfirm={confirmPending}
                onCancel={() => setPending(null)}
                onOpenChange={(open) => {
                  if (!open) setPending(null)
                }}
              >
                <Switch
                  size="small"
                  checked={active}
                  disabled={disabled}
                  data-testid={`user-status-${row.id}`}
                  // Switch 受控（checked 来自行数据），这里只负责开确认框：
                  // 真正翻动发生在 confirmPending，失败则回滚。
                  onChange={() => setPending({ id: row.id, kind: 'status', next: active ? 'inactive' : 'active' })}
                />
              </Popconfirm>
              {/* locked 等词表外存量值：显示原文而不是假装它是 active/inactive */}
              {disabled ? <Tag color="orange">{v}</Tag> : <Tag color={active ? 'green' : 'default'}>{active ? '启用' : '禁用'}</Tag>}
            </Space>
          )
        },
      },
      {
        title: '最后登录',
        dataIndex: 'last_login',
        render: (v?: string | null) => (v ? formatDateTime(v) : '从未登录'),
      },
      {
        title: '操作',
        key: 'actions',
        render: (_, row) => (
          <Tooltip title="置 must_change_password=true：该账号下次登录必须改密（不是由管理员设新密码）">
            <Popconfirm
              title="强制下次登录改密"
              description={<>对 <b>{row.username}</b> 置强制改密标志？</>}
              okText="确认"
              cancelText="取消"
              onConfirm={() => forceChangeMut.mutate({ id: row.id })}
            >
              <Button size="small" icon={<KeyOutlined />} data-testid={`user-force-change-${row.id}`}>
                强制改密
              </Button>
            </Popconfirm>
          </Tooltip>
        ),
      },
    ],
    // columns 依赖的只有这些：pending 决定 Popconfirm 的开合，overrides 决定乐观值，
    // 三个 mutate 引用稳定（React Query 的 mutate 保证）。
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [pending, overrides, statusMut.mutate, roleMut.mutate, forceChangeMut.mutate],
  )

  return (
    <div>
      <PageHeader
        title="用户管理"
        subtitle="账号处置：启用/禁用、角色调整、强制改密。禁用后登录会话与 API Key 立即失效。"
        extra={
          <Button icon={<ReloadOutlined />} onClick={() => refetch()} data-testid="users-refresh">
            刷新
          </Button>
        }
      />

      {/* 创建账号：**本 round 不提供**。后端没有 POST /users（账号由 admin-bootstrap /
          seed 建），做一个点了没反应的按钮正是 B1-1 那类缺陷。需求来了再连端点。 */}

      {isError ? (
        <ErrorState title="用户列表加载失败" error={error} onRetry={() => refetch()} />
      ) : (
        <Table<UserRow>
          rowKey="id"
          size="small"
          loading={isLoading}
          dataSource={rows}
          columns={columns}
          locale={{
            emptyText: <EmptyState title="没有用户" description="账号由 admin-bootstrap 或 seed 命令创建。" />,
          }}
          pagination={{
            current: page,
            pageSize: PAGE_SIZE,
            total,
            showSizeChanger: false,
            onChange: setPage,
          }}
        />
      )}
    </div>
  )
}

export default Users
