import { useMemo, memo } from "react";
import { Button, Space, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import { StatusTag } from "./StatusTag";
import { EmptyState } from "./EmptyState";
import { SeverityTag } from "./SeverityTag";
import { formatDateTime } from "../utils/time";

export interface Alert {
  id: string;
  host: string;
  message: string;
  severity: number;
  severity_name: string;
  status: "problem" | "acknowledged" | "resolved" | string;
  created_at: string;
  ack_time?: string;
  // 小改进 #2：误报标记
  is_false_positive?: boolean;
  marked_by?: string | null;
  marked_at?: string | null;
  false_positive_note?: string | null;
  // D-3：已关联的本系统工单 id。列表接口直出 models.Alert，该字段一直在响应里，
  // 此前只是没在前端声明。空 = 尚未建单（见 getAlertActions 的建单分支）。
  ticket_id?: string | null;
}

export interface AlertTableProps {
  data: Alert[];
  loading: boolean;
  onAck: (id: string) => Promise<void> | void;
  onResolve: (id: string) => Promise<void> | void;
  // 小改进 #2：标记/反标记误报
  onMarkFP?: (id: string, isFP: boolean) => void;
  // D-3：告警一键建单（undefined 时不渲染该按钮）
  onCreateTicket?: (id: string) => void;
  // C-P6: 批量勾选（undefined 时不开启）
  selectedIds?: string[];
  onSelectionChange?: (ids: string[]) => void;
  // M3/P5: 服务端分页受控。total 传入时启用受控分页（current/pageSize/onChange 由父组件持有）
  total?: number;
  page?: number;
  pageSize?: number;
  onPageChange?: (page: number, pageSize: number) => void;
}

export interface AlertActionHandlers {
  onAck: (id: string) => void;
  onResolve: (id: string) => void;
  onMarkFP?: (id: string, isFP: boolean) => void;
  onCreateTicket?: (id: string) => void;
}

export interface AlertAction {
  key: string;
  label: string;
  danger?: boolean;
  // 仅作指示的项（如「已建单」）disabled 且不带 onClick —— 让 onClick 可选比塞一个
  // 永不执行的空函数诚实。antd Button 本就接受 undefined。
  disabled?: boolean;
  onClick?: () => void;
}

// M13：抽「操作按钮决策」为纯函数，桌面端（AlertTable 操作列）与移动端（AlertCard）共用，
// 避免两处 status/is_false_positive 分支漂移（H9 教训：Assets 移动端与 AssetTable 桌面端渲染不一致导致 maintenance 标错）。
export function getAlertActions(
  record: Alert,
  { onAck, onResolve, onMarkFP, onCreateTicket }: AlertActionHandlers,
): AlertAction[] {
  const actions: AlertAction[] = [];
  if (record.status === "problem") {
    actions.push({ key: "ack", label: "确认", onClick: () => onAck(record.id) });
    actions.push({ key: "resolve", label: "解决", onClick: () => onResolve(record.id) });
  } else if (record.status === "acknowledged") {
    actions.push({ key: "resolve", label: "解决", onClick: () => onResolve(record.id) });
  }
  // D-3：一键建单。ticket_id 是列表查询时的快照，只用来**省一次请求**；
  // 防重由后端幂等兜底（已关联时返回既有票、created=false），前端判断不承担正确性。
  if (onCreateTicket) {
    if (record.ticket_id) {
      actions.push({ key: "ticket", label: "已建单", disabled: true });
    } else {
      actions.push({ key: "ticket", label: "建单", onClick: () => onCreateTicket(record.id) });
    }
  }
  if (onMarkFP) {
    if (!record.is_false_positive) {
      actions.push({ key: "mark-fp", label: "标记误报", onClick: () => onMarkFP(record.id, true) });
    } else {
      actions.push({ key: "unmark-fp", label: "取消误报", danger: true, onClick: () => onMarkFP(record.id, false) });
    }
  }
  return actions;
}

export const AlertTable = memo(function AlertTable({
  data,
  loading,
  onAck,
  onResolve,
  onMarkFP,
  onCreateTicket,
  selectedIds,
  onSelectionChange,
  total,
  page,
  pageSize,
  onPageChange,
}: AlertTableProps) {
  // P4：columns useMemo 缓存，render 闭包引用的 onAck/onResolve/onMarkFP 引用稳定则 columns 不重建
  const columns = useMemo<ColumnsType<Alert>>(() => [
    // M2：加前端本地排序。主机/状态按字符串，级别按 severity 数值，时间按 Date 解析
    // （RFC3339 字符串字典序会因时区偏移不同而错序，故不用 localeCompare）。
    { title: "主机", dataIndex: "host", key: "host", width: 150, sorter: (a, b) => a.host.localeCompare(b.host) },
    { title: "告警信息", dataIndex: "message", key: "message" },
    {
      title: "级别",
      dataIndex: "severity_name",
      key: "severity_name",
      width: 80,
      sorter: (a, b) => a.severity - b.severity,
      render: (name: string, record: Alert) => (
        <SeverityTag severity={record.severity} label={name} />
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      width: 80,
      sorter: (a, b) => a.status.localeCompare(b.status),
      render: (s: string) => <StatusTag value={s} />,
    },
    {
      title: "触发时间",
      dataIndex: "created_at",
      key: "created_at",
      width: 180,
      sorter: (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
      // W2：原样渲染 RFC3339（如 2026-09-09T02:09:00+08:00）既占宽又难读
      render: (iso: string) => formatDateTime(iso),
    },
    {
      title: "操作",
      key: "action",
      width: 340, // D-3 加了「建单」后 280 会挤到换行
      fixed: "right",
      // M13：操作按钮决策抽到 getAlertActions，与移动端 AlertCard 共用（避免分支漂移）
      render: (_, record) => (
        <Space>
          {getAlertActions(record, { onAck, onResolve, onMarkFP, onCreateTicket }).map((a) => (
            <Button
              key={a.key}
              type="link"
              size="small"
              danger={a.danger}
              disabled={a.disabled}
              onClick={a.onClick}
            >
              {a.label}
            </Button>
          ))}
        </Space>
      ),
    },
    // R-2：onCreateTicket 漏进依赖会让 columns 不随新回调重建 → React.memo 下按钮停在旧闭包
  ], [onAck, onResolve, onMarkFP, onCreateTicket]);

  return (
    <Table<Alert>
      rowKey="id"
      columns={columns}
      dataSource={data}
      loading={loading}
      scroll={{ x: 1000 }}
      pagination={
        total !== undefined
          ? {
              current: page ?? 1,
              pageSize: pageSize ?? 20,
              total,
              showSizeChanger: true,
              showTotal: (t) => `共 ${t} 条`,
              pageSizeOptions: ['10', '20', '50', '100'],
              onChange: (p, ps) => onPageChange?.(p, ps),
            }
          : {
              showSizeChanger: true,
              showTotal: (t) => `共 ${t} 条`,
              pageSizeOptions: ['10', '20', '50', '100'],
            }
      }
      locale={{
        emptyText: (
          <EmptyState
            title="暂无告警"
            description="系统当前运行平稳，没有需要处理的告警"
            compact
          />
        ),
      }}
      rowSelection={
        onSelectionChange
          ? {
              selectedRowKeys: selectedIds ?? [],
              onChange: (keys) => onSelectionChange(keys as string[]),
            }
          : undefined
      }
    />
  );
});

export default AlertTable;
