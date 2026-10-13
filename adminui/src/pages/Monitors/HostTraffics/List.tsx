import { getAnalyticsDetails, type IAnalyticsDetails } from "@/api/analytics";
import { DEFAULT_PAGE_INDEX, DEFAULT_PAGE_SIZE, PAGE_SIZE_OPTIONS } from "@/api/apiTypes";
import { LocalTime } from "@/componets/LocalTime";
import { SectionCard } from "@/componets/Workbench";
import { formatBytes, formatNumber, formatPercent } from "@/utils/formatters";
import { Button, Table, Tag } from "antd";
import type { ColumnsType, TablePaginationConfig } from "antd/es/table";
import React, { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { HostTrafficPage } from "./HostTrafficPage";
import { HostTrafficKpiCards, useHostTrafficFilters, useHostTrafficRefresh, useHostTrafficWorkspace } from "./dashboard";
import { trafficGrainLabel } from "./shared";

const List: React.FC = () => {
  const navigate = useNavigate();
  const { filters, setFilters } = useHostTrafficFilters();
  const { autoRefreshSeconds, autoRefreshEnabled, setAutoRefreshSeconds, setAutoRefreshEnabled } = useHostTrafficWorkspace();
  const { refreshToken, refresh } = useHostTrafficRefresh(autoRefreshEnabled, autoRefreshSeconds);
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState<IAnalyticsDetails>();
  const [pagination, setPagination] = useState({ pageIndex: DEFAULT_PAGE_INDEX, pageSize: DEFAULT_PAGE_SIZE });

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getAnalyticsDetails({
      from: filters.from,
      to: filters.to,
      domain: filters.hostName || undefined,
      pageIndex: pagination.pageIndex,
      pageSize: pagination.pageSize
    })
      .then((response) => {
        if (!cancelled) setData(response.data);
      })
      .catch(() => {
        if (!cancelled) setData(undefined);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [filters, pagination, refreshToken]);

  useEffect(() => {
    setPagination((current) => current.pageIndex === 1 ? current : { ...current, pageIndex: 1 });
  }, [filters.from, filters.to, filters.hostName]);

  const columns: ColumnsType<IAnalyticsDetails["records"][number]> = [
    {
      title: "Host",
      dataIndex: "hostName",
      key: "hostName",
      width: 250,
      render: (value: string) => <Button type="link" style={{ padding: 0 }} onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(value)}`)}>{value}</Button>
    },
    {
      title: "统计时间",
      dataIndex: "period",
      key: "period",
      width: 170,
      render: (value: number) => <LocalTime timestamp={value} format={data?.grain === "day" ? "YYYY-MM-DD" : data?.grain === "hour" ? "YYYY-MM-DD HH:00" : "YYYY-MM-DD HH:mm"} />
    },
    { title: "请求次数", dataIndex: "requestCounts", align: "right", render: formatNumber },
    { title: "错误", dataIndex: "errorRequests", align: "right", render: formatNumber },
    { title: "错误率", dataIndex: "errorRate", align: "right", render: formatPercent },
    { title: "流入", dataIndex: "inboundBytes", align: "right", render: formatBytes },
    { title: "流出", dataIndex: "outboundBytes", align: "right", render: formatBytes },
    { title: "总流量", dataIndex: "totalBytes", align: "right", render: formatBytes },
    { title: "平均延迟", dataIndex: "avgDurationMs", align: "right", render: (value: number) => `${formatNumber(value)} ms` },
    { title: "P95", dataIndex: "p95Ms", align: "right", render: (value: number | null) => value == null ? "不可用" : `${formatNumber(value)} ms` },
    { title: "状态", dataIndex: "isPartial", width: 108, render: (value: boolean) => value ? <Tag color="gold">实时汇总中</Tag> : <Tag color="green">完整</Tag> }
  ];
  const page: TablePaginationConfig = {
    current: data?.paging.pageIndex ?? pagination.pageIndex,
    pageSize: data?.paging.pageSize ?? pagination.pageSize,
    total: data?.paging.total ?? 0,
    showSizeChanger: true,
    pageSizeOptions: [...PAGE_SIZE_OPTIONS]
  };
  const totalRequests = (data?.records ?? []).reduce((sum, item) => sum + item.requestCounts, 0);
  const totalBytes = (data?.records ?? []).reduce((sum, item) => sum + item.totalBytes, 0);

  return (
    <HostTrafficPage
      title="流量明细"
      description="按 Host 与自适应时间桶查看请求、流量、错误和延迟汇总。"
      filters={filters}
      onFiltersChange={setFilters}
      onRefresh={refresh}
      autoRefreshSeconds={autoRefreshSeconds}
      autoRefreshEnabled={autoRefreshEnabled}
      onAutoRefreshSecondsChange={setAutoRefreshSeconds}
      onAutoRefreshEnabledChange={setAutoRefreshEnabled}
      grain={data?.grain}
      coverageState={data?.coverage.state}
    >
      <HostTrafficKpiCards
        loading={loading}
        items={[
          { title: "当前页请求", value: formatNumber(totalRequests), detail: "当前筛选页内汇总", tone: "flow" },
          { title: "当前页流量", value: formatBytes(totalBytes), detail: "流入与流出之和", tone: "signal" },
          { title: "明细记录", value: formatNumber(data?.paging.total), detail: `按${trafficGrainLabel(data?.grain)}和 Host 分组`, tone: "warning" },
          { title: "当前桶", value: (data?.records ?? []).some((item) => item.isPartial) ? "实时汇总中" : "已完成", detail: "未完成桶会持续更新", tone: "alert" }
        ]}
      />
      <SectionCard title="Host 流量明细" description={`每条记录对应一个 Host 在一个${trafficGrainLabel(data?.grain)}统计周期内的聚合数据。`}>
        <Table
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={data?.records ?? []}
          pagination={page}
          scroll={{ x: 1460 }}
          size="small"
          onChange={(next) => setPagination({ pageIndex: next.current ?? DEFAULT_PAGE_INDEX, pageSize: next.pageSize ?? DEFAULT_PAGE_SIZE })}
        />
      </SectionCard>
    </HostTrafficPage>
  );
};

export default List;
