import moment from "moment";
import React, { useEffect, useRef } from "react";
export const LocalTime = ({ timestamp, format }: { timestamp?: number | string | null, format?: string | null }) => {
    if (format == null) {
        format = 'YYYY-MM-DD HH:mm:ss';
    }
    if (timestamp === null || timestamp === undefined || timestamp === "") {
        return <>-</>;
    }
    const numericTimestamp = typeof timestamp === "string" ? Number(timestamp) : timestamp;
    if (!Number.isFinite(numericTimestamp) || numericTimestamp <= 0) {
        return <>-</>;
    }
    return <>{moment(numericTimestamp * 1000).local().format(format)}</>;
};
const randomid = () => parseInt(String(Math.random() * 1e15), 10).toString(36);
const getCode = (arr: any = []) =>
  arr
    .map((dt: any) => {
      if (typeof dt === 'string') {
        return dt;
      }
      if (dt.props && dt.props.children) {
        return getCode(dt.props.children);
      }
      return false;
    })
    .filter(Boolean)
    .join('');
