import type { IRequestHistoryListItem } from "@/api/requestHistory";
import { Popover, Tag } from "antd";
import React from "react";
import {
  explainRequestStatus,
  type RequestStatusField
} from "./requestStatusExplanation";

interface RequestStatusPopoverProps {
  field: RequestStatusField;
  value?: number | null;
  record: Pick<IRequestHistoryListItem, "statusCode" | "upstreamStatusCode" | "statusSource" | "errorCategory" | "errorCode">;
  color: string;
}

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
    <div className="request-status-card">
      <header className="request-status-card__header">
        <span>{explanation.fieldLabel.toUpperCase()}</span>
        <strong>{explanation.status ?? "—"}</strong>
        <b>{explanation.title}</b>
      </header>
      <section>
        <span className="request-status-card__label">结论</span>
        <p>{explanation.conclusion}</p>
      </section>
      <section>
        <span className="request-status-card__label">状态含义</span>
        <p>{explanation.meaning}</p>
      </section>
      <section>
        <span className="request-status-card__label">影响判断</span>
        <p>{explanation.impact}</p>
      </section>
      {explanation.notice ? <p className="request-status-card__notice">{explanation.notice}</p> : null}
      <div className="request-status-card__metadata">
        <div>
          <span>状态来源</span>
          <b>{explanation.sourceLabel}</b>
          <small>{explanation.sourceMeaning}</small>
        </div>
        {explanation.categoryLabel ? (
          <div>
            <span>错误分类</span>
            <b>{explanation.categoryLabel}</b>
            <small>{explanation.categoryMeaning}</small>
          </div>
        ) : null}
        {explanation.errorCode ? (
          <div>
            <span>错误代码</span>
            <code>{explanation.errorCode}</code>
          </div>
        ) : null}
      </div>
      <section>
        <span className="request-status-card__label">建议操作</span>
        <ul>
          {explanation.actions.map((action) => <li key={action}>{action}</li>)}
        </ul>
      </section>
      <footer>可使用本行“详情”查看 Trace ID、Header、异常信息和完整请求上下文。</footer>
    </div>
  );
};

export const RequestStatusPopover: React.FC<RequestStatusPopoverProps> = ({ field, value, record, color }) => {
  const label = field === "final" ? "最终状态" : "上游状态";
  const trigger = value === null || value === undefined
    ? <span className="request-status-trigger request-status-trigger--empty">—</span>
    : <Tag color={color}>{value}</Tag>;

  return (
    <Popover
      content={<StatusExplanationCard field={field} value={value} record={record} />}
      trigger={["hover", "focus", "click"]}
      mouseEnterDelay={0.16}
      placement="top"
      overlayClassName="request-status-popover"
    >
      <button
        type="button"
        className="request-status-trigger"
        aria-label={`查看${label}${value === null || value === undefined ? "未获取" : value}的说明`}
      >
        {trigger}
      </button>
    </Popover>
  );
};
