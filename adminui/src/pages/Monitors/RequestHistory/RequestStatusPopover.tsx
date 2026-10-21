import type { IRequestHistoryListItem } from "@/api/requestHistory";
import { Badge, Popover, PopoverAnchor, PopoverContent, type BadgeProps } from "@modra/react";
import React, { useEffect, useRef, useState } from "react";
import {
  explainRequestStatus,
  type RequestStatusField
} from "./requestStatusExplanation";

interface RequestStatusPopoverProps {
  field: RequestStatusField;
  value?: number | null;
  record: Pick<IRequestHistoryListItem, "statusCode" | "upstreamStatusCode" | "statusSource" | "errorCategory" | "errorCode">;
  variant: BadgeProps["variant"];
}

const labelClass = "font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground";

const StatusExplanationCard: React.FC<{
  field: RequestStatusField;
  value?: number | null;
  record: RequestStatusPopoverProps["record"];
}> = ({ field, value, record }) => {
  const explanation = explainRequestStatus({
    ...record,
    [field === "final" ? "statusCode" : "upstreamStatusCode"]: value
  }, field);

  return (
    <div className="w-[min(390px,calc(100vw-40px))] text-[13px] leading-relaxed text-foreground">
      <header className="grid grid-cols-[auto_1fr] items-center gap-x-2 gap-y-0.5 border-b border-border bg-muted px-4 py-3">
        <span className="col-span-2 font-mono text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">{explanation.fieldLabel.toUpperCase()}</span>
        <strong className="font-mono text-2xl leading-none">{explanation.status ?? "—"}</strong>
        <b className="text-sm leading-snug">{explanation.title}</b>
      </header>
      <div className="flex flex-col gap-2.5 px-4 pt-2.5">
        <section>
          <span className={labelClass}>结论</span>
          <p>{explanation.conclusion}</p>
        </section>
        <section>
          <span className={labelClass}>状态含义</span>
          <p>{explanation.meaning}</p>
        </section>
        <section>
          <span className={labelClass}>影响判断</span>
          <p>{explanation.impact}</p>
        </section>
        {explanation.notice ? (
          <p className="rounded-r-md border-l-[3px] border-warning bg-warning/10 px-2 py-2 text-xs">{explanation.notice}</p>
        ) : null}
        <div className="grid gap-2 rounded-lg border border-border bg-muted p-2.5 text-xs">
          <div className="grid grid-cols-[72px_1fr] gap-x-2">
            <span className="row-span-2 text-muted-foreground">状态来源</span>
            <b className="break-words font-semibold">{explanation.sourceLabel}</b>
            <small className="text-[11px] text-muted-foreground">{explanation.sourceMeaning}</small>
          </div>
          {explanation.categoryLabel ? (
            <div className="grid grid-cols-[72px_1fr] gap-x-2">
              <span className="row-span-2 text-muted-foreground">错误分类</span>
              <b className="break-words font-semibold">{explanation.categoryLabel}</b>
              <small className="text-[11px] text-muted-foreground">{explanation.categoryMeaning}</small>
            </div>
          ) : null}
          {explanation.errorCode ? (
            <div className="grid grid-cols-[72px_1fr] gap-x-2">
              <span className="text-muted-foreground">错误代码</span>
              <code className="break-words font-semibold">{explanation.errorCode}</code>
            </div>
          ) : null}
        </div>
        <section>
          <span className={labelClass}>建议操作</span>
          <ul className="mt-1 grid list-disc gap-1 pl-[18px] marker:text-primary">
            {explanation.actions.map((action) => <li key={action}>{action}</li>)}
          </ul>
        </section>
      </div>
      <footer className="mt-3 border-t border-border px-4 py-2 text-[11px] leading-snug text-muted-foreground">
        可使用本行“详情”查看 Trace ID、Header、异常信息和完整请求上下文。
      </footer>
    </div>
  );
};

const OPEN_DELAY_MS = 160;
const CLOSE_DELAY_MS = 120;

export const RequestStatusPopover: React.FC<RequestStatusPopoverProps> = ({ field, value, record, variant }) => {
  const label = field === "final" ? "最终状态" : "上游状态";
  const empty = value === null || value === undefined;
  const [open, setOpen] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  const schedule = (next: boolean, delay: number) => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setOpen(next), delay);
  };
  useEffect(() => () => window.clearTimeout(timer.current), []);

  // 悬停/聚焦/点击都可打开；离开触发器与浮层后延迟关闭，保证鼠标能移进浮层。
  return (
    <Popover open={open} onOpenChange={(next) => { window.clearTimeout(timer.current); setOpen(next); }}>
      <PopoverAnchor asChild>
        <button
          type="button"
          className="inline-flex cursor-help items-center rounded-sm border-0 bg-transparent p-0 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
          aria-label={`查看${label}${empty ? "未获取" : value}的说明`}
          onPointerEnter={() => schedule(true, OPEN_DELAY_MS)}
          onPointerLeave={() => schedule(false, CLOSE_DELAY_MS)}
          onFocus={() => schedule(true, 0)}
          onBlur={() => schedule(false, CLOSE_DELAY_MS)}
          onClick={() => { window.clearTimeout(timer.current); setOpen(true); }}
        >
          {empty
            ? <span className="inline-flex min-w-5 justify-center font-mono text-muted-foreground underline decoration-dotted underline-offset-[3px]">—</span>
            : <Badge variant={variant}>{value}</Badge>}
        </button>
      </PopoverAnchor>
      <PopoverContent
        side="top"
        className="w-auto max-w-[calc(100vw-24px)] overflow-hidden p-0"
        onOpenAutoFocus={(event) => event.preventDefault()}
        onPointerEnter={() => window.clearTimeout(timer.current)}
        onPointerLeave={() => schedule(false, CLOSE_DELAY_MS)}
      >
        <StatusExplanationCard field={field} value={value} record={record} />
      </PopoverContent>
    </Popover>
  );
};
