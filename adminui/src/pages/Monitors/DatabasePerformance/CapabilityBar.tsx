import type { IDatabaseCapability, IDatabasePerformance } from "@/api/analytics";
import { CheckCircleFilled, ExclamationCircleFilled, MinusCircleFilled } from "@ant-design/icons";
import { Tooltip } from "antd";
import React from "react";

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

const CapabilityBadge: React.FC<{ name: string; capability?: IDatabaseCapability; optional?: boolean }> = ({ name, capability, optional }) => {
  const summary = capabilitySummary(name, capability);
  const state = capability?.enabled ? "on" : optional ? "off" : "warn";
  const icon = state === "on"
    ? <CheckCircleFilled />
    : state === "off"
      ? <MinusCircleFilled />
      : <ExclamationCircleFilled />;

  return (
    <Tooltip title={<><b>{summary.title}</b><br />{summary.description}</>}>
      <span className={`capability-bar__badge capability-bar__badge--${state}`} tabIndex={0}>
        {icon}
        <code>{name}</code>
      </span>
    </Tooltip>
  );
};

export const CapabilityBar: React.FC<{ capabilities?: IDatabasePerformance["capabilities"] }> = ({ capabilities }) => (
  <div className="capability-bar">
    <span className="capability-bar__label">统计能力</span>
    <CapabilityBadge name="pg_stat_statements" capability={capabilities?.pgStatStatements} />
    <CapabilityBadge name="pg_stat_io" capability={capabilities?.pgStatIO} optional />
    <CapabilityBadge name="track_io_timing" capability={capabilities?.trackIOTiming} optional />
  </div>
);

export default CapabilityBar;
