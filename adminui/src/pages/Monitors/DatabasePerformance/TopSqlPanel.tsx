import type { IDatabaseCapability } from "@/api/analytics";
import { Badge, DataTable, Input } from "@modra/react";
import type { ColumnDef, PaginationState } from "@tanstack/react-table";
import React, { useMemo, useState } from "react";
import { MonBar, MonCard, MonSegmented } from "@/ui/monParts";
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

const rightAligned = (title: string): ColumnDef<TopSqlRow, unknown>["header"] => () => <div className="text-right">{title}</div>;

export const TopSqlPanel: React.FC<{
  rows: TopSqlRow[];
  loading: boolean;
  capability?: IDatabaseCapability;
}> = ({ rows, loading, capability }) => {
  const [sortKey, setSortKey] = useState<TopSqlSortKey>("totalExecMs");
  const [keyword, setKeyword] = useState("");
  const [detail, setDetail] = useState<TopSqlRow | null>(null);
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 });

  const visibleRows = useMemo(() => sortTopSql(filterTopSql(rows, keyword), sortKey), [rows, keyword, sortKey]);

  const columns: ColumnDef<TopSqlRow, unknown>[] = [
    {
      id: "query",
      header: "SQL",
      meta: { minWidth: 300 },
      cell: ({ row }) => {
        const record = row.original;
        // DataTable 没有整行点击，改为让摘要区本身成为按钮，键盘可达。
        return (
          <button type="button" className="flex min-w-[260px] max-w-[640px] flex-col gap-[3px] text-left" onClick={() => setDetail(record)} aria-label="查看 SQL 详情">
            <span className="flex items-center gap-2">
              <Badge variant="secondary" className="text-[10px]">{sqlStatementKind(record.query)}</Badge>
              <span className="font-mono text-[11px] text-muted-foreground">{record.queryId || "未生成"}</span>
            </span>
            <span className="line-clamp-2 font-mono text-xs leading-normal text-foreground">{summarizeSql(record.query)}</span>
          </button>
        );
      }
    },
    { id: "calls", header: rightAligned("调用"), meta: { minWidth: 92 }, cell: ({ row }) => <div className="text-right">{formatCount(row.original.calls)}</div> },
    { id: "totalExecMs", header: rightAligned("总执行"), meta: { minWidth: 104 }, cell: ({ row }) => <div className="text-right">{formatMs(row.original.totalExecMs)}</div> },
    { id: "meanExecMs", header: rightAligned("平均执行"), meta: { minWidth: 104 }, cell: ({ row }) => <div className="text-right">{formatMs(row.original.meanExecMs)}</div> },
    {
      id: "share",
      header: "耗时占比",
      meta: { minWidth: 132 },
      cell: ({ row }) => {
        const share = execShare(row.original, rows);
        return (
          <div className="flex items-center gap-2">
            <MonBar ratio={share} />
            <b className="min-w-11 text-right font-mono text-xs font-medium">{formatRate(share)}</b>
          </div>
        );
      }
    }
  ];

  return (
    <MonCard
      title="Top SQL"
      loading={loading}
      extra={capability?.enabled ? (
        <div className="flex flex-wrap items-center justify-end gap-2">
          <MonSegmented options={sortOptions} value={sortKey} onChange={setSortKey} ariaLabel="排序方式" />
          <Input size="sm" placeholder="筛选 SQL" className="w-[180px] max-md:w-[140px]" value={keyword} onChange={(event) => { setKeyword(event.target.value); setPagination((current) => ({ ...current, pageIndex: 0 })); }} />
        </div>
      ) : (
        <Badge variant="warning">{capability?.installed ? "查询不可用" : "不可用"}</Badge>
      )}
    >
      {capability?.enabled ? (
        <>
          <DataTable
            columns={columns}
            data={visibleRows}
            getRowId={(record, index) => record.queryId || `top-sql-${index}`}
            pagination={pagination}
            onPaginationChange={setPagination}
            showPagination={visibleRows.length > pagination.pageSize}
            pageSizeOptions={[10]}
            emptyText={keyword ? "没有匹配的语句" : "暂无统计"}
          />
          <span className="mt-2 block text-[11px] text-muted-foreground">点击任意一行的 SQL 摘要查看完整归一化 SQL 与规划、块读写指标</span>
        </>
      ) : (
        <span className="text-sm text-muted-foreground">
          {capability?.reason || "当前节点无法读取 Top SQL 统计。"}
        </span>
      )}
      <TopSqlDetailDrawer row={detail} share={detail ? execShare(detail, rows) : 0} onClose={() => setDetail(null)} />
    </MonCard>
  );
};

export default TopSqlPanel;
