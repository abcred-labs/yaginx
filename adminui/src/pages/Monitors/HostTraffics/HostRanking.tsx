import { getAnalyticsDomains, type IAnalyticsDomain } from "@/api/analytics";
import { formatBytes, formatNumber, formatPercent } from "@/utils/formatters";
import { hostDetailPath } from "@/utils/analyticsLinks";
import { SectionCard } from "@/ui/workbench";
import { MonSegmented } from "@/ui/monParts";
import { Button, DataTable } from "@modra/react";
import type { ColumnDef } from "@tanstack/react-table";
import React, { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { BandwidthPanel } from "./BandwidthAnalysis";
import { ContributionPanel } from "./RequestContribution";
import { HostTrafficPage } from "./HostTrafficPage";
import { HostTrafficKpiCards, useHostTrafficFilters, useHostTrafficRefresh, useHostTrafficWorkspace } from "./dashboard";
import type { TrafficPanelMeta } from "./panelTypes";

type SortKey = "requests" | "bytes" | "errorRate" | "p95Ms";

const sortOptions: Array<{ label: string; value: SortKey }> = [
  { label: "按请求数", value: "requests" },
  { label: "按流量", value: "bytes" },
  { label: "按错误率", value: "errorRate" },
  { label: "按 P95", value: "p95Ms" }
];

const sortValue = (row: IAnalyticsDomain, key: SortKey): number => {
  if (key === "bytes") return row.inboundBytes + row.outboundBytes;
  return Number(row[key] ?? 0);
};

// 全局视图：一张 Host 排行表加请求贡献、带宽两组图，取代原来的贡献 / 带宽 / 明细三个标签。
const HostRanking: React.FC = () => {
  const navigate = useNavigate();
  const { filters, setFilters } = useHostTrafficFilters();
  const { autoRefreshSeconds, autoRefreshEnabled, setAutoRefreshSeconds, setAutoRefreshEnabled } = useHostTrafficWorkspace();
  const { refreshToken, refresh } = useHostTrafficRefresh(autoRefreshEnabled, autoRefreshSeconds);
  const [loading, setLoading] = useState(true);
  const [domains, setDomains] = useState<IAnalyticsDomain[]>([]);
  const [meta, setMeta] = useState<TrafficPanelMeta>({});
  const [sortKey, setSortKey] = useState<SortKey>("requests");

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    getAnalyticsDomains({ from: filters.from, to: filters.to }, { ignoreGlobalError: true, signal: controller.signal })
      .then((response) => { if (!controller.signal.aborted) setDomains(response.data?.domains ?? []); })
      .catch(() => { if (!controller.signal.aborted) setDomains([]); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [filters.from, filters.to, refreshToken]);

  const rows = useMemo(
    () => [...domains].sort((left, right) => sortValue(right, sortKey) - sortValue(left, sortKey) || left.domain.localeCompare(right.domain)),
    [domains, sortKey]
  );
  const totalRequests = domains.reduce((sum, item) => sum + item.requests, 0);
  const totalBytes = domains.reduce((sum, item) => sum + item.inboundBytes + item.outboundBytes, 0);
  const top = rows[0];

  const right = (title: string): ColumnDef<IAnalyticsDomain, unknown>["header"] => () => <div className="text-right">{title}</div>;
  const num = (id: string, title: string, render: (row: IAnalyticsDomain) => React.ReactNode): ColumnDef<IAnalyticsDomain, unknown> => ({
    id, header: right(title), cell: ({ row }) => <div className="text-right">{render(row.original)}</div>
  });
  const columns: ColumnDef<IAnalyticsDomain, unknown>[] = [
    { id: "domain", header: "Host", meta: { minWidth: 240 }, cell: ({ row }) => <Button variant="link" className="h-auto p-0" onClick={() => navigate(hostDetailPath(row.original.domain))}>{row.original.domain}</Button> },
    num("requests", "请求数", (r) => formatNumber(r.requests)),
    num("share", "请求占比", (r) => formatPercent(totalRequests ? r.requests / totalRequests : 0)),
    num("inbound", "流入", (r) => formatBytes(r.inboundBytes)),
    num("outbound", "流出", (r) => formatBytes(r.outboundBytes)),
    num("errorRate", "错误率", (r) => formatPercent(r.errorRate)),
    num("p95Ms", "P95", (r) => r.p95Ms == null ? "不可用" : `${formatNumber(r.p95Ms)} ms`)
  ];
  const query = { from: filters.from, to: filters.to };

  return (
    <HostTrafficPage
      title="Host 排行"
      description="所有 Host 的请求、流量、错误率和延迟排行；点击 Host 进入详情，查看单个站点请从站点进入。"
      filters={filters}
      onFiltersChange={setFilters}
      onRefresh={refresh}
      autoRefreshSeconds={autoRefreshSeconds}
      autoRefreshEnabled={autoRefreshEnabled}
      onAutoRefreshSecondsChange={setAutoRefreshSeconds}
      onAutoRefreshEnabledChange={setAutoRefreshEnabled}
      grain={meta.grain}
      coverageState={meta.coverageState}
    >
      <HostTrafficKpiCards
        loading={loading}
        items={[
          { title: "总请求量", value: formatNumber(totalRequests), detail: "所有 Host 累计", tone: "flow" },
          { title: "活跃 Host", value: formatNumber(domains.length), detail: "产生请求的 Host 数", tone: "signal" },
          { title: "总流量", value: formatBytes(totalBytes), detail: "流入与流出之和", tone: "warning" },
          { title: "Top Host", value: top?.domain || "—", detail: "当前排序依据下的第一名", tone: "alert" }
        ]}
      />
      <SectionCard title="Host 排行表" description="同一批 Host 的请求、流量、错误率和延迟，可切换排序依据。" extra={<MonSegmented ariaLabel="排序依据" options={sortOptions} value={sortKey} onChange={(value) => setSortKey(value as SortKey)} />}>
        <DataTable columns={columns} data={rows} getRowId={(record) => record.domain} loading={loading} />
      </SectionCard>
      <ContributionPanel query={query} refreshToken={refreshToken} onMeta={setMeta} hideKpis />
      <BandwidthPanel query={query} refreshToken={refreshToken} onRetry={refresh} hideKpis />
    </HostTrafficPage>
  );
};

export default HostRanking;
