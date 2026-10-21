import { getResourceCatalog, IResourceCatalog, IResourceHost, ResourceCatalogStatus, ResourceCatalogWebsiteAssociation } from "@/api/analytics";
import { DEFAULT_PAGE_INDEX, DEFAULT_PAGE_SIZE, PAGE_SIZE_OPTIONS } from "@/api/apiTypes";
import { LocalTime } from "@/components/LocalTime";
import { DataToolbar, SignalTag, WorkbenchPage } from "@/ui/workbench";
import { MonAlert } from "@/ui/monParts";
import { Button, DataTable, Input, Select, SelectClearButton, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@modra/react";
import type { ColumnDef } from "@tanstack/react-table";
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
  const columns: ColumnDef<IResourceHost, unknown>[] = [
    { id: "host", header: "Host", cell: ({ row }) => <Button variant="link" className="h-auto p-0" onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(row.original.host)}`)}>{row.original.host}</Button> },
    { id: "status", header: "状态", meta: { minWidth: 116 }, cell: ({ row }) => <SignalTag tone={row.original.managementStatus === "managed" ? "success" : "warning"}>{row.original.managementStatus === "managed" ? "已托管" : "未托管"}</SignalTag> },
    { id: "websites", header: "关联网站", cell: ({ row }) => row.original.websites.length ? <div className="flex flex-wrap">{row.original.websites.map((website) => <Button key={website.id} variant="link" size="sm" onClick={() => navigate(websiteViewPath(website.id))}>{website.name || website.id}</Button>)}</div> : "—" },
    { id: "certificate", header: "证书", meta: { minWidth: 150 }, cell: ({ row }) => row.original.certificate ? <SignalTag tone="success">有效至 <LocalTime timestamp={row.original.certificate.notAfter} /></SignalTag> : "—" },
    { id: "lastActivity", header: "最近活动", meta: { minWidth: 180 }, cell: ({ row }) => row.original.lastActivity ? <LocalTime timestamp={row.original.lastActivity} /> : "—" }
  ];
  const paging = { pageIndex: (catalog?.paging.pageIndex ?? DEFAULT_PAGE_INDEX) - 1, pageSize: catalog?.paging.pageSize ?? DEFAULT_PAGE_SIZE };
  return <WorkbenchPage eyebrow="OBSERVABILITY / HOSTS" title="Host 资源" description="汇总实际观测到的 Host、关联站点、证书和最近活动。配置请从关联站点进入。">
    <DataToolbar actions={<Button onClick={() => load(1)}>搜索</Button>}>
      <Input value={host} onChange={(event) => setHost(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") load(1); }} placeholder="Host（支持模糊搜索）" aria-label="Host" className="w-[280px]" />
      <Select value={status ?? ""} onValueChange={(value) => setStatus((value || undefined) as ResourceCatalogStatus | undefined)}>
        <SelectTrigger aria-label="状态" className="w-[132px]">
          <SelectValue placeholder="状态" />
          {status ? <SelectClearButton aria-label="清除状态" onClear={() => setStatus(undefined)} /> : null}
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="managed">已托管</SelectItem>
          <SelectItem value="unmanaged">未托管</SelectItem>
        </SelectContent>
      </Select>
      <Select value={websiteAssociation ?? ""} onValueChange={(value) => setWebsiteAssociation((value || undefined) as ResourceCatalogWebsiteAssociation | undefined)}>
        <SelectTrigger aria-label="关联网站" className="w-[144px]">
          <SelectValue placeholder="关联网站" />
          {websiteAssociation ? <SelectClearButton aria-label="清除关联网站" onClear={() => setWebsiteAssociation(undefined)} /> : null}
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="associated">已关联</SelectItem>
          <SelectItem value="unassociated">未关联</SelectItem>
        </SelectContent>
      </Select>
    </DataToolbar>
    {error ? <MonAlert type="error" message={error} /> : null}
    <DataTable
      columns={columns}
      data={catalog?.records ?? []}
      getRowId={(record) => record.host}
      loading={loading}
      manualPagination
      pagination={paging}
      onPaginationChange={(updater) => {
        const next = typeof updater === "function" ? updater(paging) : updater;
        void load(next.pageIndex + 1, next.pageSize);
      }}
      total={catalog?.paging.total ?? 0}
      showPagination
      pageSizeOptions={PAGE_SIZE_OPTIONS.map(Number)}
    />
  </WorkbenchPage>;
};
export default HostCatalog;
