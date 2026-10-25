import { Badge, Button } from "@modra/react";
import React, { useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { MonCard } from "@/ui/monParts";
import { DatabaseHealthPanel, GatewayHealthPanel, TrafficBreakdownPanel, TrafficKpiPanel, TrafficTrendPanel } from "./DashboardPanels";
import { RefreshRequestKind } from "./refreshMode";

interface AnalyticsDashboardProps {
  range: number;
  refreshKey: number;
  refreshKind: RefreshRequestKind;
  autoRefreshSeconds?: number;
  selectedDomain?: string;
  onSelectedDomainChange: (domain?: string) => void;
  onAutoRefreshLoadingChange: (isLoading: boolean) => void;
}

export const AnalyticsDashboard: React.FC<AnalyticsDashboardProps> = ({ range, refreshKey, autoRefreshSeconds, selectedDomain, onAutoRefreshLoadingChange }) => {
  const navigate = useNavigate();
  const pending = useRef(new Set<string>());
  const notifyPanelLoading = useCallback((panel: string, loading: boolean) => {
    if (!autoRefreshSeconds) return;
    if (loading) pending.current.add(panel); else pending.current.delete(panel);
    onAutoRefreshLoadingChange(pending.current.size > 0);
  }, [autoRefreshSeconds, onAutoRefreshLoadingChange]);
  const kpiLoading = useCallback((loading: boolean) => notifyPanelLoading("kpi", loading), [notifyPanelLoading]);
  const trendLoading = useCallback((loading: boolean) => notifyPanelLoading("trend", loading), [notifyPanelLoading]);
  const breakdownLoading = useCallback((loading: boolean) => notifyPanelLoading("breakdown", loading), [notifyPanelLoading]);
  return <section className="flex flex-col gap-3">
    <TrafficKpiPanel range={range} selectedDomain={selectedDomain} refreshKey={refreshKey} autoRefreshSeconds={autoRefreshSeconds} onLoadingChange={kpiLoading} />
    <div className="grid grid-cols-1 gap-3 xl:grid-cols-3">
      <div className="xl:col-span-2"><TrafficTrendPanel range={range} selectedDomain={selectedDomain} refreshKey={refreshKey} autoRefreshSeconds={autoRefreshSeconds} onLoadingChange={trendLoading} /></div>
      <div className="flex flex-col gap-3"><GatewayHealthPanel /><DatabaseHealthPanel /></div>
    </div>
    <TrafficBreakdownPanel range={range} selectedDomain={selectedDomain} refreshKey={refreshKey} autoRefreshSeconds={autoRefreshSeconds} onLoadingChange={breakdownLoading} />
    <MonCard title="运行状态与下一步">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="success">各面板独立刷新</Badge>
        <span className="text-sm text-muted-foreground">基础设施每 15 秒更新，流量面板跟随当前窗口刷新。</span>
        <Button onClick={() => navigate("/monitor/traffic/request-trend")}>打开流量分析</Button>
        <Button variant="outline" onClick={() => navigate("/monitor/requestHistory/list")}>查看请求追踪</Button>
        <Button variant="outline" onClick={() => navigate("/resource/hosts")}>查看 Host 目录</Button>
      </div>
    </MonCard>
  </section>;
};
