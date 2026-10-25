import { getAnalyticsContributions, type IAnalyticsContributions } from "@/api/analytics";
import { chartPalette, useAppearance } from "@/appearance";
import EChartPanel from "@/components/charts/EChartPanel";
import { formatNumber, formatPercent } from "@/utils/formatters";
import React, { useEffect, useState } from "react";
import type { TrafficPanelProps } from "./panelTypes";
import { buildRequestContributionOption, buildRequestRankOption } from "./chartOptions";
import { HostTrafficKpiCards } from "./dashboard";
import { trafficGrainLabel } from "./shared";

export const ContributionPanel: React.FC<TrafficPanelProps> = ({ query, refreshToken, onMeta, hideKpis }) => {
  const { effectiveTheme } = useAppearance();
  const colors = chartPalette(effectiveTheme);
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState<IAnalyticsContributions>();
  const queryKey = `${query.from}|${query.to}|${query.domain ?? ""}|${(query.domains ?? []).join(",")}`;
  useEffect(() => { onMeta?.({ grain: data?.grain, coverageState: data?.coverage.state }); }, [data?.grain, data?.coverage.state]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getAnalyticsContributions({ from: query.from, to: query.to, domain: query.domain, domains: query.domains })
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
  }, [queryKey, refreshToken]);

  const summary = data?.summary;
  const rankings = data?.rankings ?? [];
  const timeline = data?.timeline ?? [];
  const topHost = rankings[0];
  const hasData = rankings.length > 0;
  const grain = data?.grain ?? "minute";

  return (
    <>
      {hideKpis ? null : <HostTrafficKpiCards
        loading={loading}
        items={[
          { title: "总请求量", value: formatNumber(summary?.totalRequests), detail: "筛选范围内累计", tone: "flow" },
          { title: "活跃 Host", value: formatNumber(summary?.activeHostCount), detail: "按请求量参与排行", tone: "signal" },
          { title: "Top Host", value: summary?.topHostName || "—", detail: "请求量最高的 Host", tone: "warning" },
          {
            title: "Top Host 占比",
            value: formatPercent(topHost?.share),
            detail: `${formatNumber(summary?.topHostRequestCounts)} 次请求`,
            tone: "alert"
          }
        ]}
      />}
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-24">
        <div className="xl:col-span-14">
          <EChartPanel
            title="Top 5 请求贡献趋势"
            hint={`按${trafficGrainLabel(grain)}聚合；其余 Host 合并为“其他”。`}
            loading={loading}
            isEmpty={!hasData}
            option={hasData ? buildRequestContributionOption(timeline, grain, colors) : undefined}
            height={350}
            emptyHeight={180}
            emptyDescription="当前筛选范围内暂无 Host 请求贡献"
          />
        </div>
        <div className="xl:col-span-10">
          <EChartPanel
            title="Top 10 请求排行"
            loading={loading}
            isEmpty={!hasData}
            option={hasData ? buildRequestRankOption(rankings, colors) : undefined}
            height={350}
            emptyHeight={180}
            emptyDescription="当前筛选范围内暂无排行数据"
          />
        </div>
      </div>
    </>
  );
};
