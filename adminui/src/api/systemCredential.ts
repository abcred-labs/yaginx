import type { IApiRspEnvelop } from './apiTypes';
import { FetchJson } from '@/requests/FetchJson';

export interface APICredential {
  id: string;
  name: string;
  purpose: string;
  tokenPrefix: string;
  status: 'active' | 'revoked';
  createdAt: number;
  updatedAt: number;
  revokedAt: number | null;
}

export interface APICredentialSecret extends APICredential {
  token: string;
}

export const listAPICredentials = (): Promise<IApiRspEnvelop<APICredential[]>> =>
  FetchJson({ url: '/api/system_credentials', method: 'GET' });

export const createAPICredential = (data: { name: string; purpose: string }): Promise<IApiRspEnvelop<APICredentialSecret>> =>
  FetchJson({ url: '/api/system_credentials', method: 'POST', data });

export const resetAPICredential = (id: string): Promise<IApiRspEnvelop<APICredentialSecret>> =>
  FetchJson({ url: `/api/system_credentials/${encodeURIComponent(id)}/reset`, method: 'POST' });

export const revokeAPICredential = (id: string): Promise<IApiRspEnvelop<APICredential>> =>
  FetchJson({ url: `/api/system_credentials/${encodeURIComponent(id)}/revoke`, method: 'POST' });
