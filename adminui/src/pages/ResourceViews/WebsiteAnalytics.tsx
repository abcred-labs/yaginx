import { getAnalyticsDomains, getWebsiteAnalytics, type IAnalyticsDomain, type IAnalyticsSubjectView } from "@/api/analytics";
import { Badge, Button, Card, CardContent, DataTable, Empty, Progress, Spin, Statistic } from "@modra/react";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowRight, RefreshCw } from "@/ui/icons";
import React, { useEffect, useMemo, useState } from "react";
import type { EChartsOption } from "echarts";
import { useNavigate, useParams } from "react-router-dom";
import { LocalTime } from "@/components/LocalTime";
import { SectionCard, SignalTag, WorkbenchPage } from "@/ui/workbench";
import { MonAlert, MonCard, MonDateTimeRange, MonDescriptions, MonSegmented, type MonDayjsRange } from "@/ui/monParts";
import EChartPanel from "@/components/charts/EChartPanel";
import { buildAnalyticsTrendOption } from "@/components/charts/analyticsTrend";
import { chartPalette, useAppearance } from "@/appearance";
import { formatBytes, formatNumber, formatPercent, toNumber } from "@/utils/formatters";
import { defaultResourceViewWindowSeconds } from "./resourceViewDefaults";
import { websiteEditPath } from "./websiteViewRoute";
import type { Int64 } from "@/api/identifier";

const presets = [{ label: "1 小时", value: 3600 }, { label: "24 小时", value: 86400 }, { label: "7 天", value: 604800 }];
const metric = (value: number | null | undefined, suffix = "") => value == null ? "不可用" : `${formatNumber(value)}${suffix}`;

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
  const [range, setRange] = useState(defaultResourceViewWindowSeconds);
  const [customRange, setCustomRange] = useState<MonDayjsRange | null>(null);
  const [data, setData] = useState<IAnalyticsSubjectView | null>();
  const [hostStats, setHostStats] = useState<IAnalyticsDomain[]>([]);
  const [loadingOverview, setLoadingOverview] = useState(true);
  const [loadingHosts, setLoadingHosts] = useState(true);
  const [error, setError] = useState("");
  const window = useMemo(() => { const to = Math.floor(Date.now() / 1000); if (customRange?.[0] && customRange?.[1]) return { from: Math.floor(customRange[0].valueOf() / 1000), to: Math.floor(customRange[1].valueOf() / 1000) }; return { from: to - range, to }; }, [range, customRange]);

  const load = async () => {
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

  const overview = data?.overview;
  const kpis = overview?.kpis;
  const openHistory = (filters: Record<string, string | number>) => {
    const query = new URLSearchParams(Object.entries(filters).map(([key, value]) => [key, String(value)]));
    query.set("websiteId", id);
    navigate(`/monitor/requestHistory/list?${query.toString()}`);
  };
  const quality = overview ? `${overview.isPartial ? "统计桶仍在收集" : "数据完整"} · 延迟 ${formatNumber(overview.dataLagSeconds)} 秒` : "等待数据";
  const topHost = hostStats[0];
  const latencyWarning = (kpis?.maxDurationMs ?? 0) > 30000 || (kpis?.p95Ms ?? 0) > 30000;

  const hostColumns: ColumnDef<IAnalyticsDomain, unknown>[] = [
    { id: "domain", header: "Host", cell: ({ row }) => <Button variant="link" className="h-auto p-0" onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(row.original.domain)}`)}>{row.original.domain}</Button> },
    { id: "requests", header: "请求", cell: ({ row }) => formatNumber(row.original.requests) },
    { id: "errorRate", header: "错误率", cell: ({ row }) => formatPercent(row.original.errorRate) },
    { id: "p95Ms", header: "P95", cell: ({ row }) => metric(row.original.p95Ms, " ms") }
  ];
  const kpiCards = [
    ["请求总计", formatNumber(kpis?.requests)], ["当前吞吐", metric(kpis?.requestsPerSecond, " req/s")], ["错误率", formatPercent(toNumber(kpis?.errorRate))],
    ["P50 / P95 / P99", `${metric(kpis?.p50Ms, " ms")} / ${metric(kpis?.p95Ms, " ms")} / ${metric(kpis?.p99Ms, " ms")}`], ["入站 / 出站", `${formatBytes(toNumber(kpis?.inboundBytes))} / ${formatBytes(toNumber(kpis?.outboundBytes))}`], ["平均 / 最大耗时", `${metric(kpis?.avgDurationMs, " ms")} / ${metric(kpis?.maxDurationMs, " ms")}`]
  ];
  const chipButton = "cursor-pointer";

  return <WorkbenchPage eyebrow="OBSERVABILITY / WEBSITE / ANALYTICS" title={data?.website?.name || "站点分析"} backTo={websiteEditPath(id)} description="聚合当前站点所有 Host 的流量、错误、延迟、证书覆盖和数据质量信号。" extra={<>
    <MonSegmented options={presets} value={range} ariaLabel="统计窗口" onChange={(value) => { setCustomRange(null); setRange(value); }} />
    <MonDateTimeRange value={customRange} onChange={setCustomRange} className="w-[320px]" />
    <Button variant="outline" onClick={load}><RefreshCw className="size-4" aria-hidden="true" />刷新</Button>
    <Button variant="outline" onClick={() => navigate(websiteEditPath(id))}>配置站点</Button>
  </>}>
    {loadingOverview && !data ? <Spin /> : !data ? <Empty description="站点不存在或已删除" /> : <>
      {error ? <MonAlert type="error" message={error} /> : null}
      <SectionCard title="站点主机" description="点击 Host 可进入单 Host 的完整排查视图。"><MonDescriptions columnsClass="md:grid-cols-2" items={(data.hosts ?? []).map((host) => ({ key: host.host, label: <Button variant="link" className="h-auto p-0" onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(host.host)}`)}>{host.host}</Button>, children: <SignalTag tone={host.certificate ? "success" : "warning"}>{host.certificate ? <>证书有效至 <LocalTime timestamp={host.certificate.notAfter} /></> : "未发现有效证书"}</SignalTag> }))} /></SectionCard>
      <SectionCard title="分析状态" description="数据覆盖与采集延迟始终随窗口展示。"><div className="flex flex-wrap items-center gap-2"><SignalTag tone={overview?.isPartial ? "warning" : "success"}>{quality}</SignalTag><span className="text-sm text-muted-foreground">来源：{overview?.source || "站点汇总"}</span><Button variant="link" onClick={() => openHistory({ from: window.from, to: window.to })}>查看窗口请求 <ArrowRight className="size-4" aria-hidden="true" /></Button></div></SectionCard>
      {latencyWarning ? <MonAlert type="warning" message="检测到异常耗时" description="P95 或最大耗时超过 30 秒。请结合客户端取消、上游超时和统计桶完整度判断是否为真实慢请求。" /> : null}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">{kpiCards.map(([title, value]) => <Card key={title}><CardContent className="p-4"><Statistic title={title} value={value} /></CardContent></Card>)}</div>
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-3">
        <div className="xl:col-span-2"><EChartPanel title="请求与错误趋势" loading={loadingOverview} isEmpty={!data.timeseries.points.length} option={data.timeseries.points.length ? buildAnalyticsTrendOption(data.timeseries.points, colors) : undefined} height={340} emptyDescription="当前窗口暂无观测数据" /></div>
        <MonCard title="延迟摘要" loading={loadingOverview} className="min-h-[388px]">
          <div className="mb-3"><div className="mb-1 text-sm text-muted-foreground">延迟覆盖率 {Math.min(100, Math.round((kpis?.latencyCoverageRate ?? 0) * 100))}%</div><Progress value={Math.min(100, Math.round((kpis?.latencyCoverageRate ?? 0) * 100))} /></div>
          <MonDescriptions columnsClass="grid-cols-1" items={[{ key: "p50", label: "P50", children: metric(kpis?.p50Ms, " ms") }, { key: "p95", label: "P95", children: metric(kpis?.p95Ms, " ms") }, { key: "p99", label: "P99", children: metric(kpis?.p99Ms, " ms") }]} />
        </MonCard>
      </div>
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
        <MonCard title="Host 排名" extra={<span className="text-sm text-muted-foreground">按请求量</span>}>
          <DataTable columns={hostColumns} data={hostStats} getRowId={(record) => record.domain} loading={loadingHosts} />
        </MonCard>
        <EChartPanel title="延迟分位" loading={loadingHosts} option={topHost ? latencyOption(topHost, colors) : undefined} isEmpty={!topHost} height={260} emptyDescription="暂无 Host 延迟数据" />
      </div>
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
    </>}
  </WorkbenchPage>;
};

export default WebsiteAnalytics;
