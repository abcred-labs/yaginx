import { Tooltip } from "antd";
import React from "react";

export type MetricTone = "default" | "warning" | "alert";

export interface MetricItem {
  label: string;
  value: React.ReactNode;
  hint?: string;
  tone?: MetricTone;
}

// 扁平的 Descriptions 把 6-8 条数字排成同一级，异常值扫不出来。
// 这里把 label 收成小号灰字、值用等宽字号抬一级，并允许单项标记为异常。
export const MetricGrid: React.FC<{ items: MetricItem[] }> = ({ items }) => (
  <dl className="metric-grid">
    {items.map((item) => (
      <div key={item.label} className={`metric-grid__item metric-grid__item--${item.tone ?? "default"}`}>
        <dt>
          {item.hint ? <Tooltip title={item.hint}><span className="metric-grid__label-hint">{item.label}</span></Tooltip> : item.label}
        </dt>
        <dd>{item.value}</dd>
      </div>
    ))}
  </dl>
);

export default MetricGrid;
