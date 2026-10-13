import { SPACE } from "@/utils/spacing";
import { getAnalyticsContributions, type IAnalyticsContributions } from "@/api/analytics";
import { chartPalette, useAppearance } from "@/appearance";
import EChartPanel from "@/componets/charts/EChartPanel";
import { formatNumber, formatPercent } from "@/utils/formatters";
import { Col, Row } from "antd";
import React, { useEffect, useState } from "react";
import { HostTrafficPage } from "./HostTrafficPage";
import { buildRequestContributionOption, buildRequestRankOption } from "./chartOptions";
import { HostTrafficKpiCards, useHostTrafficFilters, useHostTrafficRefresh, useHostTrafficWorkspace } from "./dashboard";
import { trafficGrainLabel } from "./shared";

const RequestContribution: React.FC = () => {
  const { effectiveTheme } = useAppearance();
  const colors = chartPalette(effectiveTheme);
  const { filters, setFilters } = useHostTrafficFilters();
  const { autoRefreshSeconds, autoRefreshEnabled, setAutoRefreshSeconds, setAutoRefreshEnabled } = useHostTrafficWorkspace();
  const { refreshToken, refresh } = useHostTrafficRefresh(autoRefreshEnabled, autoRefreshSeconds);
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState<IAnalyticsContributions>();

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getAnalyticsContributions({ from: filters.from, to: filters.to, domain: filters.hostName || undefined })
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
  }, [filters, refreshToken]);

  const summary = data?.summary;
  const rankings = data?.rankings ?? [];
  const timeline = data?.timeline ?? [];
  const topHost = rankings[0];
  const hasData = rankings.length > 0;
  const grain = data?.grain ?? "minute";

  return (
    <HostTrafficPage
      title="请求贡献"
      description="将请求量按 Host 排序，快速区分主导流量与长尾访问。"
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
      />
      <Row gutter={[SPACE.space3, SPACE.space3]}>
        <Col xs={24} xl={14}>
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
        </Col>
        <Col xs={24} xl={10}>
          <EChartPanel
            title="Top 10 请求排行"
            loading={loading}
            isEmpty={!hasData}
            option={hasData ? buildRequestRankOption(rankings, colors) : undefined}
            height={350}
            emptyHeight={180}
            emptyDescription="当前筛选范围内暂无排行数据"
          />
        </Col>
      </Row>
    </HostTrafficPage>
  );
};

export default RequestContribution;
