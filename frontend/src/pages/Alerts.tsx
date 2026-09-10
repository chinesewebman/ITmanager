import { Button, Modal, Popconfirm, Progress, Select, Space, theme, message } from "antd";
import {
  SyncOutlined,
  CheckOutlined,
  CheckCircleOutlined,
  DownloadOutlined,
} from "@ant-design/icons";
import { alertApi } from "../services/api";
import type { AlertListParams } from "../services/apiClient";
import { PageHeader } from "../components/PageHeader";
import { ErrorState } from "../components/ErrorState";
import { AlertTable, type Alert } from "../components/AlertTable";
import { AlertCard } from "../components/AlertCard";
import {
  AlertStatsCards,
  type AlertStats,
} from "../components/AlertStatsCards";
import { useApiMutation, useApiQuery, queryKeys } from "../hooks/useApiQuery";
import { useState, useCallback } from "react";
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useResponsiveTable, MobileCardList } from '../hooks/useResponsiveTable'

// W1：假数据兜底已删除。统计卡的 0 值只是「无数据时占位」，不是虚构数字。
const EMPTY_STATS: AlertStats = {
  total: 0,
  problem: 0,
  acknowledged: 0,
  resolved: 0,
};

interface AlertsResp {
  items: Alert[];
  stats: AlertStats | null;
  total: number;
}

function Alerts() {
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [severityFilter, setSeverityFilter] = useState<string>("");
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  // M3/P5：服务端分页——page/pageSize 由本组件持有；筛选变化时重置回第 1 页
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  // C-P9: 列表 + stats 合并到 React Query，filter 变化走 queryKey 隔离缓存
  const filters = { status: statusFilter, severity: severityFilter };

  useDocumentTitle('告警中心')
  const { token } = theme.useToken()
  // M13：移动端 (xs) 用卡片列表替代表格，避免 AlertTable 横向溢出（桌面端不变）
  const { isMobile } = useResponsiveTable()
  // W1：删掉 queryFn 内的 `?? MOCK_ALERTS / ?? DEFAULT_STATS` 兜底 ——
  // 原写法让 React Query 的 isError 恒为 false，失败被渲染成一屏假告警。
  const { data, isLoading, isError, error, refetch } = useApiQuery<AlertsResp>(
    queryKeys.alerts.list({ ...filters, page, pageSize }),
    async () => {
      // antd Select onChange 给 string，但 spec 要求 literal union
      // 调用方负责 narrow（业务已知：只有 3 个合法值）
      const params = {
        page,
        page_size: pageSize,
        ...(statusFilter && {
          status: statusFilter as AlertListParams["status"],
        }),
        ...(severityFilter && { severity: severityFilter }),
      } as AlertListParams;
      const res: any = await alertApi.list(params);
      const body = res?.data?.data;
      const items = body?.items;
      return {
        items: Array.isArray(items) ? items : [],
        stats: body?.stats ?? null,
        total: body?.total ?? 0,
      };
    },
  );

  // 写操作 invalidate alerts 树
  const ackMut = useApiMutation((id: string) => alertApi.acknowledge(id), {
    onSuccess: () => {
      message.success("告警已确认");
      refetch();
    },
    onError: () => message.error("确认失败"),
  });
  const resolveMut = useApiMutation((id: string) => alertApi.resolve(id), {
    onSuccess: () => {
      message.success("告警已解决");
      refetch();
    },
    onError: () => message.error("解决失败"),
  });

  // C-P6: 批量写操作 (v1.3: 改逐条 ack/resolve 以显示 progress modal)
  const [bulkProgress, setBulkProgress] = useState<{
    open: boolean
    kind: "ack" | "resolve"
    total: number
    done: number
    failed: number
  } | null>(null)

  // 工具: 逐条执行, 实时更新进度
  async function runBulk(
    ids: string[],
    kind: "ack" | "resolve",
    action: (id: string) => Promise<unknown>,
  ) {
    setBulkProgress({ open: true, kind, total: ids.length, done: 0, failed: 0 })
    let done = 0
    let failed = 0
    for (const id of ids) {
      try {
        await action(id)
        done++
      } catch {
        failed++
      }
      setBulkProgress((p) => (p ? { ...p, done, failed } : null))
    }
    setBulkProgress((p) => (p ? { ...p, open: false } : null))
    const label = kind === "ack" ? "确认" : "解决"
    message.success(`批量${label}完成: 成功 ${done}, 失败 ${failed}`)
    setSelectedIds([])
    refetch()
  }

  const bulkAckMut = useApiMutation(
    (ids: string[]) => runBulk(ids, "ack", (id) => alertApi.acknowledge(id)),
    {
      onError: () => {
        setBulkProgress(null)
        message.error("批量确认失败")
      },
    },
  )
  const bulkResolveMut = useApiMutation(
    (ids: string[]) => runBulk(ids, "resolve", (id) => alertApi.resolve(id)),
    {
      onError: () => {
        setBulkProgress(null)
        message.error("批量解决失败")
      },
    },
  )

  // 小改进 #2：标记/反标记误报
  const markFPMut = useApiMutation(
    ({ id, isFP }: { id: string; isFP: boolean }) =>
      alertApi
        .markFalsePositive(id, isFP, isFP ? "运维标记" : "")
        .then((r) => ({ r, isFP })),
    {
      onSuccess: ({ isFP }) => {
        message.success(isFP ? "已标记为误报" : "已取消误报");
        refetch();
      },
      onError: () => message.error("操作失败"),
    },
  );

  // 小改进 #2：导出误报训练集 CSV（浏览器下载）
  const handleExportFP = async () => {
    try {
      const res = await alertApi.exportFalsePositives();
      const blob = new Blob([res.data], { type: "text/csv;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `false_positives_${new Date().toISOString().slice(0, 10)}.csv`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      message.success("已下载误报训练集 CSV");
    } catch (e) {
      message.error("导出失败");
    }
  };

  // P4：稳定回调引用，使 AlertTable 的 React.memo 生效（否则每渲染新建箭头函数，memo 白搭）
  // 解构出 mutate 再依赖：ackMut 对象每渲染新建，但 React Query 的 .mutate 引用稳定；
  // 直接依赖 ackMut.mutate 会被 exhaustive-deps 误判为缺 ackMut。
  const { mutate: ackMutate } = ackMut;
  const { mutate: resolveMutate } = resolveMut;
  const { mutate: markFPMutate } = markFPMut;
  const handleAck = useCallback((id: string) => ackMutate(id), [ackMutate]);
  const handleResolve = useCallback((id: string) => resolveMutate(id), [resolveMutate]);
  const handleMarkFP = useCallback(
    (id: string, isFP: boolean) => markFPMutate({ id, isFP }),
    [markFPMutate],
  );

  const list = data?.items ?? [];
  // 200 + 空 data 时 stats 为 null，直接读 stats.problem 会 TypeError 白屏
  const stats = data?.stats ?? EMPTY_STATS;
  const total = data?.total ?? 0;
  const hasSelection = selectedIds.length > 0;
  // M3/P5：受控分页回调（引用稳定，供 memo 化的 AlertTable 使用）
  const handlePageChange = useCallback((p: number, ps: number) => {
    setPage(p);
    setPageSize(ps);
  }, []);

  return (
    <div>
      <PageHeader
        title="告警中心"
        subtitle={`当前 ${stats.problem} 个未处理告警${hasSelection ? `，已选 ${selectedIds.length} 条` : ""}`}
        extra={
          <Space>
            {hasSelection && (
              <>
                {/* M14：批量操作误点立即生效 → 套 Popconfirm 二次确认（范本 Settings:503） */}
                <Popconfirm
                  title={`批量确认已选的 ${selectedIds.length} 条告警？`}
                  okText="确认"
                  cancelText="取消"
                  onConfirm={() => bulkAckMut.mutate(selectedIds)}
                >
                  <Button
                    icon={<CheckOutlined />}
                    loading={bulkAckMut.isPending}
                  >
                    批量确认
                  </Button>
                </Popconfirm>
                <Popconfirm
                  title={`批量解决已选的 ${selectedIds.length} 条告警？`}
                  okText="解决"
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                  onConfirm={() => bulkResolveMut.mutate(selectedIds)}
                >
                  <Button
                    type="primary"
                    icon={<CheckCircleOutlined />}
                    loading={bulkResolveMut.isPending}
                  >
                    批量解决
                  </Button>
                </Popconfirm>
              </>
            )}
            <Button
              icon={<DownloadOutlined />}
              onClick={handleExportFP}
              title="导出误报训练集 CSV（ML 用）"
            >
              导出训练集
            </Button>
            <Button icon={<SyncOutlined />} onClick={() => refetch()}>
              刷新
            </Button>
          </Space>
        }
      />

      {isError ? (
        <ErrorState error={error} onRetry={refetch} />
      ) : (
        <>
          <AlertStatsCards stats={stats} loading={isLoading} />

          <div style={{ marginBottom: 16 }}>
            <Space>
              <Select
                placeholder="状态"
                allowClear
                value={statusFilter || undefined}
                onChange={(v) => { setStatusFilter(v ?? ""); setPage(1); }}
                style={{ width: 120 }}
                options={[
                  { label: "未处理", value: "problem" },
                  { label: "已确认", value: "acknowledged" },
                  { label: "已解决", value: "resolved" },
                ]}
              />
              <Select
                placeholder="严重级别 ≥"
                allowClear
                value={severityFilter || undefined}
                onChange={(v) => { setSeverityFilter(v ?? ""); setPage(1); }}
                style={{ width: 140 }}
                options={[
                  { label: "灾难 (≥5)", value: "5" },
                  { label: "严重 (≥4)", value: "4" },
                  { label: "一般 (≥3)", value: "3" },
                ]}
              />
            </Space>
          </div>

          {isMobile ? (
            <MobileCardList
              data={list}
              loading={isLoading}
              emptyText="暂无告警"
              renderCard={(alert: Alert) => (
                <AlertCard
                  alert={alert}
                  onAck={handleAck}
                  onResolve={handleResolve}
                  onMarkFP={handleMarkFP}
                />
              )}
            />
          ) : (
            <AlertTable
              data={list}
              loading={isLoading}
              onAck={handleAck}
              onResolve={handleResolve}
              onMarkFP={handleMarkFP}
              selectedIds={selectedIds}
              onSelectionChange={setSelectedIds}
              total={total}
              page={page}
              pageSize={pageSize}
              onPageChange={handlePageChange}
            />
          )}
        </>
      )}

      {/* v1.3 批量操作进度 modal */}
      {bulkProgress && (
        <Modal
          open={bulkProgress.open}
          title={bulkProgress.kind === "ack" ? "批量确认告警" : "批量解决告警"}
          footer={null}
          closable={false}
          maskClosable={false}
        >
          <Progress
            percent={Math.round(((bulkProgress.done + bulkProgress.failed) / bulkProgress.total) * 100)}
            status="active"
          />
          {/* H6：`var(--ant-*)` 在未开 cssVar 时全部未定义（继承属性会回落，
              非继承属性如 color 会回落成初始值）——改用 theme token */}
          <div style={{ marginTop: 12, color: token.colorTextSecondary }}>
            进度: {bulkProgress.done + bulkProgress.failed} / {bulkProgress.total}
            {bulkProgress.failed > 0 && (
              <span style={{ marginLeft: 12, color: token.colorError }}>
                失败 {bulkProgress.failed} 条
              </span>
            )}
          </div>
        </Modal>
      )}
    </div>
  );
}

export default Alerts;
