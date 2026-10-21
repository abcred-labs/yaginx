import { formatNumber } from "@/utils/formatters";
import { Badge, CopyableText, Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@modra/react";
import React from "react";
import { eyebrowClass } from "@/ui/monParts";
import { MetricGrid } from "./MetricGrid";
import { formatMs, formatRate, sqlStatementKind, type TopSqlRow } from "./topSql";

export const TopSqlDetailDrawer: React.FC<{
  row: TopSqlRow | null;
  share: number;
  onClose: () => void;
}> = ({ row, share, onClose }) => (
  <Sheet open={Boolean(row)} onOpenChange={(open) => { if (!open) onClose(); }}>
    <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-[760px]">
      <SheetHeader>
        <SheetTitle className="flex items-center gap-2">
          <Badge variant="info">{sqlStatementKind(row?.query)}</Badge>
          <CopyableText monospace value={row?.queryId || ""}>{row?.queryId || "未生成"}</CopyableText>
        </SheetTitle>
        <SheetDescription className="sr-only">完整归一化 SQL 与规划、块读写指标</SheetDescription>
      </SheetHeader>
      {row ? (
        <div className="mt-4">
          <MetricGrid
            items={[
              { label: "调用", value: formatNumber(row.calls) },
              { label: "总执行", value: formatMs(row.totalExecMs) },
              { label: "平均执行", value: formatMs(row.meanExecMs) },
              { label: "耗时占比", value: formatRate(share) }
            ]}
          />
          <section className="mt-5">
            <h4 className={`mb-2 mt-0 ${eyebrowClass}`}>完整归一化 SQL</h4>
            <div className="relative">
              <pre className="m-0 max-h-[46vh] overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-muted p-3 font-mono text-xs leading-relaxed text-foreground">{row.query}</pre>
              <div className="mt-2"><CopyableText value={row.query} className="text-xs text-muted-foreground">复制 SQL</CopyableText></div>
            </div>
          </section>
          <section className="mt-5">
            <h4 className={`mb-2 mt-0 ${eyebrowClass}`}>规划与执行</h4>
            <MetricGrid
              items={[
                { label: "总规划", value: formatMs(row.totalPlanMs) },
                { label: "平均规划", value: formatMs(row.meanPlanMs) },
                { label: "总执行", value: formatMs(row.totalExecMs) },
                { label: "平均执行", value: formatMs(row.meanExecMs) }
              ]}
            />
          </section>
          <section className="mt-5">
            <h4 className={`mb-2 mt-0 ${eyebrowClass}`}>块读写</h4>
            <MetricGrid
              items={[
                { label: "共享块读取", value: formatNumber(row.sharedBlocksRead) },
                { label: "共享块命中", value: formatNumber(row.sharedBlocksHit) },
                { label: "临时块读取", value: formatNumber(row.tempBlocksRead), tone: row.tempBlocksRead > 0 ? "warning" : "default" },
                { label: "临时块写入", value: formatNumber(row.tempBlocksWritten), tone: row.tempBlocksWritten > 0 ? "warning" : "default" }
              ]}
            />
          </section>
        </div>
      ) : null}
    </SheetContent>
  </Sheet>
);

export default TopSqlDetailDrawer;
