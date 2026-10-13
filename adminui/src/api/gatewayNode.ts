import { FetchJson } from "@/requests/FetchJson";
import type { IApiRspEnvelop } from "./apiTypes";

export interface GatewayNode {
  nodeId: string;
  machineFingerprint: string;
  displayName: string;
  status: "active" | "draining" | "disabled";
  nodeNumber: number | null;
  stateReason: string;
  softwareVersion: string;
  gitSha: string;
  appliedRevision: string;
  reloadGeneration: string;
  observedReloadGeneration: string;
  restartGeneration: string;
  observedRestartGeneration: string;
  lastSnapshotAt: number | null;
  lastSnapshotError: string;
  lastHeartbeatAt: number | null;
  stale: boolean;
  drainStartedAt: number | null;
  disabledAt: number | null;
  createdAt: number;
  updatedAt: number;
}

export interface GatewayNodePage {
  records: GatewayNode[];
  paging: { pageIndex: number; pageSize: number; total: number };
}

export const searchGatewayNodes = (data: { pageIndex?: number; pageSize?: number; status?: string; keyword?: string } = {}): Promise<IApiRspEnvelop<GatewayNodePage>> =>
  FetchJson({ url: "/api/gateway_node/search", method: "POST", data });

export const deleteGatewayNode = (nodeId: string) =>
  FetchJson({ url: "/api/gateway_node/delete", method: "POST", data: { nodeId } });

export const disableGatewayNode = (nodeId: string, reason: string) =>
  FetchJson({ url: "/api/gateway_node/disable", method: "POST", data: { nodeId, reason } });

export const reloadGatewayNode = (nodeId: string) =>
  FetchJson({ url: "/api/gateway_node/reload", method: "POST", data: { nodeId } });

export const restartGatewayNode = (nodeId: string) =>
  FetchJson({ url: "/api/gateway_node/restart", method: "POST", data: { nodeId } });
