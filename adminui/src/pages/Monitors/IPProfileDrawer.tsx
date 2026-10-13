import { IIPProfile } from "@/api/accessControl";
import { Alert, Button, Descriptions, Drawer, Empty, Popconfirm, Space, Spin, Tag, Typography } from "antd";
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

  return (
  <Drawer
    title="IP 详情"
    width={560}
    open={open}
    onClose={onClose}
    extra={profile && onBlacklist ? (
      <Button danger loading={submitting === "ip"} disabled={submitting === "network"} onClick={() => addToBlacklist(profile.ip, "ip")}>
        添加此 IP 到黑名单
      </Button>
    ) : null}
  >
    {loading ? <Spin /> : null}
    {error ? <Alert type="warning" showIcon message="IP 资料暂时不可用" description={error} style={{ marginBottom: 'var(--space-4)' }} /> : null}
    {!profile ? <Empty description={error ? "可稍后重试" : "暂无 IP 资料"} /> : (
      <Descriptions bordered size="small" column={1}>
        <Descriptions.Item label="IP">{profile.ip}</Descriptions.Item>
        <Descriptions.Item label="网络类型">
          <Tag color={profile.network?.type === "proxy" ? "orange" : "blue"}>{networkTypeText[profile.network?.type || "unknown"] || profile.network?.type}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="地理位置">{location(profile)}</Descriptions.Item>
        <Descriptions.Item label="ASN">{valueOrDash(profile.asn?.number)}</Descriptions.Item>
        <Descriptions.Item label="ASN 组织">{valueOrDash(profile.asn?.organization)}</Descriptions.Item>
        <Descriptions.Item label="网络段">
          {networkCIDR ? (
            <Space wrap>
              <Typography.Text>{networkCIDR}</Typography.Text>
              {onBlacklist ? (
                <Popconfirm
                  title={`封禁网络段 ${networkCIDR}？`}
                  description={`ASN ${valueOrDash(profile.asn?.number)}${profile.asn?.organization ? ` / ${profile.asn.organization}` : ""} 下该网段的后续访问将被拒绝，并热更新到所有节点。`}
                  okText="确认封禁"
                  cancelText="取消"
                  okButtonProps={{ danger: true, loading: submitting === "network" }}
                  onConfirm={() => addToBlacklist(networkCIDR, "network")}
                >
                  <Button size="small" danger disabled={submitting !== null}>封禁网段</Button>
                </Popconfirm>
              ) : null}
            </Space>
          ) : valueOrDash(profile.asn?.network)}
        </Descriptions.Item>
        <Descriptions.Item label="ISP">{valueOrDash(profile.network?.isp)}</Descriptions.Item>
        <Descriptions.Item label="组织">{valueOrDash(profile.network?.organization)}</Descriptions.Item>
        <Descriptions.Item label="连接类型">{valueOrDash(profile.network?.connectionType)}</Descriptions.Item>
        <Descriptions.Item label="用户类型">{valueOrDash(profile.network?.userType)}</Descriptions.Item>
        <Descriptions.Item label="代理/托管标记">
          {(() => {
            const traits = Object.entries(profile.traits || {}).filter(([, enabled]) => enabled);
            return traits.length > 0 ? traits.map(([name]) => <Tag key={name}>{name}</Tag>) : "-";
          })()}
        </Descriptions.Item>
      </Descriptions>
    )}
  </Drawer>
  );
};

export default IPProfileDrawer;
