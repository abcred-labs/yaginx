import { overviewSection, getAnalyticsOverview, type IAnalyticsOverview, type IAnalyticsPoint } from "@/api/analytics";
import { chartPalette, useAppearance } from "@/appearance";
import EChartPanel from "@/components/charts/EChartPanel";
import { formatNumber } from "@/utils/formatters";
import { Button } from "@modra/react";
import { MonAlert } from "@/ui/monParts";
import React, { useEffect, useState } from "react";
import { HostTrafficPage } from "./HostTrafficPage";
import { buildRequestTrendOption } from "./chartOptions";
import { HostTrafficKpiCards, useHostTrafficFilters, useHostTrafficRefresh, useHostTrafficWorkspace } from "./dashboard";
import { formatTrafficPeriod, trafficGrainLabel } from "./shared";

const RequestTrend: React.FC = () => {
  const { effectiveTheme } = useAppearance();
  const colors = chartPalette(effectiveTheme);
  const { filters, setFilters } = useHostTrafficFilters();
  const { autoRefreshSeconds, autoRefreshEnabled, setAutoRefreshSeconds, setAutoRefreshEnabled } = useHostTrafficWorkspace();
  const { refreshToken, refresh } = useHostTrafficRefresh(autoRefreshEnabled, autoRefreshSeconds);
  const [loading, setLoading] = useState(true);
  const [overview, setOverview] = useState<IAnalyticsOverview>();
  const [points, setPoints] = useState<IAnalyticsPoint[]>([]);
  const [error, setError] = useState<string>();

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(undefined);
    const params = { from: filters.from, to: filters.to };
    getAnalyticsOverview(params, ["overview", "timeseries"], { ignoreGlobalError: true, signal: controller.signal })
      .then((response) => {
        const summary = response.data ? overviewSection(response.data) : undefined;
        if (controller.signal.aborted) {
          return;
        }
        if (response.code !== 200 || !summary) {
          setError("请求趋势加载失败，请重试。");
          setOverview(undefined);
          setPoints([]);
          return;
        }
        setOverview(summary);
        setPoints(response.data?.points ?? []);
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setError("请求趋势加载失败，请检查网络后重试。");
          setOverview(undefined);
          setPoints([]);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => {
      controller.abort();
    };
  }, [filters, refreshToken]);

  const grain = overview?.grain ?? "minute";
  const peak = points.reduce<IAnalyticsPoint | undefined>((current, point) => !current || point.requests > current.requests ? point : current, undefined);
  const hasData = points.some((point) => point.requests > 0);
  const average = points.length ? (overview?.kpis.requests ?? 0) / points.length : 0;

  return (
    <HostTrafficPage
      title="请求趋势"
      description="全部 Host 的请求变化，快速定位异常时段；查看单个站点请从站点进入。"
      filters={filters}
      onFiltersChange={setFilters}
      onRefresh={refresh}
      autoRefreshSeconds={autoRefreshSeconds}
      autoRefreshEnabled={autoRefreshEnabled}
      onAutoRefreshSecondsChange={setAutoRefreshSeconds}
      onAutoRefreshEnabledChange={setAutoRefreshEnabled}
      grain={overview?.grain}
      coverageState={overview?.coverage.state}
    >
      <HostTrafficKpiCards
        loading={loading}
        items={[
          { title: "总请求量", value: formatNumber(overview?.kpis.requests), detail: "筛选范围内累计", tone: "flow" },
          { title: "活跃 Host", value: formatNumber(overview?.activeHostCount), detail: "产生请求的 Host 数", tone: "signal" },
          { title: `${trafficGrainLabel(grain)}均值`, value: formatNumber(average), detail: `按${trafficGrainLabel(grain)}聚合`, tone: "warning" },
          {
            title: "请求峰值",
            value: formatNumber(peak?.requests),
            detail: peak?.requests ? formatTrafficPeriod(peak.bucketStartAt, grain === "day" ? "MM-DD" : grain === "hour" ? "MM-DD HH:00" : "MM-DD HH:mm") : "当前范围暂无峰值",
            tone: "alert"
          }
        ]}
      />
      {error ? <MonAlert type="error" message={error} action={<Button size="sm" variant="outline" onClick={refresh}>重试</Button>} /> : null}
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-24">
        <div className="xl:col-span-24">
          <EChartPanel
            title="请求量趋势"
            hint={`按${trafficGrainLabel(grain)}聚合；空心点表示当前桶仍在实时汇总。`}
            loading={loading}
            isEmpty={!hasData}
            option={hasData ? buildRequestTrendOption(points, grain, colors) : undefined}
            height={360}
            emptyHeight={180}
            emptyDescription="当前筛选范围内暂无请求记录"
          />
        </div>
      </div>
    </HostTrafficPage>
  );
};

export default RequestTrend;
