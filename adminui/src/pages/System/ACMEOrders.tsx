import {
  Alert, Badge, Button, DataTable, Descriptions, Empty, Input, Pagination, Sheet, SheetContent, SheetHeader,
  SheetTitle, Timeline,
} from '@modra/react';
import type { ColumnDef } from '@tanstack/react-table';
import React, { useEffect, useMemo, useState } from 'react';
import { ACMEOrder, ACMEOrderLog, getACMELogs, getACMEOrderLogs, getACMEOrders, cancelACMEOrder, retryACMEOrder } from '@/api/acme';
import { notifySuccess } from '@/ui/notify';
import { SectionCard, WorkbenchPage, WorkbenchTabs } from '@/ui/workbench';

type SystemLog = Record<string, any>;

const labels: Record<string, string> = { pending: '等待处理', locked: '申请中', authorizing: '验证中', finalizing: '签发中', retry_wait: '等待重试', valid: '已完成', failed: '失败', canceled: '已取消' };
const operationLabels: Record<string, string> = { create: '创建订单', lock: '获取锁', lock_conflict: '锁冲突', authorizing: '开始验证', finalizing: '开始签发', retry: '安排重试', cancel: '取消订单', failed: '处理失败', invalid_domain: '域名无效', complete: '完成签发' };
const formatTime = (value?: string | number) => value ? new Date(typeof value === 'number' ? value * 1000 : value).toLocaleString() : '—';
const ORDER_LOG_PAGE_SIZE = 10;

const statusVariant = (status: string) => status === 'failed' ? 'destructive' : status === 'valid' ? 'success' : 'warning';

const StatusBadge: React.FC<{ status: string }> = ({ status }) => (
  <Badge variant={statusVariant(status)}>{labels[status] ?? status}</Badge>
);

const logColor = (result?: string) => result === 'failure' ? 'error' : result === 'success' ? 'success' : 'primary';

const ACMEOrders: React.FC = () => {
  const [rows, setRows] = useState<ACMEOrder[]>([]);
  const [loading, setLoading] = useState(false);
  const [domain, setDomain] = useState(() => new URLSearchParams(window.location.search).get('domain') ?? '');
  const [systemLogs, setSystemLogs] = useState<SystemLog[]>([]);
  const [systemLogsLoading, setSystemLogsLoading] = useState(false);
  const [selected, setSelected] = useState<ACMEOrder | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [orderLogs, setOrderLogs] = useState<ACMEOrderLog[]>([]);
  const [orderLogsTotal, setOrderLogsTotal] = useState(0);
  const [orderLogsPage, setOrderLogsPage] = useState(1);
  const [orderLogsLoading, setOrderLogsLoading] = useState(false);
  const [orderLogsError, setOrderLogsError] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    try {
      const r = await getACMEOrders({ pageSize: 200, domain: domain.trim() || undefined });
      if (r.code === 200) setRows(r.data.records);
    } finally {
      setLoading(false);
    }
  };
  const loadSystemLogs = async () => {
    setSystemLogsLoading(true);
    try {
      const r = await getACMELogs({ pageIndex: 1, pageSize: 100 });
      if (r.code === 200) setSystemLogs(r.data.records || []);
    } finally {
      setSystemLogsLoading(false);
    }
  };
  const loadOrderLogs = async (order: ACMEOrder, page = 1) => {
    setOrderLogsLoading(true);
    setOrderLogsError(null);
    try {
      const r = await getACMEOrderLogs(order.id, { pageIndex: page, pageSize: ORDER_LOG_PAGE_SIZE });
      if (r.code !== 200) throw new Error('日志加载失败');
      setOrderLogs(r.data.records || []);
      setOrderLogsTotal(r.data.paging?.total || 0);
      setOrderLogsPage(page);
    } catch (error) {
      setOrderLogsError(error instanceof Error ? error.message : '日志加载失败');
    } finally {
      setOrderLogsLoading(false);
    }
  };
  useEffect(() => { void load(); }, [domain]);
  useEffect(() => { void loadSystemLogs(); }, []);

  const openLogs = (order: ACMEOrder) => {
    setSelected(order);
    setDrawerOpen(true);
    void loadOrderLogs(order);
  };
  const action = async (fn: () => Promise<any>, msg: string) => {
    await fn();
    notifySuccess(msg);
    await load();
    await loadSystemLogs();
    if (selected) await loadOrderLogs(selected, orderLogsPage);
  };

  const orderColumns = useMemo<ColumnDef<ACMEOrder, unknown>[]>(() => [
    { accessorKey: 'domain', header: '域名' },
    { accessorKey: 'status', header: '状态', cell: ({ row }) => <StatusBadge status={row.original.status} /> },
    { accessorKey: 'attempt', header: '尝试次数' },
    { accessorKey: 'errorMessage', header: '错误' },
    { accessorKey: 'updatedAt', header: '更新时间', cell: ({ row }) => formatTime(row.original.updatedAt) },
    {
      id: 'actions',
      header: '操作',
      meta: { sticky: 'end' },
      cell: ({ row }) => {
        const order = row.original;
        return (
          <span className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => openLogs(order)}>日志</Button>
            {['failed', 'canceled', 'retry_wait'].includes(order.status)
              ? <Button size="sm" variant="outline" onClick={() => void action(() => retryACMEOrder(order.id), '已安排重试')}>重试</Button>
              : null}
            {['pending', 'locked', 'authorizing', 'finalizing', 'retry_wait'].includes(order.status)
              ? <Button size="sm" variant="destructive-outline" onClick={() => void action(() => cancelACMEOrder(order.id), '已取消订单')}>取消</Button>
              : null}
          </span>
        );
      },
    },
    // openLogs/action 通过闭包读取最新状态，列定义随选中订单与分页变化重建
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], [selected, orderLogsPage, domain]);

  const systemLogColumns = useMemo<ColumnDef<SystemLog, unknown>[]>(() => [
    { accessorKey: 'lastSeenAt', header: '时间', cell: ({ row }) => formatTime(row.original.lastSeenAt) },
    { accessorKey: 'operation', header: '操作', cell: ({ row }) => operationLabels[row.original.operation] ?? row.original.operation },
    { accessorKey: 'errorCode', header: '错误码' },
    { accessorKey: 'message', header: '消息' },
    { accessorKey: 'occurrenceCount', header: '次数' },
  ], []);

  return (
    <WorkbenchPage eyebrow="SYSTEM / ACME" title="ACME 订单" description="查看证书申请状态、错误和处理记录。">
      <WorkbenchTabs
        items={[
          {
            key: 'orders',
            label: '订单',
            children: (
              <SectionCard title="订单列表" description="订单状态来自共享 PostgreSQL。">
                <div className="mb-4 flex items-center gap-2">
                  <Input
                    className="w-64"
                    placeholder="按域名筛选"
                    value={domain}
                    onChange={(e) => setDomain(e.target.value)}
                    onKeyDown={(e) => { if (e.key === 'Enter') void load(); }}
                  />
                  <Button variant="outline" onClick={() => void load()}>刷新</Button>
                </div>
                <DataTable columns={orderColumns} data={rows} getRowId={(row) => row.id} loading={loading} showPagination pageSizeOptions={[10, 20, 50]} />
              </SectionCard>
            ),
          },
          {
            key: 'system-errors',
            label: '系统错误',
            children: (
              <SectionCard title="ACME 系统错误" description="这里记录全局 ACME 组件的系统级故障；单条订单的完整处理流水请点击订单行的“日志”。">
                <DataTable
                  columns={systemLogColumns}
                  data={systemLogs}
                  getRowId={(row, index) => String(row.id ?? index)}
                  loading={systemLogsLoading}
                  emptyText={<Empty description="暂无 ACME 系统级错误" />}
                  showPagination
                  pageSizeOptions={[10, 20, 50]}
                />
              </SectionCard>
            ),
          },
        ]}
      />
      <Sheet open={drawerOpen} onOpenChange={setDrawerOpen}>
        <SheetContent className="w-full overflow-y-auto sm:max-w-[620px]">
          <SheetHeader>
            <SheetTitle>{selected ? `订单日志 · ${selected.domain}` : '订单日志'}</SheetTitle>
          </SheetHeader>
          {selected ? (
            <div className="mt-4 flex flex-col gap-4">
              <Descriptions
                column={2}
                bordered
                items={[
                  { key: 'status', label: '当前状态', children: <StatusBadge status={selected.status} /> },
                  { key: 'attempt', label: '尝试次数', children: selected.attempt },
                  { key: 'updated', label: '更新时间', children: formatTime(selected.updatedAt) },
                ]}
              />
              {orderLogsError ? (
                <Alert variant="destructive" className="flex items-center justify-between gap-2">
                  <span>{orderLogsError}</span>
                  <Button size="sm" variant="outline" onClick={() => void loadOrderLogs(selected, orderLogsPage)}>重试</Button>
                </Alert>
              ) : null}
              {!orderLogsError && !orderLogsLoading && orderLogs.length === 0 ? (
                <Empty description="该订单暂无处理日志" />
              ) : (
                <>
                  <Timeline
                    items={orderLogs.map((log) => ({
                      key: log.id,
                      color: logColor(log.result),
                      children: (
                        <div className="flex flex-col gap-1.5">
                          <div className="flex flex-wrap items-center gap-2">
                            <strong>{operationLabels[log.operation] ?? log.operation}</strong>
                            <Badge variant="secondary">{labels[log.status] ?? log.status}</Badge>
                            {log.result ? <Badge variant={log.result === 'failure' ? 'destructive' : 'info'}>{log.result}</Badge> : null}
                            <span className="text-sm text-muted-foreground">第 {log.attempt} 次</span>
                          </div>
                          <p className="whitespace-pre-wrap break-words text-sm">
                            {log.message || '—'}
                            {log.errorCode ? <span className="text-destructive"> · {log.errorCode}</span> : null}
                          </p>
                          <span className="text-xs text-muted-foreground">
                            {formatTime(log.occurredAt)}{log.nodeId ? ` · 节点 ${log.nodeId}` : ''}
                          </span>
                        </div>
                      ),
                    }))}
                  />
                  {orderLogsLoading ? <span className="text-sm text-muted-foreground">加载中…</span> : null}
                </>
              )}
              {orderLogsTotal > ORDER_LOG_PAGE_SIZE ? (
                <Pagination
                  className="justify-end"
                  page={orderLogsPage}
                  pageSize={ORDER_LOG_PAGE_SIZE}
                  total={orderLogsTotal}
                  onPageChange={(page) => void loadOrderLogs(selected, page)}
                />
              ) : null}
            </div>
          ) : null}
        </SheetContent>
      </Sheet>
    </WorkbenchPage>
  );
};

export default ACMEOrders;
