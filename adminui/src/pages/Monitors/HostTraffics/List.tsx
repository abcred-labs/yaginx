import { getAnalyticsDetails, type IAnalyticsDetails } from "@/api/analytics";
import { DEFAULT_PAGE_INDEX, DEFAULT_PAGE_SIZE, PAGE_SIZE_OPTIONS } from "@/api/apiTypes";
import { LocalTime } from "@/componets/LocalTime";
import { SectionCard } from "@/ui/workbench";
import { formatBytes, formatNumber, formatPercent } from "@/utils/formatters";
import { Badge, Button, DataTable } from "@modra/react";
import type { ColumnDef } from "@tanstack/react-table";
import React, { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { HostTrafficPage } from "./HostTrafficPage";
import { HostTrafficKpiCards, useHostTrafficFilters, useHostTrafficRefresh, useHostTrafficWorkspace } from "./dashboard";
import { trafficGrainLabel } from "./shared";

const List: React.FC = () => {
  const navigate = useNavigate();
  const { filters, setFilters } = useHostTrafficFilters();
  const { autoRefreshSeconds, autoRefreshEnabled, setAutoRefreshSeconds, setAutoRefreshEnabled } = useHostTrafficWorkspace();
  const { refreshToken, refresh } = useHostTrafficRefresh(autoRefreshEnabled, autoRefreshSeconds);
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState<IAnalyticsDetails>();
  const [pagination, setPagination] = useState({ pageIndex: DEFAULT_PAGE_INDEX, pageSize: DEFAULT_PAGE_SIZE });

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getAnalyticsDetails({
      from: filters.from,
      to: filters.to,
      domain: filters.hostName || undefined,
      pageIndex: pagination.pageIndex,
      pageSize: pagination.pageSize
    })
      .then((response) => {
        if (!cancelled) setData(response.data);
      })
      .catch(() => {
        if (!cancelled) setData(undefined);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [filters, pagination, refreshToken]);

  useEffect(() => {
    setPagination((current) => current.pageIndex === 1 ? current : { ...current, pageIndex: 1 });
  }, [filters.from, filters.to, filters.hostName]);

  type DetailRow = IAnalyticsDetails["records"][number];
  const right = (title: string): ColumnDef<DetailRow, unknown>["header"] => () => <div className="text-right">{title}</div>;
  const num = (id: string, title: string, render: (row: DetailRow) => React.ReactNode, minWidth?: number): ColumnDef<DetailRow, unknown> => ({
    id,
    header: right(title),
    meta: { minWidth },
    cell: ({ row }) => <div className="text-right">{render(row.original)}</div>
  });
  const columns: ColumnDef<DetailRow, unknown>[] = [
    {
      id: "hostName",
      header: "Host",
      meta: { minWidth: 250 },
      cell: ({ row }) => <Button variant="link" className="h-auto p-0" onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(row.original.hostName)}`)}>{row.original.hostName}</Button>
    },
    {
      id: "period",
      header: "统计时间",
      meta: { minWidth: 170 },
      cell: ({ row }) => <LocalTime timestamp={row.original.period} format={data?.grain === "day" ? "YYYY-MM-DD" : data?.grain === "hour" ? "YYYY-MM-DD HH:00" : "YYYY-MM-DD HH:mm"} />
    },
    num("requestCounts", "请求次数", (r) => formatNumber(r.requestCounts)),
    num("errorRequests", "错误", (r) => formatNumber(r.errorRequests)),
    num("errorRate", "错误率", (r) => formatPercent(r.errorRate)),
    num("inboundBytes", "流入", (r) => formatBytes(r.inboundBytes)),
    num("outboundBytes", "流出", (r) => formatBytes(r.outboundBytes)),
    num("totalBytes", "总流量", (r) => formatBytes(r.totalBytes)),
    num("avgDurationMs", "平均延迟", (r) => `${formatNumber(r.avgDurationMs)} ms`),
    num("p95Ms", "P95", (r) => r.p95Ms == null ? "不可用" : `${formatNumber(r.p95Ms)} ms`),
    {
      id: "isPartial",
      header: "状态",
      meta: { minWidth: 108 },
      cell: ({ row }) => row.original.isPartial ? <Badge variant="warning">实时汇总中</Badge> : <Badge variant="success">完整</Badge>
    }
  ];
  const totalRequests = (data?.records ?? []).reduce((sum, item) => sum + item.requestCounts, 0);
  const totalBytes = (data?.records ?? []).reduce((sum, item) => sum + item.totalBytes, 0);

  return (
    <HostTrafficPage
      title="流量明细"
      description="按 Host 与自适应时间桶查看请求、流量、错误和延迟汇总。"
      filters={filters}
      onFiltersChange={setFilters}
      onRefresh={refresh}
      autoRefreshSeconds={autoRefreshSeconds}
      autoRefreshEnabled={autoRefreshEnabled}
      onAutoRefreshSecondsChange={setAutoRefreshSeconds}
      onAutoRefreshEnabledChange={setAutoRefreshEnabled}
      grain={data?.grain}
      coverageState={data?.coverage.state}
    >
      <HostTrafficKpiCards
        loading={loading}
        items={[
          { title: "当前页请求", value: formatNumber(totalRequests), detail: "当前筛选页内汇总", tone: "flow" },
          { title: "当前页流量", value: formatBytes(totalBytes), detail: "流入与流出之和", tone: "signal" },
          { title: "明细记录", value: formatNumber(data?.paging.total), detail: `按${trafficGrainLabel(data?.grain)}和 Host 分组`, tone: "warning" },
          { title: "当前桶", value: (data?.records ?? []).some((item) => item.isPartial) ? "实时汇总中" : "已完成", detail: "未完成桶会持续更新", tone: "alert" }
        ]}
      />
      <SectionCard title="Host 流量明细" description={`每条记录对应一个 Host 在一个${trafficGrainLabel(data?.grain)}统计周期内的聚合数据。`}>
        <DataTable
          columns={columns}
          data={data?.records ?? []}
          getRowId={(record) => String(record.id)}
          loading={loading}
          manualPagination
          pagination={{ pageIndex: (data?.paging.pageIndex ?? pagination.pageIndex) - 1, pageSize: data?.paging.pageSize ?? pagination.pageSize }}
          onPaginationChange={(updater) => {
            const current = { pageIndex: (data?.paging.pageIndex ?? pagination.pageIndex) - 1, pageSize: data?.paging.pageSize ?? pagination.pageSize };
            const next = typeof updater === "function" ? updater(current) : updater;
            setPagination({ pageIndex: next.pageIndex + 1, pageSize: next.pageSize });
          }}
          total={data?.paging.total ?? 0}
          showPagination
          pageSizeOptions={PAGE_SIZE_OPTIONS.map(Number)}
        />
      </SectionCard>
    </HostTrafficPage>
  );
};

export default List;
