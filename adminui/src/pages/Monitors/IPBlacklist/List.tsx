import { IIPBlacklistRule, IIPProfile, ipBlacklistRemove, ipBlacklistSearch, ipBlacklistUpsert } from "@/api/accessControl";
import { LocalTime } from "@/componets/LocalTime";
import PagedTableList from "@/componets/PagedTableList";
import { PageHeader } from "@ant-design/pro-components";
import { Button, Input, notification, Popconfirm, Space, Switch, Tag, Typography } from "antd";
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
  const reload = () => setRefreshTable(Math.random());

  const addRule = async () => {
    const value = cidr.trim();
    if (!value) {
      notification.warning({ message: "请输入 IP 或 CIDR" });
      return;
    }
    setAdding(true);
    try {
      const response = await ipBlacklistUpsert({ cidr: value });
      notification.success({ message: "黑名单规则已生效", description: `${response.data.cidr} 将在所有节点热更新。` });
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
      notification.success({ message: "已移出黑名单", description: `${record.cidr} 的历史拦截数据已保留。` });
      reload();
    } catch {
      // FetchJson already presents the service error.
    }
  };

  const addProfileRule = async (value: string, scope: IPBlacklistScope) => {
    try {
      const response = await ipBlacklistUpsert({ cidr: value });
      const label = scope === "network" ? "网络段" : "IP";
      notification.success({ message: `${label}黑名单规则已生效`, description: `${response.data.cidr} 将在所有节点热更新。` });
      reload();
    } catch {
      // FetchJson already presents the service error.
    }
  };

  const initTableColumn = (columns: any[]) => {
    columns.push({ title: "CIDR", dataIndex: "cidr", key: "cidr", width: 190, render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> });
    columns.push({
      title: "网络类型",
      key: "networkType",
      width: 100,
      render: (_: unknown, record: IIPBlacklistRule) => {
        const type = record.profile?.network?.type || "unknown";
        return <Tag color={type === "proxy" ? "orange" : "blue"}>{networkTypeText[type] || type}</Tag>;
      }
    });
    columns.push({ title: "所属/位置", key: "location", width: 220, render: (_: unknown, record: IIPBlacklistRule) => profileLocation(record.profile) });
    columns.push({ title: "ASN / 组织", key: "asn", width: 230, render: (_: unknown, record: IIPBlacklistRule) => record.profile?.asn?.organization || record.profile?.network?.organization || "-" });
    columns.push({ title: "累计拦截", dataIndex: "blockedCount", key: "blockedCount", width: 106, render: (value: number | string) => String(value ?? 0) });
    columns.push({ title: "最后拦截", dataIndex: "lastBlockedAt", key: "lastBlockedAt", width: 180, render: (value: number | string | null) => <LocalTime timestamp={value} /> });
    columns.push({ title: "创建时间", dataIndex: "createdAt", key: "createdAt", width: 180, render: (value: number | string) => <LocalTime timestamp={value} /> });
    columns.push({
      title: "状态",
      key: "status",
      width: 84,
      render: (_: unknown, record: IIPBlacklistRule) => record.isActive ? <Tag color="red">生效中</Tag> : <Tag>已移除</Tag>
    });
    columns.push({
      title: "操作",
      key: "operation",
      width: 168,
      render: (_: unknown, record: IIPBlacklistRule) => (
        <Space size={4}>
          <Button size="small" onClick={() => { setProfile(record.profile); setProfileOpen(true); }}>IP 详情</Button>
          {record.isActive ? (
            <Popconfirm title={`移出 ${record.cidr}？`} description="历史记录和拦截计数会保留。" okText="移出" cancelText="取消" onConfirm={() => removeRule(record)}>
              <Button size="small" danger>移出</Button>
            </Popconfirm>
          ) : null}
        </Space>
      )
    });
  };

  return (
    <PageHeader
      title="IP 黑名单"
      subTitle="全局规则会在所有代理节点热更新。支持 IPv4、IPv6、单 IP 与 CIDR，ACME challenge 保持绕过。"
      tags={
        <Space wrap>
          <Input.Search
            placeholder="输入 IP 或 CIDR，例如 203.0.113.4 / 2001:db8::/32"
            value={cidr}
            onChange={(event) => setCIDR(event.target.value)}
            onSearch={addRule}
            enterButton="添加到黑名单"
            loading={adding}
            style={{ width: 340 }}
          />
          <Input.Search placeholder="搜索 CIDR" value={keyword} onChange={(event) => setKeyword(event.target.value)} onSearch={reload} style={{ width: 180 }} />
          <Space size={4}>
            <Switch size="small" checked={includeRemoved} onChange={setIncludeRemoved} />
            <Typography.Text>显示已移除规则</Typography.Text>
          </Space>
          <Button onClick={reload}>刷新</Button>
        </Space>
      }
    >
      <PagedTableList
        pkFieldName="id"
        searchAction={ipBlacklistSearch}
        realodTableData={refreshTable}
        initTableColumn={initTableColumn}
        externalFilterCondition={filters}
      />
      <IPProfileDrawer open={profileOpen} profile={profile} onClose={() => setProfileOpen(false)} onBlacklist={addProfileRule} />
    </PageHeader>
  );
};

export default IPBlacklistList;
