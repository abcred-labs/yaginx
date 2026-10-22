import { Button, DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from "@modra/react";
import { ChevronDown, Spinner } from "@/ui/icons";
import React, { useState } from "react";
import { WorkbenchPage } from "@/ui/workbench";
import { MonSegmented, eyebrowClass } from "@/ui/monParts";
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

  const handleRefreshMenuClick = (key: string) => {
    const seconds = Number(key);
    if (!isAutoRefreshInterval(seconds)) {
      return;
    }
    setRefreshMode(createAutoRefreshMode(seconds as AutoRefreshInterval));
    requestRefresh("auto");
  };

  return (
    <WorkbenchPage
      eyebrow="GATEWAY / CANONICAL TRAFFIC"
      title="网关流量概览"
      description="所有指标均由 Go 网关贡献表物化；窗口未积累完整时会明确显示为收集中。"
      extra={<>
        <MonSegmented options={windowOptions} value={range} onChange={setRange} ariaLabel="统计窗口" />
        <div className="inline-flex">
          <Button variant="outline" className="rounded-r-none" onClick={handlePrimaryRefresh}>
            {refreshMode.kind === "auto" && isAutoRefreshing
              ? <><Spinner className="size-4 animate-spin" aria-hidden="true" /> {refreshButtonText(refreshMode, isAutoRefreshing)}</>
              : refreshButtonText(refreshMode, isAutoRefreshing)}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="icon" className="-ml-px rounded-l-none" aria-label="自动刷新间隔">
                <ChevronDown className="size-4" aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuRadioGroup value={autoRefreshSeconds ? String(autoRefreshSeconds) : ""} onValueChange={handleRefreshMenuClick}>
                {autoRefreshIntervals.map((seconds) => (
                  <DropdownMenuRadioItem key={seconds} value={String(seconds)}>{`每 ${seconds} 秒自动刷新`}</DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </>}
    >
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-card px-5 py-4 text-foreground max-md:px-4">
        <div>
          <span className={`${eyebrowClass} inline-flex items-center gap-1.5 before:size-1.5 before:rounded-full before:bg-success before:content-['']`}>单一数据源</span>
          <h3 className="mb-1 mt-1 text-lg font-semibold tracking-tight">请求信号正在汇聚</h3>
          <p className="m-0 max-w-[720px] text-[13px] text-muted-foreground">
            当前窗口：{windowOptions.find((item) => item.value === range)?.label}{selectedDomain ? ` · 已聚焦 ${selectedDomain}` : " · 全部域名"}
          </p>
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
