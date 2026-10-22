import { DEFAULT_PAGE_INDEX, DEFAULT_PAGE_SIZE, PageRequest } from "@/api/apiTypes";

export const buildServerPageRequest = (
  filterCondition: Record<string, any> = {},
  externalFilterCondition: Record<string, any> = {},
  page: PageRequest = {}
): Record<string, any> => {
  const request = { ...filterCondition, ...externalFilterCondition };
  // 表格组件自身的状态字段（antd 的 current 等）不属于接口协议。
  delete request.current;
  delete request.pageSizeOptions;
  return {
    ...request,
    pageIndex: page.pageIndex ?? DEFAULT_PAGE_INDEX,
    pageSize: page.pageSize ?? DEFAULT_PAGE_SIZE,
  };
};
