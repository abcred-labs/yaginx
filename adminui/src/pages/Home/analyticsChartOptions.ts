import type { EChartsOption } from "echarts";

interface MetricBarItem {
  type: string;
  value: number;
}

export const buildMetricBarOption = (title: string, items: MetricBarItem[], color: string): EChartsOption => {
  const values = [...items].sort((left, right) => right.value - left.value);
  return {
    color: [color],
    grid: { left: 96, right: 20, top: 24, bottom: 24 },
    tooltip: { trigger: "axis", axisPointer: { type: "shadow" } },
    xAxis: { type: "value" },
    yAxis: { type: "category", inverse: true, data: values.map((item) => item.type) },
    series: [{ name: title, type: "bar", data: values.map((item) => item.value), itemStyle: { borderRadius: [0, 8, 8, 0] } }]
  };
};
