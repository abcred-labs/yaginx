import { Button, Card, Col, Row, Space, Tag, Typography } from "antd";
import React, { useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { SPACE } from "@/utils/spacing";
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
  return <section className="dashboard-panels">
    <TrafficKpiPanel range={range} selectedDomain={selectedDomain} refreshKey={refreshKey} autoRefreshSeconds={autoRefreshSeconds} onLoadingChange={kpiLoading} />
    <Row gutter={[SPACE.space3, SPACE.space3]}>
      <Col xs={24} xl={16}><TrafficTrendPanel range={range} selectedDomain={selectedDomain} refreshKey={refreshKey} autoRefreshSeconds={autoRefreshSeconds} onLoadingChange={trendLoading} /></Col>
      <Col xs={24} xl={8}><Space direction="vertical" size={SPACE.space3} style={{ width: "100%" }}><GatewayHealthPanel /><DatabaseHealthPanel /></Space></Col>
    </Row>
    <TrafficBreakdownPanel range={range} selectedDomain={selectedDomain} refreshKey={refreshKey} autoRefreshSeconds={autoRefreshSeconds} onLoadingChange={breakdownLoading} />
    <Card title="运行状态与下一步">
      <Space wrap><Tag color="green">各面板独立刷新</Tag><Typography.Text type="secondary">基础设施每 15 秒更新，流量面板跟随当前窗口刷新。</Typography.Text><Button type="primary" onClick={() => navigate("/monitor/traffic/request-trend")}>打开流量分析</Button><Button onClick={() => navigate("/monitor/requestHistory/list")}>查看请求追踪</Button><Button onClick={() => navigate("/resource/hosts")}>探索 Host 资源</Button></Space>
    </Card>
  </section>;
};
