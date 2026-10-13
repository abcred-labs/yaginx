import React from "react";
import { Tooltip } from "antd";
import type { ReactNode } from "react";
import type { ColumnType } from "antd/es/table";

export const COMPACT_TEXT_LIMIT = 30;
export const compactTableScrollX = "max-content" as const;

type CompactTextOptions = {
  emptyText?: ReactNode;
  maxWidth?: number;
};

type TableColumn = ColumnType<any> & {
  children?: TableColumn[];
};

const defaultCellTextStyle = (maxWidth: number) => ({
  display: "inline-block",
  maxWidth: `${maxWidth}px`,
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
  verticalAlign: "bottom"
} as const);

export const renderCompactTextCell = (value: unknown, options?: CompactTextOptions) => {
  if (value === null || value === undefined) {
    return options?.emptyText ?? value;
  }

  if (typeof value !== "string") {
    return value as ReactNode;
  }

  if (value.length === 0) {
    return value;
  }

  const maxWidth = options?.maxWidth ?? 220;
  const displayText = value.length > COMPACT_TEXT_LIMIT ? `${value.slice(0, COMPACT_TEXT_LIMIT)}...` : value;
  const content = <span style={defaultCellTextStyle(maxWidth)}>{displayText}</span>;

  if (value.length > COMPACT_TEXT_LIMIT) {
    return <Tooltip title={value}>{content}</Tooltip>;
  }

  return content;
};

export const normalizeCompactTableColumns = <T extends TableColumn>(columns: T[]): T[] =>
  columns.map((column) => {
    const normalizedColumn: T = { ...column };

    if (normalizedColumn.children?.length) {
      normalizedColumn.children = normalizeCompactTableColumns(normalizedColumn.children);
      return normalizedColumn;
    }

    if (typeof normalizedColumn.render !== "function" && normalizedColumn.dataIndex !== undefined) {
      normalizedColumn.render = (value: unknown) => renderCompactTextCell(value);
      normalizedColumn.ellipsis = false;
    }

    return normalizedColumn;
  });
