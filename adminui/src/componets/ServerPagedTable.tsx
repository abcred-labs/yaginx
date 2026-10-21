import { DEFAULT_PAGE_INDEX, DEFAULT_PAGE_SIZE, IApiRspEnvelop, PAGE_SIZE_OPTIONS, Page, PageRequest } from "@/api/apiTypes"
import { TableOperationMenuItem, TableOperations } from "@/componets/TableOperationMenus"
import { compactTableScrollX, normalizeCompactTableColumns } from "@/componets/tableCompact"
import { Table } from "antd"
import type { TablePaginationConfig } from "antd/es/table"
import { SizeType } from "antd/es/config-provider/SizeContext"
import React, { useEffect, useState } from "react"
import { buildServerPageRequest } from "@/ui/serverPageRequest"

export { buildServerPageRequest }

export interface ServerPagedTableProps<T> {
  pkFieldName: string;
  renderTableOperationMenu?: (record: T) => TableOperationMenuItem[];
  onTableMenuClick?: (commandKey: string, record: T) => void;
  initTableColumn: (columnsArray: any[]) => void;
  realodTableData: number;
  searchAction: (data: any) => Promise<IApiRspEnvelop<Page<T>>>;
  itemConverter?: (item: T) => any;
  defaultFilterCondition?: Record<string, any>;
  externalFilterCondition?: Record<string, any>;
  filterResetKey?: number;
  selectedRowKeys?: string[];
  onSelectedRowKeysChange?: (keys: string[]) => void;
  onPageDataChange?: (records: T[]) => void;
  size?: SizeType;
}

const ServerPagedTable = <T,>(props: ServerPagedTableProps<T>) => {
  const [refreshToken, setRefreshToken] = useState(0);
  const [columns, setColumns] = useState<any[]>([]);
  const [dataSource, setDataSource] = useState<any[]>([]);
  const [page, setPage] = useState<PageRequest>({ pageIndex: DEFAULT_PAGE_INDEX, pageSize: DEFAULT_PAGE_SIZE });
  const [total, setTotal] = useState(0);
  const [filterCondition, setFilterCondition] = useState<Record<string, any>>(props.defaultFilterCondition ?? {});

  useEffect(() => {
    const columnsArray: any[] = [{ title: '#', dataIndex: 'rowIndex', key: 'rowIndex', align: 'center', width: 64 }];
    props.initTableColumn(columnsArray);
    if (props.renderTableOperationMenu && props.onTableMenuClick) {
      columnsArray.push({
        title: 'Actions', key: 'menuOperations', dataIndex: props.pkFieldName, width: 150,
        render: (_: unknown, record: T) => <TableOperations
          record={record}
          menuOptions={props.renderTableOperationMenu?.(record) ?? []}
          onMenuClick={props.onTableMenuClick ?? (() => undefined)}
          onComplete={() => setRefreshToken((value) => value + 1)}
        />
      });
    }
    setColumns(normalizeCompactTableColumns(columnsArray));
  }, [props.initTableColumn, props.onTableMenuClick, props.pkFieldName, props.renderTableOperationMenu]);

  useEffect(() => {
    setPage((current) => ({ pageIndex: DEFAULT_PAGE_INDEX, pageSize: current.pageSize ?? DEFAULT_PAGE_SIZE }));
  }, [props.externalFilterCondition, props.filterResetKey]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      props.onPageDataChange?.([]);
      const request = buildServerPageRequest(filterCondition, props.externalFilterCondition, page);
      const response = await props.searchAction(request);
      if (cancelled || response.code !== 200) return;
      const result = response.data;
      props.onPageDataChange?.(result.records);
      const effectivePage = result.paging.pageIndex || DEFAULT_PAGE_INDEX;
      const effectiveSize = result.paging.pageSize || DEFAULT_PAGE_SIZE;
      setPage({ pageIndex: effectivePage, pageSize: effectiveSize });
      setTotal(result.paging.total);
      let records = result.records.map((item, index) => ({
        ...item,
        key: (item as any)[props.pkFieldName],
        rowIndex: (effectivePage - 1) * effectiveSize + index + 1,
      }));
      if (props.itemConverter) records = records.map((item) => props.itemConverter?.(item) ?? item);
      setDataSource(records);
    };
    void load();
    return () => { cancelled = true; };
  }, [filterCondition, page.pageIndex, page.pageSize, props.externalFilterCondition, props.onPageDataChange, props.realodTableData, refreshToken, props.searchAction]);

  const onTableChange = (next: TablePaginationConfig, filters: Record<string, any>) => {
    setFilterCondition(filters);
    setPage({ pageIndex: next.current ?? DEFAULT_PAGE_INDEX, pageSize: next.pageSize ?? DEFAULT_PAGE_SIZE });
  };

  const pagination: TablePaginationConfig = {
    current: page.pageIndex,
    pageSize: page.pageSize,
    total,
    showSizeChanger: true,
    pageSizeOptions: [...PAGE_SIZE_OPTIONS],
  };

  return <Table
    dataSource={dataSource}
    columns={columns}
    pagination={pagination}
    onChange={onTableChange}
    rowSelection={props.onSelectedRowKeysChange ? {
      selectedRowKeys: props.selectedRowKeys,
      preserveSelectedRowKeys: true,
      onChange: (keys) => props.onSelectedRowKeysChange?.(keys as string[]),
    } : undefined}
    size={props.size ?? "small"}
    scroll={{ x: compactTableScrollX }}
  />;
};

export default ServerPagedTable;
