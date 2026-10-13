import { formatNumber } from "@/utils/formatters";
import { Drawer, Tag, Typography } from "antd";
import React from "react";
import { MetricGrid } from "./MetricGrid";
import { formatMs, formatRate, sqlStatementKind, type TopSqlRow } from "./topSql";

export const TopSqlDetailDrawer: React.FC<{
  row: TopSqlRow | null;
  share: number;
  onClose: () => void;
}> = ({ row, share, onClose }) => (
  <Drawer
    open={Boolean(row)}
    onClose={onClose}
    width={Math.min(760, typeof window === "undefined" ? 760 : window.innerWidth)}
    destroyOnClose
    className="top-sql-drawer"
    title={
      <div className="top-sql-drawer__title">
        <Tag color="processing">{sqlStatementKind(row?.query)}</Tag>
        <Typography.Text code copyable={{ text: row?.queryId || "" }}>{row?.queryId || "未生成"}</Typography.Text>
      </div>
    }
  >
    {row ? (
      <>
        <MetricGrid
          items={[
            { label: "调用", value: formatNumber(row.calls) },
            { label: "总执行", value: formatMs(row.totalExecMs) },
            { label: "平均执行", value: formatMs(row.meanExecMs) },
            { label: "耗时占比", value: formatRate(share) }
          ]}
        />
        <section className="top-sql-drawer__section">
          <h4 className="signal-eyebrow">完整归一化 SQL</h4>
          <Typography.Paragraph className="top-sql-drawer__sql" copyable={{ text: row.query }}>
            {row.query}
          </Typography.Paragraph>
        </section>
        <section className="top-sql-drawer__section">
          <h4 className="signal-eyebrow">规划与执行</h4>
          <MetricGrid
            items={[
              { label: "总规划", value: formatMs(row.totalPlanMs) },
              { label: "平均规划", value: formatMs(row.meanPlanMs) },
              { label: "总执行", value: formatMs(row.totalExecMs) },
              { label: "平均执行", value: formatMs(row.meanExecMs) }
            ]}
          />
        </section>
        <section className="top-sql-drawer__section">
          <h4 className="signal-eyebrow">块读写</h4>
          <MetricGrid
            items={[
              { label: "共享块读取", value: formatNumber(row.sharedBlocksRead) },
              { label: "共享块命中", value: formatNumber(row.sharedBlocksHit) },
              { label: "临时块读取", value: formatNumber(row.tempBlocksRead), tone: row.tempBlocksRead > 0 ? "warning" : "default" },
              { label: "临时块写入", value: formatNumber(row.tempBlocksWritten), tone: row.tempBlocksWritten > 0 ? "warning" : "default" }
            ]}
          />
        </section>
      </>
    ) : null}
  </Drawer>
);

export default TopSqlDetailDrawer;
