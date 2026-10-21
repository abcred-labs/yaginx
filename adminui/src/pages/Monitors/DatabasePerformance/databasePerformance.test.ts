import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const read = (file: string) => readFileSync(resolve(process.cwd(), `src/pages/Monitors/DatabasePerformance/${file}`), "utf8");

const page = read("index.tsx");
const capabilityBar = read("CapabilityBar.tsx");
const topSqlPanel = read("TopSqlPanel.tsx");
const topSqlDrawer = read("TopSqlDetailDrawer.tsx");

describe("database performance page", () => {
  it("polls every fifteen seconds and preserves the last successful snapshot", () => {
    expect(page).toContain("refreshIntervalMs = 15_000");
    expect(page).toContain("setSnapshot(response.data)");
    expect(page).toContain("setStale(true)");
    expect(page).toContain("controller.abort()");
  });

  it("groups the page into labelled sections", () => {
    expect(page).toContain('<Section label="概览">');
    expect(page).toContain('<Section label="运行态">');
    expect(page).toContain('<Section label="存储">');
    expect(page).toContain('<Section label="查询">');
  });

  it("reports extension availability as badges instead of stacked alerts", () => {
    expect(page).toContain("<CapabilityBar capabilities={snapshot?.capabilities} />");
    expect(capabilityBar).toContain("pg_stat_statements");
    expect(capabilityBar).toContain("pg_stat_io");
    expect(capabilityBar).toContain("track_io_timing");
    expect(capabilityBar).toContain("未安装扩展：");
    expect(capabilityBar).toContain("查询不可用");
    expect(capabilityBar).toContain("状态无法确认");
  });
});

describe("top sql panel", () => {
  it("renders a summary row and opens the detail drawer on click", () => {
    expect(topSqlPanel).toContain("summarizeSql(record.query)");
    expect(topSqlPanel).toContain("onClick={() => setDetail(record)}");
    expect(topSqlPanel).toContain("top-sql-${index}");
    expect(topSqlPanel).toContain("未生成");
  });

  it("falls back to the capability reason when sql statistics are unavailable", () => {
    expect(topSqlPanel).toContain("capability?.enabled");
    expect(topSqlPanel).toContain("capability?.installed");
    expect(topSqlPanel).toContain("当前节点无法读取 Top SQL 统计。");
  });

  it("keeps every pg_stat_statements metric in the drawer", () => {
    expect(topSqlDrawer).toContain("完整归一化 SQL");
    expect(topSqlDrawer).toContain("总规划");
    expect(topSqlDrawer).toContain("平均规划");
    expect(topSqlDrawer).toContain("共享块读取");
    expect(topSqlDrawer).toContain("共享块命中");
    expect(topSqlDrawer).toContain("临时块读取");
    expect(topSqlDrawer).toContain("临时块写入");
  });
});
