import type { EChartsOption } from "echarts";
import { Card, Empty, Spin, Typography } from "antd";
import React from "react";
import ReactECharts from "echarts-for-react";
import "./chart.less";

interface EChartPanelProps {
  title: React.ReactNode;
  option?: EChartsOption;
  loading?: boolean;
  isEmpty?: boolean;
  height?: number;
  extra?: React.ReactNode;
  emptyDescription?: string;
  hint?: React.ReactNode;
  emptyHeight?: number;
}

const EChartPanel: React.FC<EChartPanelProps> = ({
  title,
  option,
  loading = false,
  isEmpty = false,
  height = 320,
  extra,
  emptyDescription = "暂无图表数据",
  hint,
  emptyHeight
}) => {
  return (
    <Card className="chart-panel" title={title} extra={extra}>
      <div
        className={`chart-panel__body ${!loading && !isEmpty ? "chart-panel__body--chart" : ""}`}
        style={{ minHeight: `${!loading && (isEmpty || !option) && emptyHeight ? emptyHeight : height}px` }}
      >
        {loading ? (
          <Spin size="large" />
        ) : isEmpty || !option ? (
          <div className="chart-panel__empty">
            <Empty description={emptyDescription} />
            {hint ? <Typography.Text type="secondary" className="chart-panel__hint">{hint}</Typography.Text> : null}
          </div>
        ) : (
          <ReactECharts
            option={option}
            notMerge
            lazyUpdate
            opts={{ renderer: "svg" }}
            style={{ height: `${height}px` }}
            className="chart-panel__echart"
          />
        )}
      </div>
    </Card>
  );
};

export default EChartPanel;
