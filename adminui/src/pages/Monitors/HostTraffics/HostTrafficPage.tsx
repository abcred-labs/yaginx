import { Badge, Button, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Switch } from "@modra/react";
import dayjs, { type Dayjs } from "dayjs";
import { Search } from "lucide-react";
import React, { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { DataToolbar, WorkbenchPage } from "@/ui/workbench";
import { MonDateTimeRange, MonSegmented } from "@/ui/monParts";
import { HostTrafficFilters, hostTrafficRangePresets, rangeFromPicker, trafficGrainLabel } from "./shared";

export const navigationItems = [
  { path: "/monitor/traffic/request-trend", label: "趋势" },
  { path: "/monitor/traffic/request-contribution", label: "贡献" },
  { path: "/monitor/traffic/bandwidth", label: "带宽" },
  { path: "/monitor/traffic/list", label: "明细" }
];

const autoRefreshOptions = [
  { value: 0, label: "不自动刷新" },
  { value: 10, label: "10 秒" },
  { value: 30, label: "30 秒" },
  { value: 60, label: "60 秒" }
];

interface HostTrafficPageProps {
  title: string;
  description: string;
  filters: HostTrafficFilters;
  onFiltersChange: (filters: HostTrafficFilters) => void;
  onRefresh: () => void;
  autoRefreshSeconds: number;
  autoRefreshEnabled: boolean;
  onAutoRefreshSecondsChange: (value: number) => void;
  onAutoRefreshEnabledChange: (value: boolean) => void;
  grain?: "minute" | "hour" | "day";
  coverageState?: "collecting" | "complete" | "incomplete";
  children: React.ReactNode;
}

export const HostTrafficPage: React.FC<HostTrafficPageProps> = ({
  title,
  description,
  filters,
  onFiltersChange,
  onRefresh,
  autoRefreshSeconds,
  autoRefreshEnabled,
  onAutoRefreshSecondsChange,
  onAutoRefreshEnabledChange,
  grain,
  coverageState,
  children
}) => {
  const location = useLocation();
  const navigate = useNavigate();
  const [hostInput, setHostInput] = useState(filters.hostName);
  const range: [Dayjs, Dayjs] = [
    dayjs.unix(filters.from),
    dayjs.unix(filters.to)
  ];
  const periodEnd = dayjs().startOf("minute");
  const rangePresets = hostTrafficRangePresets.map((preset) => ({
    label: preset.label,
    value: [periodEnd.subtract(preset.minutes - 1, "minute"), periodEnd] as [Dayjs, Dayjs]
  }));

  useEffect(() => setHostInput(filters.hostName), [filters.hostName]);

  const applyHost = () => onFiltersChange({ ...filters, hostName: hostInput.trim() });
  const updateRange = (value: [Dayjs | null, Dayjs | null] | null) => {
    if (!value) {
      return;
    }
    const nextRange = rangeFromPicker(value);
    if (nextRange) {
      onFiltersChange({ ...filters, ...nextRange });
    }
  };

  return (
    <WorkbenchPage
      eyebrow="ANALYTICS / HOST TRAFFIC"
      title={title}
      description={description}
      extra={<Button variant="outline" onClick={onRefresh}>刷新数据</Button>}
      className="!gap-2"
    >
      <div className="max-w-[620px] max-md:w-full max-md:overflow-x-auto">
        <MonSegmented
          block
          ariaLabel="流量分析导航"
          value={navigationItems.find((item) => location.pathname.endsWith(item.path.split("/").pop() || ""))?.path ?? location.pathname}
          options={navigationItems.map((item) => ({ label: item.label, value: item.path }))}
          onChange={(value) => navigate({ pathname: String(value), search: location.search })}
        />
      </div>
      <DataToolbar className="rounded-md border border-border bg-card p-2">
        <div className="flex items-center gap-1">
          <Input
            placeholder="Host（精确匹配）"
            aria-label="Host"
            value={hostInput}
            onChange={(event) => setHostInput(event.target.value)}
            onKeyDown={(event) => { if (event.key === "Enter") applyHost(); }}
            className="w-[220px]"
          />
          <Button variant="outline" size="icon" aria-label="搜索 Host" onClick={applyHost}><Search className="size-4" aria-hidden="true" /></Button>
        </div>
        <MonDateTimeRange
          ariaLabel="统计时间范围"
          clearable={false}
          className="w-[308px] max-w-[calc(100vw-56px)]"
          presets={rangePresets}
          value={range}
          onChange={updateRange}
        />
        <div className="flex items-center gap-1">
          <Select value={String(autoRefreshSeconds)} onValueChange={(value) => onAutoRefreshSecondsChange(Number(value))}>
            <SelectTrigger aria-label="自动刷新间隔" className="w-[116px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {autoRefreshOptions.map((option) => <SelectItem key={option.value} value={String(option.value)}>{option.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <Switch aria-label="启用自动刷新" checked={autoRefreshEnabled} onCheckedChange={onAutoRefreshEnabledChange} />
        </div>
        {grain ? <div className="flex items-center gap-1 text-muted-foreground">
          <Badge variant="info">按{trafficGrainLabel(grain)}聚合</Badge>
          {coverageState === "collecting" ? <Badge variant="warning">当前桶实时汇总中</Badge> : null}
          {coverageState === "incomplete" ? <Badge variant="warning">数据覆盖不完整</Badge> : null}
        </div> : null}
      </DataToolbar>
      {children}
    </WorkbenchPage>
  );
};
