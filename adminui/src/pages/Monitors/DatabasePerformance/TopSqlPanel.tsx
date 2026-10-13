import type { IDatabaseCapability } from "@/api/analytics";
import { Card, Empty, Input, Segmented, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import React, { useMemo, useState } from "react";
import { TopSqlDetailDrawer } from "./TopSqlDetailDrawer";
import {
  execShare,
  filterTopSql,
  formatCount,
  formatMs,
  formatRate,
  sortTopSql,
  sqlStatementKind,
  summarizeSql,
  type TopSqlRow,
  type TopSqlSortKey
} from "./topSql";

const sortOptions: Array<{ label: string; value: TopSqlSortKey }> = [
  { label: "总执行", value: "totalExecMs" },
  { label: "平均执行", value: "meanExecMs" },
  { label: "调用次数", value: "calls" }
];

export const TopSqlPanel: React.FC<{
  rows: TopSqlRow[];
  loading: boolean;
  capability?: IDatabaseCapability;
}> = ({ rows, loading, capability }) => {
  const [sortKey, setSortKey] = useState<TopSqlSortKey>("totalExecMs");
  const [keyword, setKeyword] = useState("");
  const [detail, setDetail] = useState<TopSqlRow | null>(null);

  const visibleRows = useMemo(() => sortTopSql(filterTopSql(rows, keyword), sortKey), [rows, keyword, sortKey]);

  const columns: ColumnsType<TopSqlRow> = [
    {
      title: "SQL",
      dataIndex: "query",
      render: (value: string, record) => (
        <div className="top-sql__summary">
          <div className="top-sql__summary-meta">
            <Tag bordered={false}>{sqlStatementKind(value)}</Tag>
            <Typography.Text type="secondary" className="top-sql__query-id">{record.queryId || "未生成"}</Typography.Text>
          </div>
          <p className="top-sql__summary-text">{summarizeSql(value)}</p>
        </div>
      )
    },
    { title: "调用", dataIndex: "calls", width: 92, align: "right", render: formatCount },
    { title: "总执行", dataIndex: "totalExecMs", width: 104, align: "right", render: formatMs },
    { title: "平均执行", dataIndex: "meanExecMs", width: 104, align: "right", render: formatMs },
    {
      title: "耗时占比",
      key: "share",
      width: 132,
      render: (_, record) => {
        const share = execShare(record, rows);
        return (
          <div className="top-sql__share">
            <span className="top-sql__share-track"><i style={{ width: `${Math.min(100, share * 100)}%` }} /></span>
            <b>{formatRate(share)}</b>
          </div>
        );
      }
    }
  ];

  return (
    <Card
      className="top-sql"
      title="Top SQL"
      loading={loading}
      extra={capability?.enabled ? (
        <div className="top-sql__toolbar">
          <Segmented size="small" options={sortOptions} value={sortKey} onChange={(value) => setSortKey(value as TopSqlSortKey)} />
          <Input.Search size="small" allowClear placeholder="筛选 SQL" className="top-sql__search" onChange={(event) => setKeyword(event.target.value)} />
        </div>
      ) : (
        <Tag color="warning">{capability?.installed ? "查询不可用" : "不可用"}</Tag>
      )}
    >
      {capability?.enabled ? (
        <>
          <Table
            rowKey={(record, index) => record.queryId || `top-sql-${index}`}
            size="small"
            columns={columns}
            dataSource={visibleRows}
            pagination={{ pageSize: 10, size: "small", hideOnSinglePage: true, showSizeChanger: false }}
            scroll={{ x: 720 }}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={keyword ? "没有匹配的语句" : "暂无统计"} /> }}
            onRow={(record) => ({ onClick: () => setDetail(record) })}
            rowClassName="top-sql__row"
          />
          <Typography.Text type="secondary" className="top-sql__hint">点击任意一行查看完整归一化 SQL 与规划、块读写指标</Typography.Text>
        </>
      ) : (
        <Typography.Text type="secondary">
          {capability?.reason || "当前节点无法读取 Top SQL 统计。"}
        </Typography.Text>
      )}
      <TopSqlDetailDrawer row={detail} share={detail ? execShare(detail, rows) : 0} onClose={() => setDetail(null)} />
    </Card>
  );
};

export default TopSqlPanel;
