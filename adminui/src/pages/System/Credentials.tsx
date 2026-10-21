import {
  Alert, AlertDescription, AlertTitle, Badge, Button, CopyableText, DataTable, Dialog, DialogContent,
  DialogFooter, DialogHeader, DialogTitle, Form, FormControl, FormField, FormItem, FormLabel, FormMessage, Input,
} from '@modra/react';
import type { ColumnDef } from '@tanstack/react-table';
import { Ban, Plus, RefreshCw, X } from 'lucide-react';
import React, { useEffect, useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import {
  createAPICredential,
  listAPICredentials,
  resetAPICredential,
  revokeAPICredential,
  type APICredential,
  type APICredentialSecret,
} from '@/api/systemCredential';
import { confirmDanger } from '@/ui/confirmAction';
import { notifySuccess } from '@/ui/notify';
import { SectionCard, WorkbenchPage } from '@/ui/workbench';

const formatTime = (value: number | null) => value ? new Date(value * 1000).toLocaleString() : '—';

const SecretNotice: React.FC<{ secret: APICredentialSecret; onClose: () => void }> = ({ secret, onClose }) => {
  const copy = async () => {
    await navigator.clipboard.writeText(secret.token);
    notifySuccess('Token 已复制');
  };
  return (
    <Alert variant="warning">
      <div className="flex items-start justify-between gap-2">
        <AlertTitle>请立即复制并保存 Token，关闭后无法再次查看</AlertTitle>
        <button type="button" className="text-muted-foreground hover:text-foreground" aria-label="关闭" onClick={onClose}>
          <X className="size-4" aria-hidden="true" />
        </button>
      </div>
      <AlertDescription className="mt-2 flex flex-col gap-2">
        <Input type="password" readOnly value={secret.token} aria-label="Token" />
        <div><Button variant="outline" onClick={() => void copy()}>复制 Token</Button></div>
      </AlertDescription>
    </Alert>
  );
};

interface CreateValues {
  name: string;
  purpose: string;
}

const CreateDialog: React.FC<{ open: boolean; onClose: () => void; onCreated: (secret: APICredentialSecret) => void }> = ({ open, onClose, onCreated }) => {
  const form = useForm<CreateValues>({ defaultValues: { name: '', purpose: '' } });

  const close = () => {
    form.reset();
    onClose();
  };

  const submit = form.handleSubmit(async (values) => {
    const response = await createAPICredential(values);
    if (response.code === 200) {
      close();
      onCreated(response.data);
      notifySuccess('凭据已创建');
    }
  });

  const required = (message: string) => ({ validate: (value: string) => value.trim().length > 0 || message });

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) close(); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>创建 API 凭据</DialogTitle>
        </DialogHeader>
        <Form {...form} layout="vertical">
          <form className="flex flex-col gap-4" onSubmit={submit} noValidate>
            <FormField
              control={form.control}
              name="name"
              rules={required('请输入名称')}
              render={({ field }) => (
                <FormItem>
                  <FormLabel required>名称</FormLabel>
                  <FormControl><Input {...field} placeholder="生产 MCP" /></FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="purpose"
              rules={required('请输入用途')}
              render={({ field }) => (
                <FormItem>
                  <FormLabel required>用途</FormLabel>
                  <FormControl><Input {...field} placeholder="mcp" /></FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={close}>取消</Button>
              <Button type="submit" loading={form.formState.isSubmitting}>创建</Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
};

const Credentials: React.FC = () => {
  const [credentials, setCredentials] = useState<APICredential[]>([]);
  const [loading, setLoading] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [secret, setSecret] = useState<APICredentialSecret | null>(null);

  const load = async () => {
    setLoading(true);
    try {
      const response = await listAPICredentials();
      if (response.code === 200) setCredentials(response.data ?? []);
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, []);

  const reset = (item: APICredential) => confirmDanger({
    title: '重置凭据',
    description: `重置后 ${item.tokenPrefix} 立即失效，并生成新的 Token。`,
    confirmText: '重置',
    onConfirm: async () => {
      const response = await resetAPICredential(item.id);
      if (response.code === 200) {
        setSecret(response.data);
        await load();
        notifySuccess('凭据已重置');
      }
    },
  });

  const revoke = (item: APICredential) => confirmDanger({
    title: '吊销凭据',
    description: `吊销 ${item.tokenPrefix} 后不可恢复，确认继续吗？`,
    confirmText: '吊销',
    onConfirm: async () => {
      const response = await revokeAPICredential(item.id);
      if (response.code === 200) {
        await load();
        notifySuccess('凭据已吊销');
      }
    },
  });

  const columns = useMemo<ColumnDef<APICredential, unknown>[]>(() => [
    { accessorKey: 'name', header: '名称' },
    { accessorKey: 'purpose', header: '用途' },
    { accessorKey: 'tokenPrefix', header: 'Token 前缀', cell: ({ row }) => <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{row.original.tokenPrefix}</code> },
    {
      accessorKey: 'status',
      header: '状态',
      cell: ({ row }) => row.original.status === 'active'
        ? <Badge variant="success">启用</Badge>
        : <Badge variant="destructive">已吊销</Badge>,
    },
    { accessorKey: 'createdAt', header: '创建时间', cell: ({ row }) => formatTime(row.original.createdAt) },
    { accessorKey: 'updatedAt', header: '更新时间', cell: ({ row }) => formatTime(row.original.updatedAt) },
    {
      id: 'actions',
      header: '操作',
      cell: ({ row }) => row.original.status === 'active' ? (
        <span className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => reset(row.original)}>重置</Button>
          <Button size="sm" variant="destructive-outline" onClick={() => revoke(row.original)}>
            <Ban className="size-3.5" aria-hidden="true" />吊销
          </Button>
        </span>
      ) : null,
    },
    // reset/revoke 只依赖稳定的 setState 与 API，列定义无需随其重建
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], []);

  return (
    <WorkbenchPage eyebrow="SYSTEM / CREDENTIALS" title="API 凭据" description="统一管理 MCP 和未来接口使用的 Bearer token。">
      <SectionCard title="凭据列表" description="数据库只保存 hash。Token 只在创建或重置后显示一次。">
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-2">
            <Button onClick={() => setCreateOpen(true)}><Plus className="size-4" aria-hidden="true" />创建凭据</Button>
            <Button variant="outline" loading={loading} onClick={() => void load()}><RefreshCw className="size-4" aria-hidden="true" />刷新</Button>
          </div>
          {secret ? <SecretNotice secret={secret} onClose={() => setSecret(null)} /> : null}
          <DataTable columns={columns} data={credentials} getRowId={(row) => row.id} loading={loading} />
        </div>
      </SectionCard>
      <SectionCard title="MCP 使用说明" description="MCP 复用现有管理端口。创建用途为 mcp 的凭据后，在 Agent 中使用以下地址。">
        <div className="flex flex-col gap-2 text-sm">
          <CopyableText value={`${window.location.origin}/mcp`} monospace />
          <CopyableText value={`${window.location.origin}/.well-known/mcp.json`} monospace />
          <span className="text-muted-foreground">请求头格式：Authorization: Bearer &lt;token&gt;</span>
        </div>
      </SectionCard>
      <CreateDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(created) => { setSecret(created); void load(); }}
      />
    </WorkbenchPage>
  );
};

export default Credentials;
