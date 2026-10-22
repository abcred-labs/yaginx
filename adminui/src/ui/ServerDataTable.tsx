import { DataTable } from '@modra/react';
import type { ColumnDef, ColumnFiltersState, PaginationState } from '@tanstack/react-table';
import React, { useEffect, useMemo, useState } from 'react';
import { DEFAULT_PAGE_SIZE, IApiRspEnvelop, PAGE_SIZE_OPTIONS, Page } from '@/api/apiTypes';
import { buildServerPageRequest } from './serverPageRequest';

export interface ServerDataTableProps<T, R = Record<string, unknown>> {
  columns: ColumnDef<T, unknown>[];
  /** 接收含 pageIndex（从 1 开始）、pageSize 与过滤条件的请求体。 */
  searchAction: (request: R) => Promise<IApiRspEnvelop<Page<T>>>;
  getRowId: (row: T) => string;
  /** 外部过滤条件；引用变化时回到第一页。 */
  filter?: Record<string, unknown>;
  /** 自增后强制重新加载当前页。 */
  reloadKey?: number;
  selectedRowKeys?: string[];
  onSelectedRowKeysChange?: (keys: string[]) => void;
  onPageDataChange?: (records: T[]) => void;
  emptyText?: React.ReactNode;
}

/** 表头过滤器的值按列 id 平铺进请求体，与旧 antd 表格的 filters 约定一致。 */
export const columnFiltersToRequest = (filters: ColumnFiltersState): Record<string, unknown> =>
  Object.fromEntries(filters.map(({ id, value }) => [id, value]));

const PAGE_SIZE_NUMBERS = PAGE_SIZE_OPTIONS.map(Number);

const ServerDataTable = <T, R = Record<string, unknown>>(props: ServerDataTableProps<T, R>) => {
  const { searchAction, filter, reloadKey, onPageDataChange } = props;
  // DataTable 的 pageIndex 从 0 开始，API 从 1 开始，只在请求边界换算。
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: DEFAULT_PAGE_SIZE });
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);
  const [rows, setRows] = useState<T[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setPagination((current) => ({ ...current, pageIndex: 0 }));
  }, [filter]);

  const columnFilterRequest = useMemo(() => columnFiltersToRequest(columnFilters), [columnFilters]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    const request = buildServerPageRequest(columnFilterRequest, filter, {
      pageIndex: pagination.pageIndex + 1,
      pageSize: pagination.pageSize,
    });
    searchAction(request as R)
      .then((response) => {
        if (cancelled || response.code !== 200) return;
        setRows(response.data.records);
        setTotal(response.data.paging.total);
        onPageDataChange?.(response.data.records);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [columnFilterRequest, filter, pagination.pageIndex, pagination.pageSize, reloadKey, searchAction, onPageDataChange]);

  return (
    <DataTable
      columns={props.columns}
      data={rows}
      getRowId={props.getRowId}
      loading={loading}
      emptyText={props.emptyText}
      manualPagination
      manualFiltering
      pagination={pagination}
      onPaginationChange={setPagination}
      columnFilters={columnFilters}
      onColumnFiltersChange={(updater) => {
        setColumnFilters(updater);
        setPagination((current) => ({ ...current, pageIndex: 0 }));
      }}
      total={total}
      showPagination
      pageSizeOptions={PAGE_SIZE_NUMBERS}
      enableRowSelection={Boolean(props.onSelectedRowKeysChange)}
      selectedRowKeys={props.selectedRowKeys}
      onSelectedRowKeysChange={(keys) => props.onSelectedRowKeysChange?.(keys.map(String))}
    />
  );
};

export default ServerDataTable;
