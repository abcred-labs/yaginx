import { getResourceCatalog, IResourceCatalog, IResourceHost, ResourceCatalogStatus, ResourceCatalogWebsiteAssociation } from "@/api/analytics";
import { acmeOrdersPath } from "@/utils/analyticsLinks";
import { DEFAULT_PAGE_INDEX, DEFAULT_PAGE_SIZE, PAGE_SIZE_OPTIONS } from "@/api/apiTypes";
import { LocalTime } from "@/components/LocalTime";
import { DataToolbar, SignalTag, WorkbenchPage } from "@/ui/workbench";
import { MonAlert } from "@/ui/monParts";
import { webDomainDelete } from "@/api/website";
import { confirmDanger } from "@/ui/confirmAction";
import { notifySuccess } from "@/ui/notify";
import { certificateDaysLeft, certificateTone } from "./hostCertificate";
import { Button, DataTable, Input, Select, SelectClearButton, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@modra/react";
import type { ColumnDef } from "@tanstack/react-table";
import React, { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { websiteViewPath } from "./websiteViewRoute";

const AUTO_RENEW_LABELS = { off: "未开启", active: "自动申请", stopped: "已停止" } as const;
const ORDER_STATUS_LABELS: Record<string, string> = { pending: "等待处理", locked: "申请中", authorizing: "验证中", finalizing: "签发中", retry_wait: "等待重试", valid: "已完成", failed: "失败", canceled: "已取消" };

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
  const removeHost = async (hostName: string) => {
    const response = await webDomainDelete({ name: hostName });
    if (response.code !== 200) return; // 全局请求层已提示错误
    notifySuccess("已删除", hostName);
    await load(catalog?.paging.pageIndex ?? DEFAULT_PAGE_INDEX, catalog?.paging.pageSize ?? DEFAULT_PAGE_SIZE);
  };
  const columns: ColumnDef<IResourceHost, unknown>[] = [
    { id: "host", header: "Host", cell: ({ row }) => <Button variant="link" className="h-auto p-0" onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(row.original.host)}`)}>{row.original.host}</Button> },
    { id: "status", header: "状态", meta: { minWidth: 116 }, cell: ({ row }) => <SignalTag tone={row.original.managementStatus === "managed" ? "success" : "warning"}>{row.original.managementStatus === "managed" ? "已托管" : "未托管"}</SignalTag> },
    { id: "websites", header: "关联网站", cell: ({ row }) => row.original.websites.length ? <div className="flex flex-wrap">{row.original.websites.map((website) => <Button key={website.id} variant="link" size="sm" onClick={() => navigate(websiteViewPath(website.id))}>{website.name || website.id}</Button>)}</div> : "—" },
    { id: "certificate", header: "证书", meta: { minWidth: 230 }, cell: ({ row }) => {
      const certificate = row.original.certificate;
      if (!certificate) return "—";
      const daysLeft = certificateDaysLeft(certificate.notAfter);
      return <div className="flex flex-wrap items-center gap-1">
        <SignalTag tone={certificateTone(daysLeft)}>有效至 <LocalTime timestamp={certificate.notAfter} /></SignalTag>
        <span className={daysLeft >= 45 ? "text-success" : daysLeft >= 15 ? "text-warning" : "text-destructive"}>{daysLeft >= 0 ? `剩余 ${daysLeft} 天` : `已过期 ${-daysLeft} 天`}</span>
      </div>;
    } },
    { id: "autoRenew", header: "自动申请", meta: { minWidth: 220 }, cell: ({ row }) => {
      const auto = row.original.autoRenew;
      if (!auto) return "—";
      const order = auto.latestOrder;
      return <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-1">
          <SignalTag tone={auto.status === "active" ? "success" : auto.status === "stopped" ? "danger" : "neutral"}>{AUTO_RENEW_LABELS[auto.status]}</SignalTag>
          {auto.status === "active" ? <span className="text-xs text-muted-foreground">今日 {auto.attemptsToday}/{auto.maxPerDay} 次</span> : null}
          {auto.failDays > 0 && auto.status !== "off" ? <span className="text-xs text-muted-foreground">连续失败 {auto.failDays} 天</span> : null}
        </div>
        {auto.status === "stopped" && auto.stopReason ? <span className="text-xs text-muted-foreground">{auto.stopReason}</span> : null}
        {order ? <Button variant="link" size="sm" className="h-auto justify-start p-0" onClick={() => navigate(acmeOrdersPath(row.original.host))}>订单 #{order.id} · {ORDER_STATUS_LABELS[order.status] ?? order.status}</Button> : null}
      </div>;
    } },
    { id: "lastActivity", header: "最近活动", meta: { minWidth: 180 }, cell: ({ row }) => row.original.lastActivity ? <LocalTime timestamp={row.original.lastActivity} /> : "—" },
    { id: "actions", header: "操作", meta: { minWidth: 80 }, cell: ({ row }) => {
      const bound = row.original.websites.length > 0;
      return <Button
        variant="link"
        size="sm"
        className="h-auto p-0 text-destructive"
        disabled={bound}
        title={bound ? "该 Host 已被站点绑定，请先在站点中移除" : undefined}
        onClick={() => confirmDanger({
          title: `删除 ${row.original.host}？`,
          description: "将移除该 Host 的配置、取消进行中的证书申请，并清除其流量统计与请求记录；已签发的证书不会被删除。",
          confirmText: "删除",
          cancelText: "取消",
          onConfirm: () => removeHost(row.original.host)
        })}
      >删除</Button>;
    } }
  ];
  const paging = { pageIndex: (catalog?.paging.pageIndex ?? DEFAULT_PAGE_INDEX) - 1, pageSize: catalog?.paging.pageSize ?? DEFAULT_PAGE_SIZE };
  return <WorkbenchPage eyebrow="CONFIG / HOSTS" title="Host 目录" description="Host 资产清单：托管状态、关联站点、证书与自动申请、最近活动。点击 Host 查看流量与请求，配置请从关联站点进入。">
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
