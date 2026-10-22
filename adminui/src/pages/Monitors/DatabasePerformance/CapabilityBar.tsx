import type { IDatabaseCapability, IDatabasePerformance } from "@/api/analytics";
import { Tooltip, TooltipContent, TooltipTrigger } from "@modra/react";
import { CircleAlert, CircleCheck, CircleMinus } from "@/ui/icons";
import React from "react";
import { cx, eyebrowClass } from "@/ui/monParts";

const pgStatStatementsMissingReason = "pg_stat_statements extension is not installed";

// 扩展缺失原本是三条整宽 Alert，把真正的指标顶出首屏。
// 能力状态是常驻事实而非一次性告警，所以收成标题下的一行徽章，原因进 Tooltip。
export const capabilitySummary = (name: string, capability?: IDatabaseCapability): { title: string; description: string } => {
  if (!capability) {
    return { title: `${name} 状态无法确认`, description: "当前节点未返回该项能力状态。" };
  }
  if (capability.enabled) {
    return { title: `${name} 已启用`, description: capability.reason || "统计数据可用。" };
  }
  if (capability.installed) {
    return { title: `${name} 查询不可用`, description: capability.reason || "扩展已安装，但当前节点无法读取统计。" };
  }
  if (capability.reason === pgStatStatementsMissingReason) {
    return { title: `未安装扩展：${name}`, description: "在 PostgreSQL 中安装并预加载后可查看 Top SQL 统计。" };
  }
  return { title: `${name} 不可用`, description: capability.reason || "当前节点未能确认该项能力。" };
};

const badgeTone = {
  on: "border-success/40 text-success",
  warn: "border-warning/50 bg-warning/10 text-warning",
  off: "text-muted-foreground"
} as const;

const CapabilityBadge: React.FC<{ name: string; capability?: IDatabaseCapability; optional?: boolean }> = ({ name, capability, optional }) => {
  const summary = capabilitySummary(name, capability);
  const state = capability?.enabled ? "on" : optional ? "off" : "warn";
  const Icon = state === "on" ? CircleCheck : state === "off" ? CircleMinus : CircleAlert;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cx("inline-flex cursor-help items-center gap-1.5 rounded-full border border-border bg-card px-2.5 py-0.5 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", badgeTone[state])}
          tabIndex={0}
        >
          <Icon className="size-3.5" aria-hidden="true" />
          <code className="bg-transparent font-mono text-[11px]">{name}</code>
        </span>
      </TooltipTrigger>
      <TooltipContent><b>{summary.title}</b><br />{summary.description}</TooltipContent>
    </Tooltip>
  );
};

export const CapabilityBar: React.FC<{ capabilities?: IDatabasePerformance["capabilities"] }> = ({ capabilities }) => (
  <div className="flex flex-wrap items-center gap-2">
    <span className={eyebrowClass}>统计能力</span>
    <CapabilityBadge name="pg_stat_statements" capability={capabilities?.pgStatStatements} />
    <CapabilityBadge name="pg_stat_io" capability={capabilities?.pgStatIO} optional />
    <CapabilityBadge name="track_io_timing" capability={capabilities?.trackIOTiming} optional />
  </div>
);

export default CapabilityBar;
