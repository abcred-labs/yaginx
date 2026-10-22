import { getDatabasePerformance, type DatabaseHealthLevel, type IDatabasePerformance } from "@/api/analytics";
import { formatBytes, formatNumber } from "@/utils/formatters";
import { Badge, Button, DataTable, Statistic, Tooltip, TooltipContent, TooltipTrigger } from "@modra/react";
import type { ColumnDef, PaginationState } from "@tanstack/react-table";
import dayjs from "dayjs";
import React, { useEffect, useState } from "react";
import { usePublishPageMeta } from "@/layouts/PageMeta";
import { MonAlert, MonBar, MonCard, StatDot, cx, eyebrowClass, toneDotClass } from "@/ui/monParts";
import { CapabilityBar } from "./CapabilityBar";
import { MetricGrid } from "./MetricGrid";
import { TopSqlPanel } from "./TopSqlPanel";
import { formatCount, formatRate } from "./topSql";

const refreshIntervalMs = 15_000;
const deadTupleWarnRatio = 0.2;

const healthStatus: Record<DatabaseHealthLevel, { label: string; variant: "success" | "warning" | "destructive" }> = {
  healthy: { label: "健康", variant: "success" },
  warning: { label: "注意", variant: "warning" },
  critical: { label: "严重", variant: "destructive" }
};

const healthDot: Record<DatabaseHealthLevel, keyof typeof toneDotClass> = {
  healthy: "signal",
  warning: "warning",
  critical: "alert"
};

const formatTimestamp = (value: number) => value > 0 ? dayjs.unix(value).format("YYYY-MM-DD HH:mm:ss") : "暂无";

// 表维护时间只需要“多久没做过”这个量级，精确时刻留给 Tooltip。
const formatSince = (value: number) => {
  if (!value || value <= 0) {
    return "从未";
  }
  const minutes = Math.max(0, dayjs().diff(dayjs.unix(value), "minute"));
  if (minutes < 60) {
    return `${minutes} 分钟前`;
  }
  if (minutes < 60 * 24) {
    return `${Math.floor(minutes / 60)} 小时前`;
  }
  return `${Math.floor(minutes / (60 * 24))} 天前`;
};

const Section: React.FC<{ label: string; children: React.ReactNode }> = ({ label, children }) => (
  <section className="flex flex-col gap-2">
    <h2 className={cx("mx-0 mb-0 mt-2 flex items-center gap-2 after:h-px after:flex-1 after:bg-border after:content-['']", eyebrowClass)}>{label}</h2>
    {children}
  </section>
);

type TableRow = IDatabasePerformance["tables"][number];

const rightAligned = (title: string): ColumnDef<TableRow, unknown>["header"] => () => <div className="text-right">{title}</div>;

const tableColumns: ColumnDef<TableRow, unknown>[] = [
  { id: "name", header: "表", meta: { minWidth: 200 }, cell: ({ row }) => <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">{row.original.name}</code> },
  { id: "sizeBytes", header: rightAligned("容量"), meta: { minWidth: 100 }, cell: ({ row }) => <div className="text-right">{formatBytes(row.original.sizeBytes)}</div> },
  { id: "liveTuples", header: rightAligned("活元组"), meta: { minWidth: 100 }, cell: ({ row }) => <div className="text-right">{formatCount(row.original.liveTuples)}</div> },
  { id: "deadTuples", header: rightAligned("死元组"), meta: { minWidth: 100 }, cell: ({ row }) => <div className="text-right">{formatCount(row.original.deadTuples)}</div> },
  {
    id: "deadTupleRatio",
    header: "死元组比例",
    meta: { minWidth: 140 },
    cell: ({ row }) => {
      const value = row.original.deadTupleRatio;
      const warn = value >= deadTupleWarnRatio;
      return (
        <div className="flex items-center gap-2">
          <MonBar ratio={value} tone={warn ? "warning" : "flow"} />
          <b className={cx("font-mono text-xs font-medium", warn ? "text-warning" : "text-foreground")}>{formatRate(value)}</b>
        </div>
      );
    }
  },
  {
    id: "maintenance",
    header: "最近维护",
    meta: { minWidth: 140 },
    cell: ({ row }) => {
      const record = row.original;
      const latest = Math.max(record.lastAutovacuumAt, record.lastAutoanalyzeAt);
      return (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="cursor-help font-mono text-xs text-foreground underline decoration-dotted underline-offset-[3px]">{formatSince(latest)}</span>
          </TooltipTrigger>
          <TooltipContent>autovacuum：{formatTimestamp(record.lastAutovacuumAt)}<br />autoanalyze：{formatTimestamp(record.lastAutoanalyzeAt)}</TooltipContent>
        </Tooltip>
      );
    }
  }
];

const DatabasePerformance: React.FC = () => {
  // 顶栏面包屑由页面发布；不发布时会停留在上一个页面的标题。
  usePublishPageMeta({ trail: "SYSTEM / DATABASE", title: "数据库性能" });
  const [snapshot, setSnapshot] = useState<IDatabasePerformance>();
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState<string>();
  const [refreshToken, setRefreshToken] = useState(0);
  const [tablePagination, setTablePagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 8 });
  const refresh = () => setRefreshToken((value) => value + 1);

  useEffect(() => {
    const timer = window.setInterval(refresh, refreshIntervalMs);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setRefreshing(true);
    if (!snapshot) {
      setLoading(true);
    }
    getDatabasePerformance({ ignoreGlobalError: true, signal: controller.signal })
      .then((response) => {
        if (controller.signal.aborted) {
          return;
        }
        if (response.code !== 200 || !response.data) {
          setError("数据库性能数据加载失败，请重试。");
          setStale(true);
          return;
        }
        setSnapshot(response.data);
        setError(undefined);
        setStale(false);
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setError("数据库性能数据加载失败，请检查网络后重试。");
          setStale(true);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
          setRefreshing(false);
        }
      });
    return () => controller.abort();
  }, [refreshToken]);

  const level = snapshot?.health.level ?? "healthy";
  const health = healthStatus[level];
  const poolUsage = snapshot?.pool.usageRate ?? 0;
  const ioAvailable = snapshot?.io.available ?? false;
  const tables = snapshot?.tables ?? [];

  // 数值卡与概览页同构：标题前小圆点表状态。
  const StatTitle: React.FC<{ tone: keyof typeof toneDotClass; children: React.ReactNode }> = ({ tone, children }) => (
    <span className="flex items-center gap-1.5"><StatDot tone={tone} />{children}</span>
  );
  const noteClass = "text-sm text-muted-foreground";

  return (
    <div className="flex flex-col gap-3">
      <header className="flex flex-col items-start justify-between gap-4 md:flex-row md:items-end">
        <div>
          <h3 className="m-0 text-xl font-semibold">数据库性能</h3>
          <span className={noteClass}>每 15 秒刷新，只读取当前 Yaginx 节点连接池与 PostgreSQL 统计视图。</span>
        </div>
        <div className="flex w-full items-center justify-between gap-3 md:w-auto md:justify-start">
          {snapshot ? <span className={cx("text-sm", stale ? "text-warning" : "text-muted-foreground")}>{stale ? "数据已过期" : `更新于 ${formatTimestamp(snapshot.asOf)}`}</span> : null}
          <Button variant="outline" onClick={refresh} loading={refreshing}>立即刷新</Button>
        </div>
      </header>

      <CapabilityBar capabilities={snapshot?.capabilities} />

      {error ? <MonAlert type="error" message={error} action={<Button size="sm" variant="outline" onClick={refresh}>重试</Button>} /> : null}

      <Section label="概览">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <MonCard loading={loading} className="h-full">
            <Statistic title={<StatTitle tone={healthDot[level]}>数据库健康</StatTitle>} value={health.label} />
            <div className="mt-2"><Badge variant={health.variant}>{snapshot?.health.reasons.join("；") || "未发现阈值告警"}</Badge></div>
          </MonCard>
          <MonCard loading={loading} className="h-full">
            <Statistic title={<StatTitle tone={poolUsage >= 0.8 ? "warning" : "signal"}>连接池使用率</StatTitle>} value={formatRate(poolUsage)} />
            <div className="my-2 flex"><MonBar ratio={poolUsage} /></div>
            <span className={noteClass}>已借出 {formatNumber(snapshot?.pool.acquired)} / 上限 {formatNumber(snapshot?.pool.max)}</span>
          </MonCard>
          <MonCard loading={loading} className="h-full">
            <Statistic title={<StatTitle tone={(snapshot?.activity.lockWaits ?? 0) > 0 ? "warning" : "signal"}>锁等待</StatTitle>} value={formatNumber(snapshot?.activity.lockWaits)} />
            <span className={noteClass}>最长 {formatNumber(snapshot?.activity.maxLockWaitSeconds)} 秒</span>
          </MonCard>
          <MonCard loading={loading} className="h-full">
            <Statistic title={<StatTitle tone="signal">缓存命中率</StatTitle>} value={formatRate(snapshot?.database.cacheHitRate)} />
            <span className={noteClass}>统计重置：{formatTimestamp(snapshot?.database.statsResetAt ?? 0)}</span>
          </MonCard>
        </div>
      </Section>

      <Section label="运行态">
        <div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
          <MonCard title="连接与会话" loading={loading} className="h-full">
            <MetricGrid
              items={[
                { label: "数据库连接", value: formatNumber(snapshot?.database.connections) },
                { label: "活跃会话", value: formatNumber(snapshot?.activity.active) },
                { label: "空闲会话", value: formatNumber(snapshot?.activity.idle) },
                { label: "事务中空闲", value: formatNumber(snapshot?.activity.idleInTransaction), tone: (snapshot?.activity.idleInTransaction ?? 0) > 0 ? "warning" : "default", hint: "长时间处于事务中的空闲会话会阻塞 autovacuum。" },
                { label: "未授予锁", value: formatNumber(snapshot?.activity.ungrantedLocks), tone: (snapshot?.activity.ungrantedLocks ?? 0) > 0 ? "warning" : "default" },
                { label: "连接池空闲", value: formatNumber(snapshot?.pool.idle) }
              ]}
            />
          </MonCard>
          <MonCard title="I/O、缓存与临时文件" loading={loading} className="h-full">
            <MetricGrid
              items={[
                { label: "缓存命中", value: formatRate(snapshot?.database.cacheHitRate) },
                { label: "临时文件", value: formatNumber(snapshot?.database.tempFiles), tone: (snapshot?.database.tempFiles ?? 0) > 0 ? "warning" : "default" },
                { label: "临时文件容量", value: formatBytes(snapshot?.database.tempBytes) },
                { label: "死锁", value: formatNumber(snapshot?.database.deadlocks), tone: (snapshot?.database.deadlocks ?? 0) > 0 ? "alert" : "default" }
              ]}
            />
            {ioAvailable ? (
              <MetricGrid
                className="mt-3 border-t border-dashed border-border pt-3"
                items={[
                  { label: "I/O 读取", value: formatCount(snapshot?.io.reads) },
                  { label: "I/O 写入", value: formatCount(snapshot?.io.writes) },
                  { label: "读取耗时", value: `${formatNumber(snapshot?.io.readMs)} ms` },
                  { label: "写入耗时", value: `${formatNumber(snapshot?.io.writeMs)} ms` }
                ]}
              />
            ) : (
              <span className="mt-3 block border-t border-dashed border-border pt-3 text-xs text-muted-foreground">
                pg_stat_io 不可用，无法拆分读写次数与 I/O 耗时。
              </span>
            )}
          </MonCard>
        </div>
      </Section>

      <Section label="存储">
        <MonCard title="表维护与容量" loading={loading}>
          <DataTable
            columns={tableColumns}
            data={tables}
            getRowId={(record) => record.name}
            pagination={tablePagination}
            onPaginationChange={setTablePagination}
            showPagination={tables.length > tablePagination.pageSize}
            pageSizeOptions={[8]}
          />
        </MonCard>
      </Section>

      <Section label="查询">
        <TopSqlPanel rows={snapshot?.topSql ?? []} loading={loading} capability={snapshot?.capabilities.pgStatStatements} />
      </Section>
    </div>
  );
};

export default DatabasePerformance;
