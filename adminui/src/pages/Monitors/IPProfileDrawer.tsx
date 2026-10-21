import { IIPProfile } from "@/api/accessControl";
import { confirmDanger } from "@/ui/confirmAction";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Descriptions,
  Empty,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  Spin,
  type DescriptionItem
} from "@modra/react";
import React, { useState } from "react";
import { actionableProfileNetworkCIDR } from "./ipProfileNetwork";

const valueOrDash = (value?: string | number | null) => value === null || value === undefined || value === "" ? "-" : value;

const location = (profile: IIPProfile) => [
  profile.country?.name || profile.country?.isoCode,
  profile.subdivision?.name || profile.subdivision?.isoCode,
  profile.city?.name
].filter(Boolean).join(" / ") || "-";

const networkTypeText: Record<string, string> = {
  datacenter: "机房",
  residential: "家庭",
  mobile: "移动网络",
  proxy: "代理",
  unknown: "未知"
};

export type IPBlacklistScope = "ip" | "network";

export const IPProfileDrawer = ({
  open,
  loading,
  profile,
  error,
  onClose,
  onBlacklist
}: {
  open: boolean;
  loading?: boolean;
  profile: IIPProfile | null;
  error?: string;
  onClose: () => void;
  onBlacklist?: (cidr: string, scope: IPBlacklistScope) => Promise<void>;
}) => {
  const [submitting, setSubmitting] = useState<IPBlacklistScope | null>(null);
  const networkCIDR = actionableProfileNetworkCIDR(profile?.asn?.network);

  const addToBlacklist = async (cidr: string, scope: IPBlacklistScope) => {
    if (!onBlacklist) {
      return;
    }
    setSubmitting(scope);
    try {
      await onBlacklist(cidr, scope);
    } finally {
      setSubmitting(null);
    }
  };

  const traits = Object.entries(profile?.traits || {}).filter(([, enabled]) => enabled);

  const items: DescriptionItem[] = profile ? [
    { key: "ip", label: "IP", children: profile.ip },
    {
      key: "type",
      label: "网络类型",
      children: <Badge variant={profile.network?.type === "proxy" ? "warning" : "info"}>{networkTypeText[profile.network?.type || "unknown"] || profile.network?.type}</Badge>
    },
    { key: "location", label: "地理位置", children: location(profile) },
    { key: "asn", label: "ASN", children: valueOrDash(profile.asn?.number) },
    { key: "asnOrg", label: "ASN 组织", children: valueOrDash(profile.asn?.organization) },
    {
      key: "network",
      label: "网络段",
      children: networkCIDR ? (
        <div className="flex flex-wrap items-center gap-2">
          <span>{networkCIDR}</span>
          {onBlacklist ? (
            <Button
              size="sm"
              variant="destructive-outline"
              disabled={submitting !== null}
              onClick={() => confirmDanger({
                title: `封禁网络段 ${networkCIDR}？`,
                description: `ASN ${valueOrDash(profile.asn?.number)}${profile.asn?.organization ? ` / ${profile.asn.organization}` : ""} 下该网段的后续访问将被拒绝，并热更新到所有节点。`,
                confirmText: "确认封禁",
                cancelText: "取消",
                onConfirm: () => addToBlacklist(networkCIDR, "network")
              })}
            >
              封禁网段
            </Button>
          ) : null}
        </div>
      ) : valueOrDash(profile.asn?.network)
    },
    { key: "isp", label: "ISP", children: valueOrDash(profile.network?.isp) },
    { key: "org", label: "组织", children: valueOrDash(profile.network?.organization) },
    { key: "conn", label: "连接类型", children: valueOrDash(profile.network?.connectionType) },
    { key: "user", label: "用户类型", children: valueOrDash(profile.network?.userType) },
    {
      key: "traits",
      label: "代理/托管标记",
      children: traits.length > 0
        ? <span className="flex flex-wrap gap-1">{traits.map(([name]) => <Badge key={name} variant="secondary">{name}</Badge>)}</span>
        : "-"
    }
  ] : [];

  return (
    <Sheet open={open} onOpenChange={(next) => { if (!next) onClose(); }}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-[560px]">
        <SheetHeader className="pe-8">
          <SheetTitle>IP 详情</SheetTitle>
          <SheetDescription className="sr-only">查看 IP 的网络、位置与 ASN 资料，并可加入黑名单。</SheetDescription>
          {profile && onBlacklist ? (
            <div>
              <Button variant="destructive-outline" loading={submitting === "ip"} disabled={submitting === "network"} onClick={() => addToBlacklist(profile.ip, "ip")}>
                添加此 IP 到黑名单
              </Button>
            </div>
          ) : null}
        </SheetHeader>
        <div className="mt-4 flex flex-col gap-4">
          {loading ? <div className="flex justify-center"><Spin /></div> : null}
          {error ? (
            <Alert variant="warning">
              <AlertTitle>IP 资料暂时不可用</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          {!profile ? (loading ? null : <Empty description={error ? "可稍后重试" : "暂无 IP 资料"} />) : <Descriptions bordered column={1} items={items} />}
        </div>
      </SheetContent>
    </Sheet>
  );
};

export default IPProfileDrawer;
