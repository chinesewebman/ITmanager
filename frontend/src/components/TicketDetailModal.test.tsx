// M50 G-UI-Tickets：工单详情弹窗的操作区。
//
// 这一层**故意不 mock services/api**（M25 的用例 mock 掉服务模块是那边的正确取舍，
// 但这里要验的恰是「请求发出去了没、失败了怎么表现」）：走真实 axios 实例、只换掉
// adapter —— 于是响应拦截器（services/api.ts:29-63）也真的跑，「错误只弹一次 toast」
// 这条断言才有内容。若 mock 掉服务模块，拦截器根本不会执行，那条断言就变成在验一个假对象。
import '@testing-library/jest-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import { message } from 'antd'
import api from '../services/api'
import { TicketDetailModal } from './TicketDetailModal'
import type { Ticket } from './TicketTable'

const TICKET: Ticket = {
  id: 't1',
  title: '服务器磁盘空间不足',
  description: '磁盘 90%',
  priority: 'high',
  status: 'open',
  requester: '张三',
  assignee: '李四',
  created_at: '2026-02-14T10:00:00',
  updated_at: '2026-02-14T11:00:00',
}

// 词表值按后端出站口径写（CanonicalRole 折叠后的 ops_user/ops_admin/admin/auditor）。
// 留一个遗留别名行（operator）在列表里当反例：它**不该**出现，因为下拉的判据是折叠后的值。
const USERS = [
  { id: 'u1', username: 'zhangsan', nickname: '张三', role: 'ops_user' },
  { id: 'u2', username: 'lisi', nickname: '李四', role: 'ops_admin' },
  { id: 'u3', username: 'admin', nickname: '管理员', role: 'admin' },
  { id: 'u4', username: 'auditor1', nickname: '审计员', role: 'auditor' },
  { id: 'u5', username: 'wangwu', nickname: '王五', role: 'operator' },
]

let puts: Record<string, unknown>[] = []
let failWrite = false

const adapter: AxiosAdapter = async (config: InternalAxiosRequestConfig) => {
  const url = config.url ?? ''
  const base = { status: 200, statusText: 'OK', headers: {}, config }
  if ((config.method ?? '').toLowerCase() === 'put') {
    puts.push(JSON.parse(String(config.data)) as Record<string, unknown>)
    if (failWrite) {
      // 形状照 axios 的失败契约（拦截器读 error.response.status / .data.message）
      throw Object.assign(new Error('boom'), {
        config,
        response: { status: 500, data: {}, statusText: 'ERR', headers: {}, config },
      })
    }
    return { ...base, data: { code: 0, data: {} } }
  }
  if (url === '/users') {
    return { ...base, data: { code: 0, data: { items: USERS, total: USERS.length } } }
  }
  // 弹窗里的 M25 经手时间线（本文件不验它的渲染，只要求它别把弹窗带崩）
  return { ...base, data: { code: 0, data: { items: [], total: 0 } } }
}

/** 按 App 的 QueryClient 口径建（main.tsx:11-19），只有重试次数在用例里收紧以保持确定性。 */
function renderModal(initialAction?: 'comment' | 'assign' | 'priority') {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false, staleTime: 30_000 } },
  })
  const invalidate = vi.spyOn(qc, 'invalidateQueries')
  const onClose = vi.fn()

  render(
    <QueryClientProvider client={qc}>
      <TicketDetailModal ticket={TICKET} onClose={onClose} initialAction={initialAction ?? null} />
    </QueryClientProvider>,
  )

  return { invalidate, onClose }
}

beforeEach(() => {
  puts = []
  failWrite = false
  api.defaults.adapter = adapter
  vi.mocked(message.error).mockClear()
  vi.mocked(message.success).mockClear()
})

describe('TicketDetailModal 操作区', () => {
  it('footer 渲染 5 个操作按钮，原有的 [关闭] 保持不动', () => {
    renderModal()

    // antd 会在「两个汉字」的按钮文案里插一个空格（关单 → 关 单），所以按名查要用
    // 容错的 regex，并且锚定首尾 —— 否则 /改\s*派/ 会把面板里的「确认改派」也算进去。
    for (const name of [/^\+ 评论$/, /^改\s*派$/, /^改优先级$/, /^关\s*单$/, /^已解决$/, /^关\s*闭$/]) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument()
    }
  })

  it('加评论：空文本不可提交；提交后把新评论追加进 description（工单上唯一的自由文本列）', async () => {
    renderModal()
    fireEvent.click(screen.getByRole('button', { name: '+ 评论' }))

    const submit = screen.getByRole('button', { name: '提交评论' })
    // intent 硬要求：前端挡住空提交
    expect(submit).toBeDisabled()

    fireEvent.change(screen.getByPlaceholderText('补充处理进展 / 交接说明…'), {
      target: { value: '已重启服务' },
    })
    expect(submit).toBeEnabled()
    fireEvent.click(submit)

    // 断的是**发给后端的载荷**：没有 POST /tickets/:id/comments 这个端点，
    // 评论只能作为 description 的新段落落库（见 TicketDetailModal.tsx 文件头 §1）。
    await waitFor(() => expect(puts).toEqual([{ description: '磁盘 90%\n\n已重启服务' }]))
  })

  it('关单：Popconfirm 二次确认前不发请求；确认后写 status=closed，invalidate 工单整棵树并收起弹窗', async () => {
    const { invalidate, onClose } = renderModal()
    fireEvent.click(screen.getByRole('button', { name: /^关\s*单$/ }))
    await screen.findByText('确认关闭这张工单？')
    // 二次确认的意义就在这一行：点一下按钮本身不能关单
    expect(puts).toHaveLength(0)

    fireEvent.click(screen.getByRole('button', { name: '确认关单' }))

    await waitFor(() => expect(puts).toEqual([{ status: 'closed' }]))
    // ['tickets'] 前缀覆盖列表 / 统计卡 / 经手历史
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['tickets'] })
    expect(onClose).toHaveBeenCalled()
  })

  it('写入失败：错误只由 axios 拦截器弹一次 toast，弹窗不关、不出现内嵌错误块', async () => {
    failWrite = true
    const { invalidate, onClose } = renderModal()

    fireEvent.click(screen.getByRole('button', { name: /^关\s*单$/ }))
    fireEvent.click(await screen.findByRole('button', { name: '确认关单' }))

    await waitFor(() => expect(vi.mocked(message.error)).toHaveBeenCalledTimes(1))
    // 文案来自拦截器对 500 的统一处理 —— 换成弹窗自己 toast 会变成两次
    expect(vi.mocked(message.error)).toHaveBeenCalledWith('服务器内部错误')
    expect(onClose).not.toHaveBeenCalled()
    expect(invalidate).not.toHaveBeenCalled()
    expect(screen.getByText('工单详情')).toBeInTheDocument()
    expect(document.querySelector('.ant-alert-error')).toBeNull()
  })

  it('改派：候选只含运维角色（ops_user / ops_admin），确认后写 assignee_name', async () => {
    renderModal('assign')

    fireEvent.mouseDown(screen.getByRole('combobox'))
    expect(await screen.findByTitle('张三')).toBeInTheDocument()
    expect(screen.getByTitle('李四')).toBeInTheDocument()
    // admin/auditor 没有派单价值，operator 是已被后端折叠掉的遗留别名
    expect(screen.queryByTitle('管理员')).toBeNull()
    expect(screen.queryByTitle('审计员')).toBeNull()
    expect(screen.queryByTitle('王五')).toBeNull()

    fireEvent.click(screen.getByTitle('张三'))
    fireEvent.click(screen.getByRole('button', { name: '确认改派' }))

    // 列名是 assignee_name：openapi 的 Ticket.assignee 是读侧名字，库里没有这一列
    await waitFor(() => expect(puts).toEqual([{ assignee_name: '张三' }]))
  })

  it('改优先级：只给契约词表四档，确认后写 priority', async () => {
    renderModal('priority')

    fireEvent.mouseDown(screen.getByRole('combobox'))
    for (const label of ['紧急', '高', '普通', '低']) {
      expect(await screen.findByTitle(label)).toBeInTheDocument()
    }

    fireEvent.click(screen.getByTitle('紧急'))
    fireEvent.click(screen.getByRole('button', { name: '确认修改' }))

    await waitFor(() => expect(puts).toEqual([{ priority: 'critical' }]))
  })
})
