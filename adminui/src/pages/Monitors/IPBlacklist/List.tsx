import { IIPBlacklistRule, IIPProfile, ipBlacklistRemove, ipBlacklistSearch, ipBlacklistUpsert } from "@/api/accessControl";
import { LocalTime } from "@/components/LocalTime";
import ServerDataTable from "@/ui/ServerDataTable";
import { confirmDanger } from "@/ui/confirmAction";
import { notifySuccess, notifyWarning } from "@/ui/notify";
import { DataToolbar, WorkbenchPage } from "@/ui/workbench";
import { Badge, Button, CopyableText, Input, RowActions, Switch } from "@modra/react";
import type { ColumnDef } from "@tanstack/react-table";
import React, { useMemo, useState } from "react";
import IPProfileDrawer, { type IPBlacklistScope } from "../IPProfileDrawer";

const networkTypeText: Record<string, string> = {
  datacenter: "机房",
  residential: "家庭",
  mobile: "移动网络",
  proxy: "代理",
  unknown: "未知"
};

const profileLocation = (profile?: IIPProfile) => [
  profile?.country?.name || profile?.country?.isoCode,
  profile?.subdivision?.name || profile?.subdivision?.isoCode,
  profile?.city?.name
].filter(Boolean).join(" / ") || "-";

const IPBlacklistList: React.FC = () => {
  const [refreshTable, setRefreshTable] = useState(0);
  const [cidr, setCIDR] = useState("");
  const [keyword, setKeyword] = useState("");
  const [includeRemoved, setIncludeRemoved] = useState(false);
  const [adding, setAdding] = useState(false);
  const [profileOpen, setProfileOpen] = useState(false);
  const [profile, setProfile] = useState<IIPProfile | null>(null);

  const filters = useMemo(() => ({ keyword: keyword.trim(), includeRemoved }), [includeRemoved, keyword]);
  const reload = () => setRefreshTable((current) => current + 1);

  const addRule = async () => {
    const value = cidr.trim();
    if (!value) {
      notifyWarning("请输入 IP 或 CIDR");
      return;
    }
    setAdding(true);
    try {
      const response = await ipBlacklistUpsert({ cidr: value, scope: "gateway" });
      notifySuccess("网关黑名单规则已生效", `${response.data.cidr} 将在所有节点热更新。`);
      setCIDR("");
      reload();
    } catch {
      // FetchJson already presents the service error.
    } finally {
      setAdding(false);
    }
  };

  const removeRule = async (record: IIPBlacklistRule) => {
    try {
      await ipBlacklistRemove({ id: record.id });
      notifySuccess("已移出网关黑名单", `${record.cidr} 的历史拦截数据已保留。`);
      reload();
    } catch {
      // FetchJson already presents the service error.
    }
  };

  const addProfileRule = async (value: string, scope: IPBlacklistScope, target: "gateway" | "site") => {
    try {
      if (target !== "gateway") return;
      const response = await ipBlacklistUpsert({ cidr: value, scope: "gateway" });
      const label = scope === "network" ? "网络段" : "IP";
      notifySuccess(`${label}网关黑名单规则已生效`, `${response.data.cidr} 将在所有节点热更新。`);
      reload();
    } catch {
      // FetchJson already presents the service error.
    }
  };

  const columns = useMemo<ColumnDef<IIPBlacklistRule, unknown>[]>(() => [
    { id: "cidr", header: "CIDR", meta: { minWidth: 190 }, cell: ({ row }) => <CopyableText>{row.original.cidr}</CopyableText> },
    { id: "scope", header: "生效范围", cell: () => <Badge variant="warning">网关</Badge> },
    {
      id: "networkType",
      header: "网络类型",
      cell: ({ row }) => {
        const type = row.original.profile?.network?.type || "unknown";
        return <Badge variant={type === "proxy" ? "warning" : "info"}>{networkTypeText[type] || type}</Badge>;
      }
    },
    { id: "location", header: "所属/位置", meta: { minWidth: 220 }, cell: ({ row }) => profileLocation(row.original.profile) },
    {
      id: "asn",
      header: "ASN / 组织",
      meta: { minWidth: 230 },
      cell: ({ row }) => row.original.profile?.asn?.organization || row.original.profile?.network?.organization || "-"
    },
    { id: "blockedCount", header: "累计拦截", cell: ({ row }) => String(row.original.blockedCount ?? 0) },
    {
      id: "lastBlockedAt",
      header: "最后拦截",
      cell: ({ row }) => <span className="whitespace-nowrap"><LocalTime timestamp={row.original.lastBlockedAt} /></span>
    },
    {
      id: "createdAt",
      header: "创建时间",
      cell: ({ row }) => <span className="whitespace-nowrap"><LocalTime timestamp={row.original.createdAt} /></span>
    },
    {
      id: "status",
      header: "状态",
      cell: ({ row }) => row.original.isActive ? <Badge variant="destructive">生效中</Badge> : <Badge variant="secondary">已移除</Badge>
    },
    {
      id: "operation",
      header: "操作",
      meta: { sticky: "end" },
      cell: ({ row }) => {
        const record = row.original;
        return (
          <RowActions
            primary={[
              { key: "profile", label: "IP 详情", onSelect: () => { setProfile(record.profile); setProfileOpen(true); } },
              {
                key: "remove",
                label: "移出",
                destructive: true,
                hidden: !record.isActive,
                onSelect: () => confirmDanger({
                  title: `移出 ${record.cidr}？`,
                  description: "历史记录和拦截计数会保留。",
                  confirmText: "移出",
                  cancelText: "取消",
                  onConfirm: () => removeRule(record)
                })
              }
            ]}
          />
        );
      }
    }
    // 操作回调只依赖稳定的 setState 与 API，列定义无需随其重建
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], []);

  return (
    <WorkbenchPage
      eyebrow="SECURITY / IP BLACKLIST"
      title="网关黑名单"
      description="规则会在所有代理节点、所有站点及未匹配 Host 的请求上生效。支持 IPv4、IPv6、单 IP 与 CIDR，ACME challenge 保持绕过。"
    >
      <DataToolbar>
        <form className="flex items-center gap-2" onSubmit={(event) => { event.preventDefault(); void addRule(); }}>
          <Input
            className="w-[340px]"
            placeholder="输入 IP 或 CIDR，例如 203.0.113.4 / 2001:db8::/32"
            aria-label="IP 或 CIDR"
            value={cidr}
            onChange={(event) => setCIDR(event.target.value)}
          />
          <Button type="submit" loading={adding}>添加到网关黑名单</Button>
        </form>
        <form className="flex items-center gap-2" onSubmit={(event) => { event.preventDefault(); reload(); }}>
          <Input className="w-[180px]" placeholder="搜索 CIDR" aria-label="搜索 CIDR" value={keyword} onChange={(event) => setKeyword(event.target.value)} />
          <Button type="submit" variant="outline">搜索</Button>
        </form>
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={includeRemoved} onCheckedChange={setIncludeRemoved} />
          显示已移除规则
        </label>
        <Button variant="outline" onClick={reload}>刷新</Button>
      </DataToolbar>
      <ServerDataTable<IIPBlacklistRule>
        columns={columns}
        getRowId={(row) => String(row.id)}
        searchAction={ipBlacklistSearch}
        reloadKey={refreshTable}
        filter={filters}
      />
      <IPProfileDrawer open={profileOpen} profile={profile} onClose={() => setProfileOpen(false)} onBlacklist={addProfileRule} />
    </WorkbenchPage>
  );
};

export default IPBlacklistList;
