import {
  Badge, BulkActionBar, Button, FilterBar, FilterField, Input, RowActions,
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@modra/react';
import type { ColumnDef } from '@tanstack/react-table';
import { Download, Trash2, Upload } from '@/ui/icons';
import React, { useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { requireInt64String } from '@/api/identifier';
import { websiteBatchDelete, websiteDelete, websiteSearch, type IWebsiteSearchRequest, type Website } from '@/api/website';
import { confirmDanger } from '@/ui/confirmAction';
import { notifyError, notifySuccess } from '@/ui/notify';
import ServerDataTable from '@/ui/ServerDataTable';
import { SectionCard, WorkbenchPage } from '@/ui/workbench';
import { buildWebsiteSearchRequest, websiteBatchDeleteResultText, websiteListOperationPath } from './websiteListState';
import { websiteAnalyticsPath } from '@/utils/analyticsLinks';

type CertificateFilter = 'all' | 'true' | 'false';
interface FilterValues { domain: string; upstream: string; hasCertificate: CertificateFilter }
const EMPTY_FILTERS: FilterValues = { domain: '', upstream: '', hasCertificate: 'all' };

const searchWebsites = (request: Record<string, unknown>) => websiteSearch(request as IWebsiteSearchRequest);
const getWebsiteRowId = (row: Website) => row.id;

const toSearchRequest = (values: FilterValues): IWebsiteSearchRequest => buildWebsiteSearchRequest({
  domain: values.domain,
  upstream: values.upstream,
  hasCertificate: values.hasCertificate === 'all' ? undefined : values.hasCertificate === 'true',
});

const List: React.FC = () => {
  const navigate = useNavigate();
  const [reloadKey, setReloadKey] = useState(0);
  const [draftFilters, setDraftFilters] = useState<FilterValues>(EMPTY_FILTERS);
  const [appliedFilters, setAppliedFilters] = useState<Record<string, unknown>>({});
  const [selectedRowKeys, setSelectedRowKeys] = useState<string[]>([]);
  const [restoring, setRestoring] = useState(false);
  const restoreInputRef = useRef<HTMLInputElement>(null);
  const reload = () => setReloadKey((value) => value + 1);

  const applyFilters = () => {
    setAppliedFilters({ ...toSearchRequest(draftFilters) });
    setSelectedRowKeys([]);
  };
  const resetFilters = () => {
    setDraftFilters(EMPTY_FILTERS);
    setAppliedFilters({});
    setSelectedRowKeys([]);
  };

  const deleteOne = (record: Website) => {
    const pkValue = requireInt64String(record.id, '网站 ID');
    confirmDanger({
      title: '确认删除此资源？',
      description: '删除后将无法恢复。',
      confirmText: '删除',
      onConfirm: async () => {
        await websiteDelete({ id: pkValue });
        notifySuccess('站点已删除');
        reload();
      },
    });
  };

  const deleteSelected = () => {
    const ids = selectedRowKeys;
    confirmDanger({
      title: `确认删除已选择的 ${ids.length} 个站点？`,
      description: '删除后将无法恢复。',
      confirmText: '批量删除',
      onConfirm: async () => {
        const response = await websiteBatchDelete({ ids });
        notifySuccess(websiteBatchDeleteResultText(response.data));
        setSelectedRowKeys([]);
        reload();
      },
    });
  };

  const downloadConfig = () => {
    const newTab = window.open('/api/website/config/backup', '_blank');
    newTab?.focus();
    notifySuccess('已打开站点配置备份文件');
  };

  const restoreConfig = async (file: File) => {
    setRestoring(true);
    try {
      const body = new FormData();
      body.append('file', file);
      const response = await fetch('/api/website/config/restore', { method: 'POST', body, credentials: 'same-origin' });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      reload();
      notifySuccess(`${file.name} 已恢复`);
    } catch {
      notifyError(`${file.name} 恢复失败`);
    } finally {
      setRestoring(false);
    }
  };

  const columns = useMemo<ColumnDef<Website, unknown>[]>(() => [
    { id: 'name', header: '站点名称', size: 260, cell: ({ row }) => <Button variant="link" className="h-auto p-0" onClick={() => navigate(websiteAnalyticsPath(requireInt64String(row.original.id, '网站 ID')))}>{row.original.name}</Button> },
    {
      id: 'primaryHost',
      header: '主站 Host',
      size: 260,
      cell: ({ row }) => row.original.hostBindings?.find((host) => host.role === 'primary')?.hostname ?? row.original.defaultHost ?? '未配置',
    },
    {
      id: 'aliasHosts',
      header: '别名域名',
      size: 200,
      cell: ({ row }) => {
        const aliases = (row.original.hostBindings ?? []).filter((host) => host.role === 'alias').map((host) => host.hostname);
        if (aliases.length === 0) return <span className="text-muted-foreground">-</span>;
        const text = aliases.length > 2 ? `${aliases.slice(0, 2).join('、')} 等 ${aliases.length} 个` : aliases.join('、');
        return <span className="block max-w-[320px] truncate" title={aliases.join(', ')}>{text}</span>;
      },
    },
    {
      id: 'defaultDestination',
      header: '默认上游',
      size: 280,
      cell: ({ row }) => {
        const upstream = row.original.defaultDestination;
        return upstream ? <span className="block max-w-[320px] truncate" title={upstream}>{upstream}</span> : <span className="text-muted-foreground">-</span>;
      },
    },
    { id: 'proxyRules', header: '转发规则', size: 72, cell: ({ row }) => `${row.original.proxyRules?.length ?? 0} 条` },
    {
      id: 'hasCertificate',
      header: '证书',
      size: 76,
      cell: ({ row }) => row.original.hasCertificate ? <Badge variant="success">已配置</Badge> : <Badge variant="secondary">无</Badge>,
    },
    {
      id: 'ipAccessMode',
      header: 'IP 访问',
      size: 80,
      cell: ({ row }) => ({ disabled: '不限制', blacklist: '黑名单', whitelist: '白名单' } as const)[row.original.ipAccessMode ?? 'disabled'],
    },
    {
      id: 'isMaintenanceMode',
      header: '访问状态',
      size: 84,
      cell: ({ row }) => row.original.isMaintenanceMode ? <Badge variant="warning">维护中</Badge> : <Badge variant="success">正常</Badge>,
    },
    {
      id: 'actions',
      header: '操作',
      size: 150,
      cell: ({ row }) => {
        const record = row.original;
        const go = (operation: 'analytics' | 'edit' | 'childRules') => () => navigate(websiteListOperationPath(operation, requireInt64String(record.id, '网站 ID')));
        return (
          <RowActions
            moreLabel={`更多操作：${record.name}`}
            primary={[
              { key: 'analytics', label: '查看分析', onSelect: go('analytics') },
              { key: 'edit', label: '配置', onSelect: go('edit') },
            ]}
            more={[
              { key: 'childRules', label: '查看规则', onSelect: go('childRules') },
              { key: 'delete', label: '删除', destructive: true, onSelect: () => deleteOne(record) },
            ]}
          />
        );
      },
    },
    // 行内操作只依赖稳定的 navigate / setState，列定义无需随其重建
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], []);

  return (
    <WorkbenchPage
      eyebrow="CONFIGURATION / WEBSITES"
      title="网站"
      description="管理站点入口、默认上游和 TLS 覆盖范围。"
      extra={<>
        <Button variant="outline" onClick={reload}>刷新</Button>
        <Button onClick={() => navigate('../create')}>创建站点</Button>
      </>}
    >
      <SectionCard
        title="站点资源"
        description="选择站点进入工作区，集中维护主机与转发规则。"
        extra={<>
          <Button variant="outline" onClick={downloadConfig}><Download className="size-4" aria-hidden="true" />备份</Button>
          <Button variant="outline" loading={restoring} onClick={() => restoreInputRef.current?.click()}><Upload className="size-4" aria-hidden="true" />恢复</Button>
          <input
            ref={restoreInputRef}
            type="file"
            className="hidden"
            aria-label="选择站点配置备份文件"
            onChange={(event) => {
              const file = event.target.files?.[0];
              event.target.value = '';
              if (file) void restoreConfig(file);
            }}
          />
        </>}
      >
        <div className="flex flex-col gap-4">
          <FilterBar columns={4} onApply={applyFilters} onReset={resetFilters}>
            <FilterField label="域名" htmlFor="website-filter-domain">
              <Input id="website-filter-domain" placeholder="默认或附加域名" value={draftFilters.domain} onChange={(event) => setDraftFilters((current) => ({ ...current, domain: event.target.value }))} />
            </FilterField>
            <FilterField label="上游地址" htmlFor="website-filter-upstream">
              <Input id="website-filter-upstream" placeholder="默认上游地址" value={draftFilters.upstream} onChange={(event) => setDraftFilters((current) => ({ ...current, upstream: event.target.value }))} />
            </FilterField>
            <FilterField label="证书" htmlFor="website-filter-certificate">
              <Select value={draftFilters.hasCertificate} onValueChange={(value) => setDraftFilters((current) => ({ ...current, hasCertificate: value as CertificateFilter }))}>
                <SelectTrigger id="website-filter-certificate"><SelectValue placeholder="全部" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部</SelectItem>
                  <SelectItem value="true">有证书</SelectItem>
                  <SelectItem value="false">无证书</SelectItem>
                </SelectContent>
              </Select>
            </FilterField>
          </FilterBar>
          <BulkActionBar
            selectedCount={selectedRowKeys.length}
            selectionLabel={(count) => `已选择 ${count} 个站点`}
            onClearSelection={() => setSelectedRowKeys([])}
            actions={[{ key: 'delete', label: '批量删除', icon: <Trash2 className="size-4" aria-hidden="true" />, destructive: true, onSelect: deleteSelected }]}
          />
          <ServerDataTable<Website>
            columns={columns}
            searchAction={searchWebsites}
            getRowId={getWebsiteRowId}
            filter={appliedFilters}
            reloadKey={reloadKey}
            selectedRowKeys={selectedRowKeys}
            onSelectedRowKeysChange={setSelectedRowKeys}
          />
        </div>
      </SectionCard>
    </WorkbenchPage>
  );
};
export default List;
