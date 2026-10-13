import { Dropdown, Segmented, Space, Typography } from "antd";
import { LoadingOutlined } from "@ant-design/icons";
import type { MenuProps } from "antd";
import React, { useState } from "react";
import { WorkbenchPage } from "@/componets/Workbench";
import { AnalyticsDashboard } from "./AnalyticsDashboard";
import {
  AutoRefreshInterval,
  RefreshMode,
  RefreshRequestKind,
  autoRefreshIntervals,
  createAutoRefreshMode,
  createManualRefreshMode,
  isAutoRefreshInterval,
  refreshButtonText
} from "./refreshMode";
import "./index.less";

const windowOptions = [
  { label: "15 分钟", value: 15 * 60 },
  { label: "1 小时", value: 60 * 60 },
  { label: "24 小时", value: 24 * 60 * 60 },
  { label: "7 天", value: 7 * 24 * 60 * 60 },
  { label: "30 天", value: 30 * 24 * 60 * 60 }
];

const Home: React.FC = () => {
  const [range, setRange] = useState(60 * 60);
  const [selectedDomain, setSelectedDomain] = useState<string>();
  const [refreshRequest, setRefreshRequest] = useState({ key: 0, kind: "manual" as RefreshRequestKind });
  const [refreshMode, setRefreshMode] = useState<RefreshMode>(createManualRefreshMode);
  const [isAutoRefreshing, setIsAutoRefreshing] = useState(false);
  const autoRefreshSeconds = refreshMode.kind === "auto" ? refreshMode.seconds : undefined;

  const requestRefresh = (kind: RefreshRequestKind) => {
    setRefreshRequest((current) => ({ key: current.key + 1, kind }));
  };

  const handlePrimaryRefresh = () => {
    setRefreshMode(createManualRefreshMode());
    setIsAutoRefreshing(false);
    requestRefresh("manual");
  };

  const handleRefreshMenuClick: MenuProps["onClick"] = ({ key }) => {
    const seconds = Number(key);
    if (!isAutoRefreshInterval(seconds)) {
      return;
    }
    setRefreshMode(createAutoRefreshMode(seconds as AutoRefreshInterval));
    requestRefresh("auto");
  };

  const refreshMenu: MenuProps = {
    items: autoRefreshIntervals.map((seconds) => ({ key: String(seconds), label: `每 ${seconds} 秒自动刷新` })),
    onClick: handleRefreshMenuClick,
    selectable: true,
    selectedKeys: autoRefreshSeconds ? [String(autoRefreshSeconds)] : []
  };

  return (
    <WorkbenchPage
      className="dashboard-page"
      eyebrow="GATEWAY / CANONICAL TRAFFIC"
      title="网关流量概览"
      description="所有指标均由 Go 网关贡献表物化；窗口未积累完整时会明确显示为收集中。"
      extra={<Space wrap>
        <Segmented options={windowOptions} value={range} onChange={(value) => setRange(Number(value))} />
        <Dropdown.Button menu={refreshMenu} onClick={handlePrimaryRefresh}>
          {refreshMode.kind === "auto" && isAutoRefreshing
            ? <><LoadingOutlined spin /> {refreshButtonText(refreshMode, isAutoRefreshing)}</>
            : refreshButtonText(refreshMode, isAutoRefreshing)}
        </Dropdown.Button>
      </Space>}
    >
      <div className="dashboard-hero">
        <div>
          <span className="dashboard-hero__eyebrow">单一数据源</span>
          <Typography.Title level={3} className="dashboard-hero__title">请求信号正在汇聚</Typography.Title>
          <Typography.Paragraph className="dashboard-hero__desc">
            当前窗口：{windowOptions.find((item) => item.value === range)?.label}{selectedDomain ? ` · 已聚焦 ${selectedDomain}` : " · 全部域名"}
          </Typography.Paragraph>
        </div>
      </div>
      <AnalyticsDashboard
        range={range}
        refreshKey={refreshRequest.key}
        refreshKind={refreshRequest.kind}
        autoRefreshSeconds={autoRefreshSeconds}
        selectedDomain={selectedDomain}
        onSelectedDomainChange={setSelectedDomain}
        onAutoRefreshLoadingChange={setIsAutoRefreshing}
      />
    </WorkbenchPage>
  );
};

export default Home;
