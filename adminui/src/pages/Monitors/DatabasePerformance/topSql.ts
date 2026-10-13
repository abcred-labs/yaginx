import type { IDatabasePerformance } from "@/api/analytics";
import { formatNumber, toNumber } from "@/utils/formatters";

export type TopSqlRow = IDatabasePerformance["topSql"][number];
export type TopSqlSortKey = "totalExecMs" | "meanExecMs" | "calls";
export type SqlStatementKind = "SELECT" | "INSERT" | "UPDATE" | "DELETE" | "其他";

const summaryLength = 180;

// pg_stat_statements 给出的归一化 SQL 带缩进与注释，整段塞进表格单元格会把行撑到上千像素。
// 摘要只保留一行可扫读的语义，完整语句留给详情抽屉。
export const summarizeSql = (query: string | undefined | null): string => {
  if (!query) {
    return "";
  }
  const flattened = query
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/--[^\n]*/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  return flattened.length > summaryLength ? `${flattened.slice(0, summaryLength)}…` : flattened;
};

export const sqlStatementKind = (query: string | undefined | null): SqlStatementKind => {
  const summary = summarizeSql(query).toUpperCase();
  const leading = summary.replace(/^\(+\s*/, "").replace(/^WITH\b[\s\S]*?\)\s*/, "");
  if (leading.startsWith("SELECT")) {
    return "SELECT";
  }
  if (leading.startsWith("INSERT")) {
    return "INSERT";
  }
  if (leading.startsWith("UPDATE")) {
    return "UPDATE";
  }
  if (leading.startsWith("DELETE")) {
    return "DELETE";
  }
  return "其他";
};

export const sortTopSql = (rows: TopSqlRow[], key: TopSqlSortKey): TopSqlRow[] =>
  [...rows].sort((left, right) => toNumber(right[key]) - toNumber(left[key]));

export const filterTopSql = (rows: TopSqlRow[], keyword: string): TopSqlRow[] => {
  const needle = keyword.trim().toLowerCase();
  if (!needle) {
    return rows;
  }
  return rows.filter((row) => `${row.query ?? ""} ${row.queryId ?? ""}`.toLowerCase().includes(needle));
};

export const execShare = (row: TopSqlRow, rows: TopSqlRow[]): number => {
  const total = rows.reduce((sum, item) => sum + toNumber(item.totalExecMs), 0);
  return total > 0 ? toNumber(row.totalExecMs) / total : 0;
};

// 页面级的紧凑格式：共享的 formatNumber 保留 4 位小数，放在这里会变成 33,479.1234 ms 这类噪音。
export const formatMs = (value: number | string | undefined | null): string => {
  const milliseconds = toNumber(value);
  if (milliseconds === 0) {
    return "0 ms";
  }
  if (Math.abs(milliseconds) < 1) {
    return `${milliseconds.toFixed(2)} ms`;
  }
  if (Math.abs(milliseconds) < 1000) {
    return `${milliseconds.toFixed(milliseconds < 10 ? 1 : 0)} ms`;
  }
  if (Math.abs(milliseconds) < 60_000) {
    return `${(milliseconds / 1000).toFixed(1)} s`;
  }
  return `${(milliseconds / 60_000).toFixed(1)} min`;
};

export const formatRate = (value: number | string | undefined | null): string =>
  `${(toNumber(value) * 100).toFixed(1)}%`;

export const formatCount = (value: number | string | undefined | null): string => {
  const count = toNumber(value);
  if (Math.abs(count) >= 1_000_000) {
    return `${(count / 1_000_000).toFixed(1)}M`;
  }
  if (Math.abs(count) >= 10_000) {
    return `${(count / 1000).toFixed(1)}K`;
  }
  return formatNumber(count);
};
