import { getAnalyticsDomains, getWebsiteAnalytics, type IAnalyticsDomain, type IAnalyticsSubjectView } from "@/api/analytics";
import { Badge, Button, DataTable, Empty, Progress, Spin } from "@modra/react";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowRight, RefreshCw } from "@/ui/icons";
import React, { useEffect, useMemo, useState } from "react";
import type { EChartsOption } from "echarts";
import { useNavigate, useParams } from "react-router-dom";
import { BandwidthPanel } from "@/pages/Monitors/HostTraffics/BandwidthAnalysis";
import { ContributionPanel } from "@/pages/Monitors/HostTraffics/RequestContribution";
import { RequestHistoryList } from "@/pages/Monitors/RequestHistory/List";
import { acmeOrdersPath, hostDetailPath, requestHistoryPath } from "@/utils/analyticsLinks";
import { LocalTime } from "@/components/LocalTime";
import { SectionCard, SignalTag, WorkbenchPage, WorkbenchTabs } from "@/ui/workbench";
import { MonAlert, MonCard, MonDescriptions } from "@/ui/monParts";
import EChartPanel from "@/components/charts/EChartPanel";
import { chartPalette, useAppearance } from "@/appearance";
import { formatNumber, formatPercent } from "@/utils/formatters";
import { TimeWindowControl, useTimeWindow } from "./useTimeWindow";
import { MetricCards, ResourceTrend, metric } from "./ResourceMetrics";
import { websiteEditPath } from "./websiteViewRoute";
import type { Int64 } from "@/api/identifier";


const latencyOption = (domain: IAnalyticsDomain | undefined, colors: ReturnType<typeof chartPalette>): EChartsOption => ({
  color: [colors.flow, colors.alert, colors.neutral],
  tooltip: { trigger: "axis" },
  legend: { top: 4, data: ["P50", "P95", "P99"], textStyle: { color: colors.text } },
  grid: { left: 50, right: 24, top: 44, bottom: 26, containLabel: true },
  xAxis: { type: "category", data: domain ? [domain.domain] : [] },
  yAxis: { type: "value", name: "ms", axisLabel: { color: colors.neutral } },
  series: [
    { name: "P50", type: "bar", data: [domain?.p50Ms ?? null] },
    { name: "P95", type: "bar", data: [domain?.p95Ms ?? null] },
    { name: "P99", type: "bar", data: [domain?.p99Ms ?? null] }
  ]
});

const WebsiteAnalytics: React.FC = () => {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const { effectiveTheme } = useAppearance();
  const colors = chartPalette(effectiveTheme);
  const timeWindow = useTimeWindow();
  const window = timeWindow.window;
  const [data, setData] = useState<IAnalyticsSubjectView | null>();
  const [hostStats, setHostStats] = useState<IAnalyticsDomain[]>([]);
  const [loadingOverview, setLoadingOverview] = useState(true);
  const [loadingHosts, setLoadingHosts] = useState(true);
  const [error, setError] = useState("");

  const [refreshNonce, setRefreshNonce] = useState(0);
  const load = async () => {
    setRefreshNonce((value) => value + 1);
    setLoadingOverview(true); setLoadingHosts(true); setError("");
    try {
      const response = await getWebsiteAnalytics({ id: id as Int64, ...window });
      const subject = response.data ?? null;
      setData(subject);
      setLoadingOverview(false);
      if (subject?.hosts.length) {
        const hostsResponse = await getAnalyticsDomains({ domains: subject.hosts.map((host) => host.host), ...window });
        setHostStats(hostsResponse.data?.domains ?? []);
      } else setHostStats([]);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "无法加载站点分析，请稍后重试。"); setLoadingOverview(false); }
    finally { setLoadingHosts(false); }
  };
  useEffect(() => { void load(); }, [id, window.from, window.to]);

  const hostNames = useMemo(() => (data?.hosts ?? []).map((host) => host.host), [data]);
  // 站点没有任何 Host 时不能传空 domains，否则后端会汇总全部 Host。
  const trafficQuery = { from: window.from, to: window.to, domains: hostNames };
  const noHosts = <Empty description="该站点尚未绑定 Host" />;
  const overview = data?.overview;
  const kpis = overview?.kpis;
  const openHistory = (filters: Record<string, string | number>) => {
    navigate(requestHistoryPath({ ...filters, websiteId: id }));
  };
  const quality = overview ? `${overview.isPartial ? "统计桶仍在收集" : "数据完整"} · 延迟 ${formatNumber(overview.dataLagSeconds)} 秒` : "等待数据";
  const topHost = hostStats[0];
  const latencyWarning = (kpis?.maxDurationMs ?? 0) > 30000 || (kpis?.p95Ms ?? 0) > 30000;

  const hostColumns: ColumnDef<IAnalyticsDomain, unknown>[] = [
    { id: "domain", header: "Host", cell: ({ row }) => <Button variant="link" className="h-auto p-0" onClick={() => navigate(hostDetailPath(row.original.domain))}>{row.original.domain}</Button> },
    { id: "requests", header: "请求", cell: ({ row }) => formatNumber(row.original.requests) },
    { id: "errorRate", header: "错误率", cell: ({ row }) => formatPercent(row.original.errorRate) },
    { id: "p95Ms", header: "P95", cell: ({ row }) => metric(row.original.p95Ms, " ms") }
  ];
  const chipButton = "cursor-pointer";

  return <WorkbenchPage eyebrow="OBSERVABILITY / WEBSITE / ANALYTICS" title={data?.website?.name || "站点分析"} backTo={websiteEditPath(id)} description="聚合当前站点所有 Host 的流量、错误、延迟、证书覆盖和数据质量信号。" extra={<>
    <TimeWindowControl state={timeWindow} />
    <Button variant="outline" onClick={load}><RefreshCw className="size-4" aria-hidden="true" />刷新</Button>
    <Button variant="outline" onClick={() => navigate(websiteEditPath(id))}>配置站点</Button>
  </>}>
    {loadingOverview && !data ? <Spin /> : !data ? <Empty description="站点不存在或已删除" /> : <>
      {error ? <MonAlert type="error" message={error} /> : null}
      <WorkbenchTabs items={[{ key: "overview", label: "概览", children: <div className="flex flex-col gap-3">
      {latencyWarning ? <MonAlert type="warning" message="检测到异常耗时" description="P95 或最大耗时超过 30 秒。请结合客户端取消、上游超时和统计桶完整度判断是否为真实慢请求。" /> : null}
      <MetricCards overview={overview} />
      <ResourceTrend overview={overview} timeseries={data.timeseries} loading={loadingOverview} height={340} aside={
        <MonCard title="延迟摘要" loading={loadingOverview} className="min-h-[388px]">
          <div className="mb-3"><div className="mb-1 text-sm text-muted-foreground">延迟覆盖率 {Math.min(100, Math.round((kpis?.latencyCoverageRate ?? 0) * 100))}%</div><Progress value={Math.min(100, Math.round((kpis?.latencyCoverageRate ?? 0) * 100))} /></div>
          <MonDescriptions columnsClass="grid-cols-1" items={[{ key: "p50", label: "P50", children: metric(kpis?.p50Ms, " ms") }, { key: "p95", label: "P95", children: metric(kpis?.p95Ms, " ms") }, { key: "p99", label: "P99", children: metric(kpis?.p99Ms, " ms") }]} />
        </MonCard>
      } />
      </div> }, { key: "hosts", label: "Host 明细", children: <div className="flex flex-col gap-3">
      <SectionCard title="站点主机" description="点击 Host 可进入单 Host 的完整排查视图。"><MonDescriptions columnsClass="md:grid-cols-2" items={(data.hosts ?? []).map((host) => ({ key: host.host, label: <Button variant="link" className="h-auto p-0" onClick={() => navigate(hostDetailPath(host.host))}>{host.host}</Button>, children: <><SignalTag tone={host.certificate ? "success" : "warning"}>{host.certificate ? <>证书有效至 <LocalTime timestamp={host.certificate.notAfter} /></> : "未发现有效证书"}</SignalTag><Button variant="link" size="sm" onClick={() => navigate(acmeOrdersPath(host.host))}>ACME 订单</Button></> }))} /></SectionCard>
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
        <MonCard title="Host 排名" extra={<span className="text-sm text-muted-foreground">按请求量</span>}>
          <DataTable columns={hostColumns} data={hostStats} getRowId={(record) => record.domain} loading={loadingHosts} />
        </MonCard>
        <EChartPanel title="延迟分位" loading={loadingHosts} option={topHost ? latencyOption(topHost, colors) : undefined} isEmpty={!topHost} height={260} emptyDescription="暂无 Host 延迟数据" />
      </div>
      </div> }, { key: "contribution", label: "请求贡献", children: hostNames.length ? <div className="flex flex-col gap-3"><ContributionPanel query={trafficQuery} refreshToken={refreshNonce} /></div> : noHosts }, { key: "bandwidth", label: "带宽分析", children: hostNames.length ? <div className="flex flex-col gap-3"><BandwidthPanel query={trafficQuery} refreshToken={refreshNonce} onRetry={load} /></div> : noHosts }, { key: "history", label: "请求追踪", children: <RequestHistoryList fixedWebsiteId={id} embedded /> }, { key: "quality", label: "分布与数据质量", children: <div className="flex flex-col gap-3">
      <SectionCard title="分析状态" description="数据覆盖与采集延迟始终随窗口展示。"><div className="flex flex-wrap items-center gap-2"><SignalTag tone={overview?.isPartial ? "warning" : "success"}>{quality}</SignalTag><span className="text-sm text-muted-foreground">来源：{overview?.source || "站点汇总"}</span><Button variant="link" onClick={() => openHistory({ from: window.from, to: window.to })}>查看窗口请求 <ArrowRight className="size-4" aria-hidden="true" /></Button></div></SectionCard>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <MonCard title="状态码分布"><div className="flex flex-wrap gap-2">{(overview?.statusBreakdown ?? []).map((item) => {
          const isStatusFamily = /^[1-6]xx$/i.test(item.type);
          const variant = item.type.startsWith("5") || item.type.startsWith("6") ? "destructive" : "info";
          const label = `${item.type}: ${formatNumber(item.value)}`;
          return isStatusFamily
            ? <button key={item.type} type="button" onClick={() => openHistory({ statusCodePattern: item.type, from: window.from, to: window.to })}><Badge variant={variant} className={chipButton}>{label}</Badge></button>
            : <Badge key={item.type} variant={variant}>{label}</Badge>;
        })}</div></MonCard>
        <MonCard title="错误分布">{(overview?.errorBreakdown ?? []).length ? <div className="flex flex-wrap gap-2">{overview?.errorBreakdown.map((item) => <button key={item.type} type="button" onClick={() => openHistory({ errorCode: item.type, from: window.from, to: window.to })}><Badge variant="warning" className={chipButton}>{item.type}: {formatNumber(item.value)}</Badge></button>)}</div> : <span className="text-sm text-muted-foreground">当前窗口没有错误记录</span>}</MonCard>
        <MonCard title="采集覆盖"><MonDescriptions columnsClass="grid-cols-1" items={[{ key: "lag", label: "数据延迟", children: `${formatNumber(overview?.dataLagSeconds)} 秒` }, { key: "coverage", label: "延迟覆盖率", children: formatPercent(kpis?.latencyCoverageRate) }, { key: "hosts", label: "活跃 Host", children: formatNumber(data.hosts.length) }]} />
          <div className="mt-2">{overview?.isPartial ? <Badge variant="warning">当前窗口包含未完成统计桶</Badge> : <Badge variant="success">统计完整</Badge>}</div></MonCard>
      </div>
      </div> }]} />
    </>}
  </WorkbenchPage>;
};

export default WebsiteAnalytics;
