import { getAnalyticsDomains, getWebsiteAnalytics, type IAnalyticsDomain, type IAnalyticsSubjectView } from "@/api/analytics";
import { Alert, Button, Card, Col, DatePicker, Descriptions, Empty, Progress, Row, Segmented, Space, Spin, Statistic, Table, Tag, Typography } from "antd";
import { ArrowRightOutlined, ReloadOutlined } from "@ant-design/icons";
import { Dayjs } from "dayjs";
import React, { useEffect, useMemo, useState } from "react";
import type { EChartsOption } from "echarts";
import { useNavigate, useParams } from "react-router-dom";
import { LocalTime } from "@/componets/LocalTime";
import { SectionCard, SignalTag, WorkbenchPage } from "@/componets/Workbench";
import EChartPanel from "@/componets/charts/EChartPanel";
import { buildAnalyticsTrendOption } from "@/componets/charts/analyticsTrend";
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
  const [customRange, setCustomRange] = useState<[Dayjs | null, Dayjs | null] | null>(null);
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

  return <WorkbenchPage eyebrow="OBSERVABILITY / WEBSITE / ANALYTICS" title={data?.website?.name || "站点分析"} backTo={websiteEditPath(id)} description="聚合当前站点所有 Host 的流量、错误、延迟、证书覆盖和数据质量信号。" extra={<Space wrap><Segmented options={presets} value={range} onChange={(value) => { setCustomRange(null); setRange(Number(value)); }} /><DatePicker.RangePicker showTime value={customRange} onChange={(value) => setCustomRange(value as [Dayjs | null, Dayjs | null] | null)} /><Button icon={<ReloadOutlined />} onClick={load}>刷新</Button><Button onClick={() => navigate(websiteEditPath(id))}>配置站点</Button></Space>}>
    {loadingOverview && !data ? <Spin /> : !data ? <Empty description="站点不存在或已删除" /> : <>
      {error ? <Alert type="error" showIcon message={error} style={{ marginBottom: "var(--space-3)" }} /> : null}
      <SectionCard title="站点主机" description="点击 Host 可进入单 Host 的完整排查视图。"><Descriptions size="small" column={{ xs: 1, md: 2 }}>{(data.hosts ?? []).map((host) => <Descriptions.Item key={host.host} label={<Button type="link" style={{ padding: 0 }} onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(host.host)}`)}>{host.host}</Button>}><SignalTag tone={host.certificate ? "success" : "warning"}>{host.certificate ? <>证书有效至 <LocalTime timestamp={host.certificate.notAfter} /></> : "未发现有效证书"}</SignalTag></Descriptions.Item>)}</Descriptions></SectionCard>
      <SectionCard title="分析状态" description="数据覆盖与采集延迟始终随窗口展示。"><Space wrap><SignalTag tone={overview?.isPartial ? "warning" : "success"}>{quality}</SignalTag><Typography.Text type="secondary">来源：{overview?.source || "站点汇总"}</Typography.Text><Button type="link" onClick={() => openHistory({ from: window.from, to: window.to })}>查看窗口请求 <ArrowRightOutlined /></Button></Space></SectionCard>
      {latencyWarning ? <Alert type="warning" showIcon message="检测到异常耗时" description="P95 或最大耗时超过 30 秒。请结合客户端取消、上游超时和统计桶完整度判断是否为真实慢请求。" style={{ marginBottom: "var(--space-3)" }} /> : null}
      <Row gutter={[12, 12]}>{[
        ["请求总计", formatNumber(kpis?.requests)], ["当前吞吐", metric(kpis?.requestsPerSecond, " req/s")], ["错误率", formatPercent(toNumber(kpis?.errorRate))],
        ["P50 / P95 / P99", `${metric(kpis?.p50Ms, " ms")} / ${metric(kpis?.p95Ms, " ms")} / ${metric(kpis?.p99Ms, " ms")}`], ["入站 / 出站", `${formatBytes(toNumber(kpis?.inboundBytes))} / ${formatBytes(toNumber(kpis?.outboundBytes))}`], ["平均 / 最大耗时", `${metric(kpis?.avgDurationMs, " ms")} / ${metric(kpis?.maxDurationMs, " ms")}`]
      ].map(([title, value]) => <Col xs={12} md={8} xl={4} key={title}><Card size="small"><Statistic title={title} value={value} /></Card></Col>)}</Row>
      <Row gutter={[12, 12]} style={{ marginTop: 12 }}><Col xs={24} xl={16}><EChartPanel title="请求与错误趋势" loading={loadingOverview} isEmpty={!data.timeseries.points.length} option={data.timeseries.points.length ? buildAnalyticsTrendOption(data.timeseries.points, colors) : undefined} height={340} emptyDescription="当前窗口暂无观测数据" /></Col><Col xs={24} xl={8}><Card title="延迟摘要" loading={loadingOverview} style={{ height: 388 }}><Progress type="dashboard" percent={Math.min(100, Math.round((kpis?.latencyCoverageRate ?? 0) * 100))} format={(percent) => `${percent}%`} /><Descriptions size="small" column={1}><Descriptions.Item label="P50">{metric(kpis?.p50Ms, " ms")}</Descriptions.Item><Descriptions.Item label="P95">{metric(kpis?.p95Ms, " ms")}</Descriptions.Item><Descriptions.Item label="P99">{metric(kpis?.p99Ms, " ms")}</Descriptions.Item></Descriptions></Card></Col></Row>
      <Row gutter={[12, 12]} style={{ marginTop: 12 }}><Col xs={24} xl={12}><Card title="Host 排名" extra={<Typography.Text type="secondary">按请求量</Typography.Text>}><Table size="small" loading={loadingHosts} pagination={false} rowKey="domain" dataSource={hostStats} columns={[{ title: "Host", dataIndex: "domain", render: (value: string) => <Button type="link" style={{ padding: 0 }} onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(value)}`)}>{value}</Button> }, { title: "请求", dataIndex: "requests", render: (value: number) => formatNumber(value) }, { title: "错误率", dataIndex: "errorRate", render: (value: number) => formatPercent(value) }, { title: "P95", dataIndex: "p95Ms", render: (value: number | null) => metric(value, " ms") }]} /></Card></Col><Col xs={24} xl={12}><EChartPanel title="延迟分位" loading={loadingHosts} option={topHost ? latencyOption(topHost, colors) : undefined} isEmpty={!topHost} height={260} emptyDescription="暂无 Host 延迟数据" /></Col></Row>
      <Row gutter={[12, 12]} style={{ marginTop: 12 }}><Col xs={24} md={8}><Card title="状态码分布" size="small">{(overview?.statusBreakdown ?? []).map((item) => {
        const isStatusFamily = /^[1-6]xx$/i.test(item.type);
        return <Tag key={item.type} color={item.type.startsWith("5") || item.type.startsWith("6") ? "red" : "blue"} style={{ marginBottom: 8, cursor: isStatusFamily ? "pointer" : "default" }} onClick={isStatusFamily ? () => openHistory({ statusCodePattern: item.type, from: window.from, to: window.to }) : undefined}>{item.type}: {formatNumber(item.value)}</Tag>;
      })}</Card></Col><Col xs={24} md={8}><Card title="错误分布" size="small">{(overview?.errorBreakdown ?? []).length ? overview?.errorBreakdown.map((item) => <Tag key={item.type} color="volcano" style={{ marginBottom: 8, cursor: "pointer" }} onClick={() => openHistory({ errorCode: item.type, from: window.from, to: window.to })}>{item.type}: {formatNumber(item.value)}</Tag>) : <Typography.Text type="secondary">当前窗口没有错误记录</Typography.Text>}</Card></Col><Col xs={24} md={8}><Card title="采集覆盖" size="small"><Descriptions size="small" column={1}><Descriptions.Item label="数据延迟">{formatNumber(overview?.dataLagSeconds)} 秒</Descriptions.Item><Descriptions.Item label="延迟覆盖率">{formatPercent(kpis?.latencyCoverageRate)}</Descriptions.Item><Descriptions.Item label="活跃 Host">{formatNumber(data.hosts.length)}</Descriptions.Item></Descriptions>{overview?.isPartial ? <Tag color="gold">当前窗口包含未完成统计桶</Tag> : <Tag color="green">统计完整</Tag>}</Card></Col></Row>
    </>}
  </WorkbenchPage>;
};

export default WebsiteAnalytics;
