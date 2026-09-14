// M50 G-UI-Tickets：工单详情弹窗从「只读」变成「可操作」。
//
// 后端只有**一个**写入口：`PUT /api/tickets/:id`（routes.go:360，canWrite 能力），
// 请求体是「列名 → 值」的裸 map（ticket_handler.go:158-185 → service.Update）。
// 由此派生本文件的几条硬约束，写错任何一条都是 400/500 而不是「没效果」：
//
//  1. **没有 `POST /tickets/:id/comments` 这个端点**（全仓无 comment handler，
//     openapi.yaml 也无该 path —— M50 的 intent 假设它存在，实测不存在）。
//     「加评论」于是落到工单上唯一可写的自由文本列 `description`：提交 = 把新评论
//     追加成新段落再 PUT。副作用是这次改动会以「描述」的字段变更进 M25 经手历史，
//     正好是「提交后刷新经手历史」想要的效果。已知代价见 M50-completion-report.md §Risk R-1
//     （并发追加后写的覆盖先写的）。
//  2. **「改派」写 `assignee_name`**，不写 `assignee`（openapi 的 Ticket.assignee
//     是读侧字段名，库里没这一列，写了 gorm 会拼出不存在的列 → 500），也不写
//     `assignee_id`（该列前后端零消费方，写它只会多出一行裸 UUID 的经手历史）。
//  3. priority / status 的取值必须落在 service 的契约词表内（ticket_service.go:455-456）。
//     词表外的值**不是被改写而是 400**，所以这里只给词表内的选项，且不提供自由输入。
//
// mutation 形态：五种操作共用**一条** mutation（同一个后端端点、同一套后处理），
// 变量是「要写的列 + 成功文案」。拆成五条只会把 invalidate/关弹窗写五遍。
// 错误不在这里处理 —— axios 响应拦截器（services/api.ts:29-63）已经统一 toast，
// 这里再加一层就是同一句话弹两遍。弹窗内也不放 error alert（intent 明文禁止）。
import { useEffect, useState } from 'react'
import { Button, Col, Input, Modal, Popconfirm, Row, Select, message } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { StatusTag } from './StatusTag'
import { formatDateTime } from '../utils/time'
import type { Ticket } from './TicketTable'
import { TicketHistoryTimeline } from './TicketHistoryTimeline'
import { ticketApi, userApi } from '../services/api'
import { useApiMutation, useApiQuery, queryKeys } from '../hooks/useApiQuery'

const { TextArea } = Input

const PRIORITY_LABEL: Record<string, string> = {
  critical: '紧急',
  high: '高',
  normal: '普通',
  medium: '普通', // M16 遗留同义词安全网（写入方已归一为 normal），见 TicketTable.tsx 同名字典注释
  low: '低',
}

const STATUS_LABEL: Record<string, string> = {
  open: '新建',
  in_progress: '处理中',
  pending: '等待中',
  resolved: '已解决',
  closed: '关闭',
}

/** 优先级下拉项。取值＝service 的契约词表（ticket_service.go:455）的四个值，无自由输入。 */
const PRIORITY_OPTIONS = [
  { value: 'critical', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'normal', label: '普通' },
  { value: 'low', label: '低' },
]

/**
 * 可派单的角色（＝后端 capRoles[CapWrite] 里的**运维**两个角色）。
 *
 * 写成 `role === 'operator'` 会一个都选不中：`operator` 是设计期遗留别名，
 * 后端 ListUsers 出站前已用 CanonicalRole 折叠成 `ops_user`（user_handler.go:34-37、
 * middleware/roles.go:67-71）。列表里看到的一律是折叠后的词表值。
 *
 * 为什么排掉 admin：admin 是平台管理员账号，不进运维排班派单的候选池；
 * 排掉 auditor/readonly/user：它们**没有写能力**（capRoles[CapWrite]），
 * 派给它们等于把工单扔进一个改不动的账号。
 */
const ASSIGNEE_ROLES: readonly string[] = ['ops_user', 'ops_admin']

/** 弹窗内可展开的操作面板。关单/已解决没有面板 —— 它们走 Popconfirm 二次确认。 */
export type TicketPanel = 'comment' | 'assign' | 'priority'

export interface TicketDetailModalProps {
  ticket: Ticket | null
  onClose: () => void
  /**
   * 从 TicketTable 的 [更多操作] 进来时预设展开的面板。
   * 关单/已解决不预设面板：它们的入口是 footer 上带 Popconfirm 的按钮，
   * 表格行内先打开票面再确认，避免「行内一次点击即关单」。
   */
  initialAction?: TicketPanel | null
}

/**
 * 把一条新评论追加到工单描述末尾（description 是工单上唯一的自由文本列，见文件头）。
 *
 * 不做时间戳：这次 PUT 会进 M25 经手历史，谁在什么时候改的由历史行记（actor_name
 * + created_at），在文本里再插一个客户端时间只会多一份可能与服务端不一致的副本。
 */
function appendComment(description: string | undefined, comment: string): string {
  const prev = (description ?? '').trim()
  return prev ? `${prev}\n\n${comment}` : comment
}

export function TicketDetailModal({ ticket, onClose, initialAction }: TicketDetailModalProps) {
  const queryClient = useQueryClient()
  const [panel, setPanel] = useState<TicketPanel | null>(initialAction ?? null)
  const [commentText, setCommentText] = useState('')
  const [assigneeDraft, setAssigneeDraft] = useState<string | undefined>(undefined)
  const [priorityDraft, setPriorityDraft] = useState<string | undefined>(undefined)

  // destroyOnClose 只在「关闭」时销毁子树；父组件直接换一张票（或从行内下拉带
  // initialAction 进来）时弹窗本身不重挂，上一张票的草稿会跟着过来 —— 这里显式重置。
  useEffect(() => {
    setPanel(initialAction ?? null)
    setCommentText('')
    setAssigneeDraft(undefined)
    setPriorityDraft(undefined)
  }, [ticket?.id, initialAction])

  // 人员列表**懒加载**：只有真的展开「改派」面板才发请求。
  // 除了省一次请求，还有个硬理由 —— /users 挂在 canIdentity（仅 admin）下
  // （routes.go:363-367），非 admin 打开弹窗就预取会平白弹一个 403 toast。
  const {
    data: usersData,
    isLoading: usersLoading,
    isError: usersIsError,
    refetch: usersRefetch,
  } = useApiQuery(
    ['users', 'list'],
    () => userApi.list({ page_size: 500 }),
    { enabled: panel === 'assign' },
  )

  // 逐级可选：钩子拿到的是「没见过的形状」时也只是一份空候选表，不该把整页带崩
  // （同 Tickets.tsx fetchTickets 的 res?.data?.data 口径）。
  const userItems = usersData?.data?.data?.items
  const operatorOptions = (Array.isArray(userItems) ? userItems : []).flatMap((u) => {
    if (u.role == null || !ASSIGNEE_ROLES.includes(u.role)) return []
    const name = u.nickname || u.username
    return name ? [{ value: name, label: name }] : []
  })

  const saveMut = useApiMutation(
    (v: { patch: Record<string, unknown>; ok: string }) => ticketApi.update(ticket?.id ?? '', v.patch),
    {
      onSuccess: (_res, v) => {
        message.success(v.ok)
        // 一处 invalidate 覆盖列表 / 统计卡 / 经手历史（同一个 ['tickets'] 前缀）。
        queryClient.invalidateQueries({ queryKey: queryKeys.tickets.all })
        onClose()
      },
    },
  )

  const submit = (patch: Record<string, unknown>, ok: string) => saveMut.mutate({ patch, ok })

  return (
    <Modal
      title="工单详情"
      open={!!ticket}
      onCancel={onClose}
      footer={[
        <Button key="comment" onClick={() => setPanel(panel === 'comment' ? null : 'comment')}>
          + 评论
        </Button>,
        <Button key="assign" onClick={() => setPanel(panel === 'assign' ? null : 'assign')}>
          改派
        </Button>,
        <Button key="priority" onClick={() => setPanel(panel === 'priority' ? null : 'priority')}>
          改优先级
        </Button>,
        <Popconfirm
          key="close"
          title="确认关闭这张工单？"
          description="关闭后可在详情里重新打开。"
          okText="确认关单"
          cancelText="取消"
          onConfirm={() => submit({ status: 'closed' }, '工单已关闭')}
        >
          <Button danger>关单</Button>
        </Popconfirm>,
        <Popconfirm
          key="resolve"
          title="确认把这张工单标记为已解决？"
          okText="确认解决"
          cancelText="取消"
          onConfirm={() => submit({ status: 'resolved' }, '工单已解决')}
        >
          <Button>已解决</Button>
        </Popconfirm>,
        <Button key="close-modal" onClick={onClose}>
          关闭
        </Button>,
      ]}
      width={600}
      destroyOnClose
    >
      {ticket && (
        <>
          <Row gutter={16} style={{ marginBottom: 16 }}>
            <Col span={12}>
              <strong>工单标题：</strong>
              {ticket.title}
            </Col>
            <Col span={12}>
              <strong>工单ID：</strong>
              {ticket.id}
            </Col>
          </Row>
          <Row gutter={16} style={{ marginBottom: 16 }}>
            <Col span={12}>
              <strong>优先级：</strong>
              <StatusTag value={ticket.priority} label={PRIORITY_LABEL[ticket.priority] || ticket.priority} />
            </Col>
            <Col span={12}>
              <strong>状态：</strong>
              <StatusTag value={ticket.status} label={STATUS_LABEL[ticket.status] || ticket.status} />
            </Col>
          </Row>
          <Row gutter={16} style={{ marginBottom: 16 }}>
            <Col span={12}>
              <strong>请求人：</strong>
              {ticket.requester}
            </Col>
            <Col span={12}>
              <strong>处理人：</strong>
              {ticket.assignee || '-'}
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <strong>创建时间：</strong>
              {/* W2：与列表页统一走 utils/time 出口 */}
              {formatDateTime(ticket.created_at)}
            </Col>
            <Col span={12}>
              <strong>更新时间：</strong>
              {formatDateTime(ticket.updated_at)}
            </Col>
          </Row>

          {panel === 'comment' && (
            <div style={{ marginTop: 12 }}>
              <TextArea
                rows={3}
                maxLength={2000}
                value={commentText}
                onChange={(e) => setCommentText(e.target.value)}
                placeholder="补充处理进展 / 交接说明…"
              />
              <Button
                type="primary"
                size="small"
                style={{ marginTop: 8 }}
                loading={saveMut.isPending}
                // 空提交在**前端**挡住（intent 硬要求）：提交按钮在无内容时不可点。
                disabled={!commentText.trim()}
                onClick={() => {
                  const text = commentText.trim()
                  submit({ description: appendComment(ticket.description, text) }, '评论已提交')
                }}
              >
                提交评论
              </Button>
            </div>
          )}

          {panel === 'assign' && (
            <div style={{ marginTop: 12 }}>
              {usersIsError && (
                <div>
                  <span>人员列表加载失败</span>
                  <Button type="link" size="small" onClick={() => usersRefetch()}>
                    重试
                  </Button>
                </div>
              )}
              <Select
                style={{ width: 260 }}
                showSearch
                optionFilterProp="label"
                loading={usersLoading}
                placeholder={usersLoading ? '加载人员…' : '选择处理人'}
                options={operatorOptions}
                value={assigneeDraft}
                onChange={(v: string) => setAssigneeDraft(v)}
              />
              <Button
                type="primary"
                size="small"
                style={{ marginLeft: 8 }}
                loading={saveMut.isPending}
                disabled={!assigneeDraft}
                onClick={() => submit({ assignee_name: assigneeDraft }, '已改派')}
              >
                确认改派
              </Button>
            </div>
          )}

          {panel === 'priority' && (
            <div style={{ marginTop: 12 }}>
              <Select
                style={{ width: 160 }}
                placeholder="选择优先级"
                options={PRIORITY_OPTIONS}
                value={priorityDraft}
                onChange={(v: string) => setPriorityDraft(v)}
              />
              <Button
                type="primary"
                size="small"
                style={{ marginLeft: 8 }}
                loading={saveMut.isPending}
                disabled={!priorityDraft}
                onClick={() => submit({ priority: priorityDraft }, '优先级已更新')}
              >
                确认修改
              </Button>
            </div>
          )}

          {/* M25 经手记录。key=ticket.id：换一张票就重挂，省得把上一张的行带过来
              （弹窗 destroyOnClose 只在关闭时销毁，父组件直接换 ticket 时不会重挂）。 */}
          <div style={{ marginTop: 8 }}>
            <strong>经手记录</strong>
            <div style={{ marginTop: 8 }}>
              <TicketHistoryTimeline key={ticket.id} ticketId={ticket.id} />
            </div>
          </div>
        </>
      )}
    </Modal>
  )
}

export default TicketDetailModal
