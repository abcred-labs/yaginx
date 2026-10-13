export const DEFAULT_PAGE_INDEX = 1;
export const DEFAULT_PAGE_SIZE = 50;
export const MAX_PAGE_SIZE = 200;
export const PAGE_SIZE_OPTIONS = ["50", "100", "200"] as const;

export interface PageRequest {
  pageIndex?: number;
  pageSize?: number;
}

export interface PageCursor {
  pageIndex: number;
  pageSize: number;
  total: number;
}

export interface Page<T> {
  records: T[];
  paging: PageCursor;
}

export type PageResult<T> = Page<T>;
export type PaginationRequestParams = PageRequest;

export interface IApiRspEnvelop<T> {
  data: T;
  code: number;
  msg: string | null;
  errMsg: string | null;
  tid: string;
}
