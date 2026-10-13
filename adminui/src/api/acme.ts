import { FetchJson } from "@/requests/FetchJson";
export interface ACMEOrder { id:string; domain:string; status:string; attempt:number; errorClass?:string; errorMessage?:string; createdAt:string; updatedAt:string; nextAttemptAt?:string; finishedAt?:string }
export interface ACMEOrderPage { records: ACMEOrder[]; total:number; pageIndex:number; pageSize:number }
export interface ACMEOrderLog { id:string; orderId:string; occurredAt:string; operation:string; status:string; result:string; errorCode?:string; message?:string; nodeId?:string; attempt:number }
export interface ACMEOrderLogPage { records: ACMEOrderLog[]; paging: { pageIndex:number; pageSize:number; total:number } }
export const getACMEOrders = (params?: {domain?:string;status?:string;pageIndex?:number;pageSize?:number}) => FetchJson<ACMEOrderPage>({url:'/api/acme/orders',method:'GET',params});
export const cancelACMEOrder = (id:string) => FetchJson({url:'/api/acme/orders/cancel',method:'POST',data:{id}});
export const retryACMEOrder = (id:string) => FetchJson({url:'/api/acme/orders/retry',method:'POST',data:{id}});
export const getACMEOrderLogs = (id:string, params?: {pageIndex?:number;pageSize?:number}) => FetchJson<ACMEOrderLogPage>({url:`/api/acme/orders/${encodeURIComponent(id)}/logs`,method:'GET',params});
export const getACMELogs = (params?: {pageIndex?:number;pageSize?:number}) => FetchJson<{records:Array<Record<string, any>>;paging:{pageIndex:number;pageSize:number;total:number}}>({url:'/api/acme/logs',method:'POST',data:{pageIndex:params?.pageIndex??1,pageSize:params?.pageSize??100}});
