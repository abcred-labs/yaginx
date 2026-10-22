import { Tooltip, TooltipContent, TooltipTrigger } from "@modra/react";
import React from "react";
import { cx, eyebrowClass } from "@/ui/monParts";

export type MetricTone = "default" | "warning" | "alert";

export interface MetricItem {
  label: string;
  value: React.ReactNode;
  hint?: string;
  tone?: MetricTone;
}

const valueTone: Record<MetricTone, string> = {
  default: "text-foreground",
  warning: "text-warning",
  alert: "text-destructive"
};

// 扁平的 Descriptions 把 6-8 条数字排成同一级，异常值扫不出来。
// 这里把 label 收成小号灰字、值用等宽字号抬一级，并允许单项标记为异常。
export const MetricGrid: React.FC<{ items: MetricItem[]; className?: string }> = ({ items, className }) => (
  <dl className={cx("m-0 grid grid-cols-[repeat(auto-fill,minmax(118px,1fr))] gap-3", className)}>
    {items.map((item) => (
      <div key={item.label}>
        <dt className={cx("m-0", eyebrowClass)}>
          {item.hint ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="cursor-help underline decoration-dotted underline-offset-[3px]">{item.label}</span>
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">{item.hint}</TooltipContent>
            </Tooltip>
          ) : item.label}
        </dt>
        <dd className={cx("m-0 mt-0.5 font-mono text-[17px] leading-tight", valueTone[item.tone ?? "default"])}>{item.value}</dd>
      </div>
    ))}
  </dl>
);

export default MetricGrid;
