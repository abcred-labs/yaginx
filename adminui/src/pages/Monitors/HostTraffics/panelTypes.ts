import type { AnalyticsQuery } from "@/api/analytics";

export interface TrafficPanelMeta {
  grain?: "minute" | "hour" | "day";
  coverageState?: "collecting" | "complete" | "incomplete";
}

// 流量面板与筛选栏解耦：站点页传入固定的 domains，全局页传入用户输入的 domain。
export interface TrafficPanelProps {
  query: AnalyticsQuery & { from: number; to: number };
  refreshToken: number;
  onRetry?: () => void;
  // 嵌入汇总页时由外层统一展示 KPI，避免重复卡片。
  hideKpis?: boolean;
  onMeta?: (meta: TrafficPanelMeta) => void;
}
