import { describe, expect, it } from "vitest";
import {
  execShare,
  filterTopSql,
  formatCount,
  formatMs,
  formatRate,
  sortTopSql,
  sqlStatementKind,
  summarizeSql,
  type TopSqlRow
} from "./topSql";

const row = (overrides: Partial<TopSqlRow>): TopSqlRow => ({
  queryId: "1",
  query: "SELECT 1",
  calls: 0,
  totalPlanMs: 0,
  totalExecMs: 0,
  meanPlanMs: 0,
  meanExecMs: 0,
  sharedBlocksRead: 0,
  sharedBlocksHit: 0,
  tempBlocksRead: 0,
  tempBlocksWritten: 0,
  ...overrides
});

describe("summarizeSql", () => {
  it("drops comments and collapses the indentation pg_stat_statements keeps", () => {
    expect(summarizeSql("SELECT a,\n  b\n/* hint */ FROM t -- trailing\n")).toBe("SELECT a, b FROM t");
  });

  it("truncates long statements so a table row stays two lines", () => {
    const summary = summarizeSql(`SELECT ${"column, ".repeat(80)} FROM t`);
    expect(summary.length).toBe(181);
    expect(summary.endsWith("…")).toBe(true);
  });

  it("tolerates an empty statement", () => {
    expect(summarizeSql(undefined)).toBe("");
  });
});

describe("sqlStatementKind", () => {
  it("classifies the leading keyword", () => {
    expect(sqlStatementKind("  select 1")).toBe("SELECT");
    expect(sqlStatementKind("INSERT INTO t VALUES ($1)")).toBe("INSERT");
    expect(sqlStatementKind("WITH recent AS (SELECT 1) UPDATE t SET a = 1")).toBe("UPDATE");
    expect(sqlStatementKind("DELETE FROM t")).toBe("DELETE");
    expect(sqlStatementKind("VACUUM t")).toBe("其他");
  });
});

describe("sortTopSql", () => {
  const rows = [
    row({ queryId: "a", calls: 10, totalExecMs: 100, meanExecMs: 10 }),
    row({ queryId: "b", calls: 50, totalExecMs: 400, meanExecMs: 8 }),
    row({ queryId: "c", calls: 1, totalExecMs: 50, meanExecMs: 50 })
  ];

  it("orders by the requested metric without mutating the input", () => {
    expect(sortTopSql(rows, "totalExecMs").map((item) => item.queryId)).toEqual(["b", "a", "c"]);
    expect(sortTopSql(rows, "meanExecMs").map((item) => item.queryId)).toEqual(["c", "a", "b"]);
    expect(sortTopSql(rows, "calls").map((item) => item.queryId)).toEqual(["b", "a", "c"]);
    expect(rows.map((item) => item.queryId)).toEqual(["a", "b", "c"]);
  });
});

describe("filterTopSql", () => {
  const rows = [row({ queryId: "a", query: "SELECT * FROM t_request" }), row({ queryId: "b", query: "INSERT INTO t_traffic" })];

  it("matches statement text and query id case-insensitively", () => {
    expect(filterTopSql(rows, "t_REQUEST").map((item) => item.queryId)).toEqual(["a"]);
    expect(filterTopSql(rows, "b").map((item) => item.queryId)).toEqual(["b"]);
  });

  it("returns everything for a blank keyword", () => {
    expect(filterTopSql(rows, "   ")).toHaveLength(2);
  });
});

describe("execShare", () => {
  const rows = [row({ totalExecMs: 75 }), row({ totalExecMs: 25 })];

  it("computes the share of total execution time", () => {
    expect(execShare(rows[0], rows)).toBe(0.75);
  });

  it("returns zero when nothing has been executed", () => {
    const empty = [row({ totalExecMs: 0 })];
    expect(execShare(empty[0], empty)).toBe(0);
  });
});

describe("compact formatters", () => {
  it("scales durations instead of printing four decimals", () => {
    expect(formatMs(0)).toBe("0 ms");
    expect(formatMs(0.4213)).toBe("0.42 ms");
    expect(formatMs(5.42)).toBe("5.4 ms");
    expect(formatMs(128.6)).toBe("129 ms");
    expect(formatMs(33_479.1234)).toBe("33.5 s");
    expect(formatMs(180_000)).toBe("3.0 min");
  });

  it("renders rates with a single decimal", () => {
    expect(formatRate(0.586003)).toBe("58.6%");
    expect(formatRate(undefined)).toBe("0.0%");
  });

  it("abbreviates large counts", () => {
    expect(formatCount(942)).toBe("942");
    expect(formatCount(12_700)).toBe("12.7K");
    expect(formatCount(2_400_000)).toBe("2.4M");
  });
});
