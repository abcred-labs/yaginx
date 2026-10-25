import { overviewSection, getAnalyticsOverview, type IAnalyticsDomain, type IAnalyticsOverview, type IAnalyticsPoint } from "@/api/analytics";
import { chartPalette, useAppearance } from "@/appearance";
import EChartPanel from "@/components/charts/EChartPanel";
import { formatBytes, formatPercent } from "@/utils/formatters";
import { Button } from "@modra/react";
import { MonAlert } from "@/ui/monParts";
import React, { useEffect, useState } from "react";
import type { TrafficPanelProps } from "./panelTypes";
import { buildBandwidthRankOption, buildBandwidthTrendOption } from "./chartOptions";
import { HostTrafficKpiCards } from "./dashboard";
import { trafficGrainLabel } from "./shared";

export const BandwidthPanel: React.FC<TrafficPanelProps> = ({ query, refreshToken, onRetry, onMeta, hideKpis }) => {
  const { effectiveTheme } = useAppearance();
  const colors = chartPalette(effectiveTheme);
  const [loading, setLoading] = useState(true);
  const [overview, setOverview] = useState<IAnalyticsOverview>();
  const [domains, setDomains] = useState<IAnalyticsDomain[]>([]);
  const [points, setPoints] = useState<IAnalyticsPoint[]>([]);
  const [error, setError] = useState<string>();
  const queryKey = `${query.from}|${query.to}|${query.domain ?? ""}|${(query.domains ?? []).join(",")}`;
  useEffect(() => { onMeta?.({ grain: overview?.grain, coverageState: overview?.coverage.state }); }, [overview?.grain, overview?.coverage.state]);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(undefined);
    const params = { from: query.from, to: query.to, domain: query.domain, domains: query.domains };
    getAnalyticsOverview(params, ["overview", "domains", "timeseries"], { ignoreGlobalError: true, signal: controller.signal })
      .then((response) => {
        const summary = response.data ? overviewSection(response.data) : undefined;
        if (controller.signal.aborted) {
          return;
        }
        if (response.code !== 200 || !summary) {
          setError("流量消耗加载失败，请重试。");
          setOverview(undefined);
          setDomains([]);
          setPoints([]);
          return;
        }
        setOverview(summary);
        setDomains(response.data?.domains ?? []);
        setPoints(response.data?.points ?? []);
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setError("流量消耗加载失败，请检查网络后重试。");
          setOverview(undefined);
          setDomains([]);
          setPoints([]);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => {
      controller.abort();
    };
  }, [queryKey, refreshToken]);

  const rankings = domains.map((domain) => ({
    hostName: domain.domain,
    requestCounts: domain.requests,
    inboundBytes: domain.inboundBytes,
    outboundBytes: domain.outboundBytes,
    totalBytes: domain.inboundBytes + domain.outboundBytes,
    share: (overview?.kpis.inboundBytes ?? 0) + (overview?.kpis.outboundBytes ?? 0)
      ? (domain.inboundBytes + domain.outboundBytes) / ((overview?.kpis.inboundBytes ?? 0) + (overview?.kpis.outboundBytes ?? 0))
      : 0
  }));
  rankings.sort((left, right) => right.totalBytes - left.totalBytes || left.hostName.localeCompare(right.hostName));
  const topHost = rankings[0];
  const hasData = points.some((point) => point.inboundBytes > 0 || point.outboundBytes > 0);
  const grain = overview?.grain ?? "minute";

  return (
    <>
      {hideKpis ? null : <HostTrafficKpiCards
        loading={loading}
        items={[
          { title: "总流量", value: formatBytes((overview?.kpis.inboundBytes ?? 0) + (overview?.kpis.outboundBytes ?? 0)), detail: "流入与流出之和", tone: "flow" },
          { title: "流入流量", value: formatBytes(overview?.kpis.inboundBytes), detail: "客户端至代理", tone: "signal" },
          { title: "流出流量", value: formatBytes(overview?.kpis.outboundBytes), detail: "代理至客户端", tone: "warning" },
          {
            title: "最大流量 Host",
            value: topHost?.hostName || "—",
            detail: `${formatBytes(topHost?.totalBytes)} · ${formatPercent(topHost?.share)}`,
            tone: "alert"
          }
        ]}
      />}
      {error ? <MonAlert type="error" message={error} action={<Button size="sm" variant="outline" onClick={onRetry}>重试</Button>} /> : null}
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-24">
        <div className="xl:col-span-14">
          <EChartPanel
            title="入/出流量趋势"
            hint={`按${trafficGrainLabel(grain)}聚合，适合识别上传突增和大响应时段。`}
            loading={loading}
            isEmpty={!hasData}
            option={hasData ? buildBandwidthTrendOption(points, grain, colors) : undefined}
            height={350}
            emptyHeight={180}
            emptyDescription="当前筛选范围内暂无流量记录"
          />
        </div>
        <div className="xl:col-span-10">
          <EChartPanel
            title="Top 10 流量消耗"
            hint="按总字节数排序，流入和流出分段显示。"
            loading={loading}
            isEmpty={rankings.length === 0}
            option={rankings.length ? buildBandwidthRankOption(rankings, colors) : undefined}
            height={350}
            emptyHeight={180}
            emptyDescription="当前筛选范围内暂无流量排行"
          />
        </div>
      </div>
    </>
  );
};
