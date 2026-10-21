import type { EChartsOption } from "echarts";
import { Empty, Spin } from "@modra/react";
import React from "react";
import ReactECharts from "echarts-for-react";
import { MonCard } from "@/ui/monParts";

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
  const showChart = !loading && !isEmpty && Boolean(option);
  return (
    <MonCard title={title} extra={extra} className="h-full">
      <div
        className={showChart ? "block" : "flex items-center justify-center"}
        style={{ minHeight: `${!loading && (isEmpty || !option) && emptyHeight ? emptyHeight : height}px` }}
      >
        {loading ? (
          <Spin size="lg" />
        ) : !showChart ? (
          <div className="text-center">
            <Empty description={emptyDescription} />
            {hint ? <span className="mt-2 block text-sm text-muted-foreground">{hint}</span> : null}
          </div>
        ) : (
          <ReactECharts
            option={option}
            notMerge
            lazyUpdate
            opts={{ renderer: "svg" }}
            style={{ height: `${height}px` }}
            className="w-full"
          />
        )}
      </div>
    </MonCard>
  );
};

export default EChartPanel;
