import { getResourceCatalog, IResourceCatalog, IResourceHost, ResourceCatalogStatus, ResourceCatalogWebsiteAssociation } from "@/api/analytics";
import { DEFAULT_PAGE_INDEX, DEFAULT_PAGE_SIZE, PAGE_SIZE_OPTIONS } from "@/api/apiTypes";
import { LocalTime } from "@/componets/LocalTime";
import { DataToolbar, SignalTag, WorkbenchPage } from "@/componets/Workbench";
import { Alert, Button, Input, Select, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import React, { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { websiteViewPath } from "./websiteViewRoute";

const HostCatalog: React.FC = () => {
  const navigate = useNavigate();
  const [host, setHost] = useState("");
  const [status, setStatus] = useState<ResourceCatalogStatus>();
  const [websiteAssociation, setWebsiteAssociation] = useState<ResourceCatalogWebsiteAssociation>();
  const [catalog, setCatalog] = useState<IResourceCatalog>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const load = async (pageIndex = DEFAULT_PAGE_INDEX, pageSize = DEFAULT_PAGE_SIZE) => { setLoading(true); setError(""); try { const result = await getResourceCatalog({ host, status, websiteAssociation, pageIndex, pageSize }); setCatalog(result.data); } catch (reason) { setError(reason instanceof Error ? reason.message : "无法加载资源目录，请稍后重试。"); } finally { setLoading(false); } };
  useEffect(() => { load(); }, []);
  const columns: ColumnsType<IResourceHost> = [
    { title: "Host", dataIndex: "host", render: (value: string) => <Button type="link" style={{ padding: 0 }} onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(value)}`)}>{value}</Button> },
    { title: "状态", width: 116, render: (_, record) => <SignalTag tone={record.managementStatus === "managed" ? "success" : "warning"}>{record.managementStatus === "managed" ? "已托管" : "未托管"}</SignalTag> },
    { title: "关联网站", render: (_, record) => record.websites.length ? record.websites.map((website) => <Button key={website.id} type="link" size="small" onClick={() => navigate(websiteViewPath(website.id))}>{website.name || website.id}</Button>) : "—" },
    { title: "证书", width: 150, render: (_, record) => record.certificate ? <SignalTag tone="success">有效至 <LocalTime timestamp={record.certificate.notAfter} /></SignalTag> : "—" },
    { title: "最近活动", width: 180, render: (_, record) => record.lastActivity ? <LocalTime timestamp={record.lastActivity} /> : "—" }
  ];
  return <WorkbenchPage eyebrow="OBSERVABILITY / HOSTS" title="Host 资源" description="汇总实际观测到的 Host、关联站点、证书和最近活动。配置请从关联站点进入。">
    <DataToolbar actions={<Button type="primary" onClick={() => load(1)}>搜索</Button>}>
      <Input value={host} onChange={(event) => setHost(event.target.value)} onPressEnter={() => load(1)} placeholder="Host（支持模糊搜索）" style={{ width: 280 }} />
      <Select<ResourceCatalogStatus> allowClear placeholder="状态" value={status} onChange={setStatus} options={[{ value: "managed", label: "已托管" }, { value: "unmanaged", label: "未托管" }]} style={{ width: 132 }} />
      <Select<ResourceCatalogWebsiteAssociation> allowClear placeholder="关联网站" value={websiteAssociation} onChange={setWebsiteAssociation} options={[{ value: "associated", label: "已关联" }, { value: "unassociated", label: "未关联" }]} style={{ width: 144 }} />
    </DataToolbar>
    {error ? <Alert type="error" showIcon message={error} style={{ marginBottom: 'var(--space-3)' }} /> : null}
    <Table rowKey="host" size="small" columns={columns} dataSource={catalog?.records ?? []} loading={loading} pagination={{ current: catalog?.paging.pageIndex ?? DEFAULT_PAGE_INDEX, pageSize: catalog?.paging.pageSize ?? DEFAULT_PAGE_SIZE, total: catalog?.paging.total ?? 0, showSizeChanger: true, pageSizeOptions: [...PAGE_SIZE_OPTIONS], onChange: load }} />
  </WorkbenchPage>;
};
export default HostCatalog;
