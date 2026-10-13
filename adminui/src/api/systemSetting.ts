import { IApiRspEnvelop } from "./apiTypes";
import { FetchJson } from "@/requests/FetchJson";

export interface SmtpSettings {
  enabled: boolean;
  host: string;
  port: number;
  useSsl: boolean;
  userName: string;
  password: string;
  fromEmail: string;
  fromName: string;
}

export const getSmtpSetting = (): Promise<IApiRspEnvelop<SmtpSettings>> =>
  FetchJson({ url: '/api/system_setting/smtp', method: 'GET' });

export const saveSmtpSetting = (data: SmtpSettings): Promise<IApiRspEnvelop<boolean>> =>
  FetchJson({ url: '/api/system_setting/smtp', method: 'POST', data });

export const testSendSmtpSetting = (data: { email: string }): Promise<IApiRspEnvelop<boolean>> =>
  FetchJson({ url: '/api/system_setting/smtp/test_send', method: 'POST', data });

export interface RuntimeSetting {
  config: Record<string, any>;
  revision: string;
  updatedAt: number | null;
  applyMode: "reload" | "restart";
}

export interface RuntimeSettingStatus {
  revision: string;
  pendingRestart: boolean;
  nodes: Array<Record<string, any>>;
  applyMode: "reload" | "restart";
}

export interface RuntimeAccessContext {
  clientIP: string;
  allowedCIDRs: string[];
  clientAllowed: boolean;
}

export const getRuntimeSetting = (): Promise<IApiRspEnvelop<RuntimeSetting | null>> =>
  FetchJson({ url: '/api/system_setting/runtime', method: 'GET' });

export const saveRuntimeSetting = (data: {
  config: Record<string, any>;
  expectedRevision?: string | null;
  restart: boolean;
}): Promise<IApiRspEnvelop<RuntimeSetting | null>> =>
  FetchJson({ url: '/api/system_setting/runtime', method: 'POST', data });

export const getRuntimeSettingStatus = (): Promise<IApiRspEnvelop<RuntimeSettingStatus>> =>
  FetchJson({ url: '/api/system_setting/runtime/status', method: 'GET' });

export const getRuntimeAccessContext = (): Promise<IApiRspEnvelop<RuntimeAccessContext>> =>
  FetchJson({ url: '/api/system_setting/runtime/access-context', method: 'GET' });
