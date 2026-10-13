import { getDatabasePerformance, type DatabaseHealthLevel, type IDatabasePerformance } from "@/api/analytics";
import { formatBytes, formatNumber } from "@/utils/formatters";
import { SPACE } from "@/utils/spacing";
import { Alert, Badge, Button, Card, Col, Row, Statistic, Table, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import React, { useEffect, useState } from "react";
import { CapabilityBar } from "./CapabilityBar";
import { MetricGrid } from "./MetricGrid";
import { TopSqlPanel } from "./TopSqlPanel";
import { formatCount, formatRate } from "./topSql";
import "./databasePerformance.less";

const refreshIntervalMs = 15_000;
const deadTupleWarnRatio = 0.2;

const healthStatus: Record<DatabaseHealthLevel, { label: string; status: "success" | "warning" | "error" }> = {
  healthy: { label: "健康", status: "success" },
  warning: { label: "注意", status: "warning" },
  critical: { label: "严重", status: "error" }
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
  <section className="database-performance__section">
    <h2 className="signal-eyebrow database-performance__section-label">{label}</h2>
    {children}
  </section>
);

const DatabasePerformance: React.FC = () => {
  const [snapshot, setSnapshot] = useState<IDatabasePerformance>();
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState<string>();
  const [refreshToken, setRefreshToken] = useState(0);
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

  const tableColumns: ColumnsType<IDatabasePerformance["tables"][number]> = [
    { title: "表", dataIndex: "name", width: 200, render: (value) => <Typography.Text code>{value}</Typography.Text> },
    { title: "容量", dataIndex: "sizeBytes", width: 100, align: "right", render: formatBytes },
    { title: "活元组", dataIndex: "liveTuples", width: 100, align: "right", render: formatCount },
    { title: "死元组", dataIndex: "deadTuples", width: 100, align: "right", render: formatCount },
    {
      title: "死元组比例",
      dataIndex: "deadTupleRatio",
      width: 140,
      render: (value: number) => (
        <div className={`ratio-bar${value >= deadTupleWarnRatio ? " ratio-bar--warning" : ""}`}>
          <span className="ratio-bar__track"><i style={{ width: `${Math.min(100, value * 100)}%` }} /></span>
          <b>{formatRate(value)}</b>
        </div>
      )
    },
    {
      title: "最近维护",
      key: "maintenance",
      width: 140,
      render: (_, record) => {
        const latest = Math.max(record.lastAutovacuumAt, record.lastAutoanalyzeAt);
        return (
          <Tooltip title={<>autovacuum：{formatTimestamp(record.lastAutovacuumAt)}<br />autoanalyze：{formatTimestamp(record.lastAutoanalyzeAt)}</>}>
            <span className="database-performance__since">{formatSince(latest)}</span>
          </Tooltip>
        );
      }
    }
  ];

  const level = snapshot?.health.level ?? "healthy";
  const health = healthStatus[level];
  const poolUsage = snapshot?.pool.usageRate ?? 0;
  const ioAvailable = snapshot?.io.available ?? false;

  return (
    <div className="database-performance">
      <header className="database-performance__header">
        <div>
          <Typography.Title level={3}>数据库性能</Typography.Title>
          <Typography.Text type="secondary">每 15 秒刷新，只读取当前 Yaginx 节点连接池与 PostgreSQL 统计视图。</Typography.Text>
        </div>
        <div className="database-performance__actions">
          {snapshot ? <Typography.Text type={stale ? "warning" : "secondary"}>{stale ? "数据已过期" : `更新于 ${formatTimestamp(snapshot.asOf)}`}</Typography.Text> : null}
          <Button onClick={refresh} loading={refreshing}>立即刷新</Button>
        </div>
      </header>

      <CapabilityBar capabilities={snapshot?.capabilities} />

      {error ? <Alert type="error" showIcon message={error} action={<Button size="small" onClick={refresh}>重试</Button>} /> : null}

      <Section label="概览">
        <Row gutter={[SPACE.space3, SPACE.space3]}>
          <Col xs={24} sm={12} xl={6}>
            <Card loading={loading} className={`database-performance__card database-performance__card--${level}`}>
              <Statistic title="数据库健康" value={health.label} />
              <Badge status={health.status} text={snapshot?.health.reasons.join("；") || "未发现阈值告警"} />
            </Card>
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <Card loading={loading} className={`database-performance__card${poolUsage >= 0.8 ? " database-performance__card--warning" : ""}`}>
              <Statistic title="连接池使用率" value={formatRate(poolUsage)} />
              <span className="ratio-bar ratio-bar--wide"><span className="ratio-bar__track"><i style={{ width: `${Math.min(100, poolUsage * 100)}%` }} /></span></span>
              <Typography.Text type="secondary">已借出 {formatNumber(snapshot?.pool.acquired)} / 上限 {formatNumber(snapshot?.pool.max)}</Typography.Text>
            </Card>
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <Card loading={loading} className={`database-performance__card${(snapshot?.activity.lockWaits ?? 0) > 0 ? " database-performance__card--warning" : ""}`}>
              <Statistic title="锁等待" value={formatNumber(snapshot?.activity.lockWaits)} />
              <Typography.Text type="secondary">最长 {formatNumber(snapshot?.activity.maxLockWaitSeconds)} 秒</Typography.Text>
            </Card>
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <Card loading={loading} className="database-performance__card">
              <Statistic title="缓存命中率" value={formatRate(snapshot?.database.cacheHitRate)} />
              <Typography.Text type="secondary">统计重置：{formatTimestamp(snapshot?.database.statsResetAt ?? 0)}</Typography.Text>
            </Card>
          </Col>
        </Row>
      </Section>

      <Section label="运行态">
        <Row gutter={[SPACE.space3, SPACE.space3]}>
          <Col xs={24} xl={12}>
            <Card title="连接与会话" loading={loading} className="database-performance__panel">
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
            </Card>
          </Col>
          <Col xs={24} xl={12}>
            <Card title="I/O、缓存与临时文件" loading={loading} className="database-performance__panel">
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
                  items={[
                    { label: "I/O 读取", value: formatCount(snapshot?.io.reads) },
                    { label: "I/O 写入", value: formatCount(snapshot?.io.writes) },
                    { label: "读取耗时", value: `${formatNumber(snapshot?.io.readMs)} ms` },
                    { label: "写入耗时", value: `${formatNumber(snapshot?.io.writeMs)} ms` }
                  ]}
                />
              ) : (
                <Typography.Text type="secondary" className="database-performance__panel-note">
                  pg_stat_io 不可用，无法拆分读写次数与 I/O 耗时。
                </Typography.Text>
              )}
            </Card>
          </Col>
        </Row>
      </Section>

      <Section label="存储">
        <Card title="表维护与容量" loading={loading}>
          <Table rowKey="name" size="small" columns={tableColumns} dataSource={snapshot?.tables ?? []} pagination={{ pageSize: 8, size: "small", hideOnSinglePage: true, showSizeChanger: false }} scroll={{ x: 760 }} />
        </Card>
      </Section>

      <Section label="查询">
        <TopSqlPanel rows={snapshot?.topSql ?? []} loading={loading} capability={snapshot?.capabilities.pgStatStatements} />
      </Section>
    </div>
  );
};

export default DatabasePerformance;
