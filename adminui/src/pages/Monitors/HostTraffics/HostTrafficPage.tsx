import { Button, DatePicker, Input, Segmented, Select, Space, Switch, Tag } from "antd";
import dayjs, { type Dayjs } from "dayjs";
import React, { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { DataToolbar, WorkbenchPage } from "@/componets/Workbench";
import { HostTrafficFilters, hostTrafficRangePresets, rangeFromPicker, trafficGrainLabel } from "./shared";
import { SPACE } from '@/utils/spacing';
import "./hostTraffic.less";

export const navigationItems = [
  { path: "/monitor/traffic/request-trend", label: "趋势" },
  { path: "/monitor/traffic/request-contribution", label: "贡献" },
  { path: "/monitor/traffic/bandwidth", label: "带宽" },
  { path: "/monitor/traffic/list", label: "明细" }
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
      extra={<Button onClick={onRefresh}>刷新数据</Button>}
      className="host-traffic-page"
    >
      <Segmented
        className="host-traffic-page__nav"
        block
        value={navigationItems.find((item) => location.pathname.endsWith(item.path.split("/").pop() || ""))?.path ?? location.pathname}
        options={navigationItems.map((item) => ({ label: item.label, value: item.path }))}
        onChange={(value) => navigate({ pathname: String(value), search: location.search })}
      />
      <DataToolbar className="host-traffic-page__toolbar">
        <Input.Search
          placeholder="Host（精确匹配）"
          value={hostInput}
          onChange={(event) => setHostInput(event.target.value)}
          onSearch={applyHost}
          style={{ width: 220 }}
        />
        <DatePicker.RangePicker
          presets={rangePresets}
          showTime={{ format: "HH:mm" }}
          format="YYYY-MM-DD HH:mm"
          value={range}
          onChange={(value) => updateRange(value as [Dayjs | null, Dayjs | null] | null)}
          style={{ width: 308 }}
        />
        <Space size={SPACE.space1}>
          <Select
            aria-label="自动刷新间隔"
            style={{ width: 116 }}
            value={autoRefreshSeconds}
            onChange={onAutoRefreshSecondsChange}
            options={[
              { value: 0, label: "不自动刷新" },
              { value: 10, label: "10 秒" },
              { value: 30, label: "30 秒" },
              { value: 60, label: "60 秒" }
            ]}
          />
          <Switch checked={autoRefreshEnabled} onChange={onAutoRefreshEnabledChange} />
        </Space>
        {grain ? <Space size={SPACE.space1} className="host-traffic-page__status">
          <Tag color="blue">按{trafficGrainLabel(grain)}聚合</Tag>
          {coverageState === "collecting" ? <Tag color="gold">当前桶实时汇总中</Tag> : null}
          {coverageState === "incomplete" ? <Tag color="gold">数据覆盖不完整</Tag> : null}
        </Space> : null}
      </DataToolbar>
      {children}
    </WorkbenchPage>
  );
};
