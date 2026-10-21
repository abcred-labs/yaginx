import {
  Alert, AlertDescription, AlertTitle, Badge, Button, DataTable, Form, FormControl, FormDescription, FormField,
  FormItem, FormLabel, FormMessage, Input, InputNumber, Select, SelectContent, SelectItem, SelectTrigger,
  SelectValue, Switch, Tabs, TabsContent, TabsList, TabsTrigger, Textarea, confirm,
} from '@modra/react';
import type { ColumnDef } from '@tanstack/react-table';
import React, { useEffect, useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import type { Control } from 'react-hook-form';
import {
  getRuntimeSetting,
  getRuntimeSettingStatus,
  getRuntimeAccessContext,
  getSmtpSetting,
  saveRuntimeSetting,
  saveSmtpSetting,
  testSendSmtpSetting,
  type SmtpSettings,
  type RuntimeSetting,
  type RuntimeAccessContext,
} from '@/api/systemSetting';
import {
  disableGatewayNode,
  deleteGatewayNode,
  reloadGatewayNode,
  restartGatewayNode,
  searchGatewayNodes,
  type GatewayNode,
} from '@/api/gatewayNode';
import { notifyError, notifySuccess } from '@/ui/notify';
import { SectionCard, WorkbenchPage } from '@/ui/workbench';

const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value)) as T;

const merge = (target: Record<string, any>, source: Record<string, any>) => {
  for (const [key, value] of Object.entries(source)) {
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      target[key] = merge(target[key] && typeof target[key] === 'object' ? target[key] : {}, value);
    } else {
      target[key] = value;
    }
  }
  return target;
};

const statusVariant: Record<GatewayNode['status'], 'success' | 'warning' | 'destructive'> = {
  active: 'success',
  draining: 'warning',
  disabled: 'destructive',
};

const DEFAULT_CIDRS = ['127.0.0.1/32', '10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '100.64.0.0/10'];

const validCIDR = (value: string): boolean => {
  const [address, prefix, extra] = value.trim().split('/');
  if (!address || !prefix || extra !== undefined || !/^\d+$/.test(prefix)) return false;
  const bits = Number(prefix);
  const octets = address.split('.');
  if (octets.length === 4 && octets.every(item => /^\d+$/.test(item) && Number(item) <= 255)) return bits <= 32;
  return address.includes(':') && bits <= 128;
};

type FormValues = Record<string, any>;
type Ctl = Control<FormValues>;

interface BaseFieldProps {
  control: Ctl;
  name: string;
  label: string;
  className?: string;
  extra?: React.ReactNode;
}

const TextField: React.FC<BaseFieldProps & { placeholder?: string; type?: string; rules?: Record<string, any> }> = ({ control, name, label, className, extra, placeholder, type, rules }) => (
  <FormField
    control={control}
    name={name}
    rules={rules}
    render={({ field }) => (
      <FormItem className={className}>
        <FormLabel>{label}</FormLabel>
        <FormControl><Input {...field} value={field.value ?? ''} type={type} placeholder={placeholder} /></FormControl>
        {extra ? <FormDescription>{extra}</FormDescription> : null}
        <FormMessage />
      </FormItem>
    )}
  />
);

const NumberField: React.FC<BaseFieldProps & { min?: number; max?: number }> = ({ control, name, label, className, min, max }) => (
  <FormField
    control={control}
    name={name}
    render={({ field }) => (
      <FormItem className={className}>
        <FormLabel>{label}</FormLabel>
        <FormControl><InputNumber value={field.value ?? null} onValueChange={field.onChange} min={min} max={max} /></FormControl>
        <FormMessage />
      </FormItem>
    )}
  />
);

const SwitchField: React.FC<BaseFieldProps> = ({ control, name, label, className }) => (
  <FormField
    control={control}
    name={name}
    render={({ field }) => (
      <FormItem className={className}>
        <FormLabel>{label}</FormLabel>
        <FormControl><Switch checked={Boolean(field.value)} onCheckedChange={field.onChange} /></FormControl>
      </FormItem>
    )}
  />
);

const SelectField: React.FC<BaseFieldProps & { options: Array<{ value: string; label: string }> }> = ({ control, name, label, className, options }) => (
  <FormField
    control={control}
    name={name}
    render={({ field }) => (
      <FormItem className={className}>
        <FormLabel>{label}</FormLabel>
        <Select value={field.value ?? undefined} onValueChange={field.onChange}>
          <FormControl>
            <SelectTrigger><SelectValue /></SelectTrigger>
          </FormControl>
          <SelectContent>
            {options.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
          </SelectContent>
        </Select>
        <FormMessage />
      </FormItem>
    )}
  />
);

/** 表单值是 string[]，界面上按“每行一个”编辑。 */
const CidrField: React.FC<BaseFieldProps & { rows: number; below?: React.ReactNode }> = ({ control, name, label, className, rows, below }) => (
  <FormField
    control={control}
    name={name}
    render={({ field }) => (
      <FormItem className={className}>
        <FormLabel>{label}</FormLabel>
        <FormControl>
          <Textarea
            rows={rows}
            value={Array.isArray(field.value) ? field.value.join('\n') : ''}
            onChange={event => field.onChange(event.target.value.split(/\r?\n/))}
          />
        </FormControl>
        {below}
      </FormItem>
    )}
  />
);

const GRID = 'grid grid-cols-1 gap-4 md:grid-cols-3';

const RuntimeSettings: React.FC = () => {
  const form = useForm<FormValues>({ defaultValues: {} });
  const [runtime, setRuntime] = useState<RuntimeSetting | null>(null);
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState<any>(null);
  const [accessContext, setAccessContext] = useState<RuntimeAccessContext | null>(null);

  const load = async () => {
    const [setting, currentStatus, access] = await Promise.all([getRuntimeSetting(), getRuntimeSettingStatus(), getRuntimeAccessContext()]);
    if (setting.code === 200) {
      setRuntime(setting.data);
      if (setting.data?.config) form.reset(clone(setting.data.config));
    }
    if (currentStatus.code === 200) setStatus(currentStatus.data);
    if (access.code === 200) setAccessContext(access.data);
    // 已保存的共享配置可能不含这两个列表（服务端按默认值生效），此时用生效值回填，
    // 否则表单为空会被保存校验拒绝。
    const effective = access.code === 200 && access.data?.allowedCIDRs?.length ? access.data.allowedCIDRs : DEFAULT_CIDRS;
    const emptyList = (path: string) => !(form.getValues(path) ?? []).length;
    if (emptyList('management.allowedCIDRs')) form.setValue('management.allowedCIDRs', effective);
    if (emptyList('listeners.proxyProtocol.trustedCIDRs')) form.setValue('listeners.proxyProtocol.trustedCIDRs', DEFAULT_CIDRS);
  };

  useEffect(() => { void load(); }, []);

  const save = async () => {
    if (!(await form.trigger())) return;
    const values = clone(form.getValues());
    const cidrs = (values.management?.allowedCIDRs ?? []).map((cidr: string) => cidr.trim()).filter(Boolean);
    const trusted = (values.listeners?.proxyProtocol?.trustedCIDRs ?? []).map((cidr: string) => cidr.trim()).filter(Boolean);
    values.management = { ...values.management, allowedCIDRs: cidrs };
    values.listeners = { ...values.listeners, proxyProtocol: { ...values.listeners?.proxyProtocol, trustedCIDRs: trusted } };
    if (cidrs.length === 0 || cidrs.some((cidr: string) => !validCIDR(cidr))) {
      notifyError('请输入至少一个有效的管理 CIDR（每行一个）');
      return;
    }
    if (trusted.length === 0 || trusted.some((cidr: string) => !validCIDR(cidr))) {
      notifyError('请输入至少一个有效的 PROXY 信任网段 CIDR（每行一个）');
      return;
    }
    await confirm({
      title: '保存运行配置',
      description: '运行参数需要重启才能生效，保存后会按节点逐个 drain、重启并等待 ready。节点启动失败时会暂停后续节点。',
      confirmText: '保存并滚动重启',
      onConfirm: async () => {
        setLoading(true);
        try {
          const config = merge(clone(runtime?.config ?? {}), values);
          const response = await saveRuntimeSetting({ config, expectedRevision: runtime?.revision ?? null, restart: true });
          if (response.code === 200) {
            notifySuccess('运行配置已保存，滚动重启已排队');
            await load();
          } else if (response.msg) notifyError(response.msg);
        } finally {
          setLoading(false);
        }
      },
    });
  };

  const addCurrentIP = () => {
    if (!accessContext?.clientIP) return;
    const suffix = accessContext.clientIP.includes(':') ? '/128' : '/32';
    const current: string[] = form.getValues('management.allowedCIDRs') ?? [];
    if (!current.includes(accessContext.clientIP + suffix)) {
      form.setValue('management.allowedCIDRs', [...current, accessContext.clientIP + suffix], { shouldDirty: true });
    }
  };

  const { control } = form;

  return (
    <SectionCard title="运行参数" description="这些参数在 PostgreSQL 中集中保存，所有已批准节点共享。保存后按节点滚动重启。">
      <Form {...form} layout="vertical">
        <form onSubmit={(event) => { event.preventDefault(); void save(); }} noValidate>
          <Tabs defaultValue="proxy">
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
              <TabsList>
                <TabsTrigger value="proxy">代理连接</TabsTrigger>
                <TabsTrigger value="collection">数据采集</TabsTrigger>
                <TabsTrigger value="observability">证书与可观测性</TabsTrigger>
              </TabsList>
              <div className="flex flex-wrap items-center gap-2">
                <Button type="submit" loading={loading}>保存并滚动重启</Button>
                <Badge variant={status?.pendingRestart ? 'warning' : 'success'}>{status?.pendingRestart ? '存在待应用节点' : '节点配置已同步'}</Badge>
                {runtime ? <Badge variant="secondary">Revision {runtime.revision}</Badge> : null}
              </div>
            </div>
            <TabsContent value="proxy">
              <div className={GRID}>
                <TextField control={control} name="proxy.activityTimeout" label="活动超时" placeholder="5m" />
                <TextField control={control} name="proxy.responseHeaderTimeout" label="响应头超时" placeholder="30s" />
                <NumberField control={control} name="proxy.maxConcurrentRequests" label="最大并发请求" min={1} max={65536} />
                <NumberField control={control} name="proxy.maxRequestHeaderBytes" label="最大请求头字节数" min={8192} max={1048576} />
                <NumberField control={control} name="proxy.maxRequestBodyBytes" label="最大请求体字节数" min={0} />
                <NumberField control={control} name="proxy.maxIdleConnectionsPerHost" label="每主机最大空闲连接" min={1} max={4096} />
              </div>
            </TabsContent>
            <TabsContent value="collection">
              <div className={GRID}>
                <SwitchField control={control} name="trafficAggregation.enabled" label="启用流量聚合" />
                <TextField control={control} name="trafficAggregation.flushInterval" label="聚合刷新间隔" placeholder="15s" />
                <NumberField control={control} name="trafficAggregation.retentionDays" label="流量保留天数" min={1} max={3650} />
                <SwitchField control={control} name="requestHistory.enabled" label="启用请求历史" />
                <SelectField
                  control={control}
                  name="requestHistory.recordMode"
                  label="记录模式"
                  options={[{ value: 'ErrorsOnly', label: '仅错误' }, { value: 'AllMetadata', label: '全部元数据' }]}
                />
                <NumberField control={control} name="requestHistory.retentionHours" label="历史保留小时" min={1} max={8760} />
              </div>
            </TabsContent>
            <TabsContent value="observability">
              <div className={GRID}>
                <TextField
                  control={control}
                  name="acme.email"
                  label="ACME 账户邮箱"
                  placeholder="operator@example.com"
                  rules={{ pattern: { value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/, message: '请输入有效邮箱' } }}
                />
                <TextField
                  control={control}
                  name="acme.keyValueEndpointURL"
                  label="远程 Challenge Endpoint（可选）"
                  placeholder="http://other-yaginx:9090"
                  className="md:col-span-2"
                  extra="留空使用本节点管理 endpoint；填写后必须指向同集群其他 Yaginx 节点"
                />
                <TextField control={control} name="acme.renewBefore" label="提前续期" placeholder="168h" />
                <Alert variant="info" className="md:col-span-2">
                  <AlertTitle>ACME 与本地 Challenge endpoint 始终运行</AlertTitle>
                  <AlertDescription>证书申请范围在站点 Host 配置中选择“自动申请 ACME 证书”。Directory URL 默认使用 Let’s Encrypt production。</AlertDescription>
                </Alert>
                <SelectField control={control} name="observability.logLevel" label="日志级别" options={['debug', 'info', 'warn', 'error'].map(value => ({ value, label: value }))} />
                <SelectField control={control} name="observability.logFormat" label="日志格式" options={[{ value: 'json', label: 'JSON' }, { value: 'text', label: 'Text' }]} />
                <div className="hidden md:block" />
                <CidrField
                  control={control}
                  name="management.allowedCIDRs"
                  label="管理端允许访问网段（每行一个 CIDR）"
                  rows={3}
                  className="md:col-span-2"
                  below={accessContext ? (
                    <div className="mt-2 flex flex-col items-start gap-2">
                      <Alert variant={accessContext.clientAllowed ? 'success' : 'warning'} className="w-full">
                        <AlertTitle>{`当前请求 IP：${accessContext.clientIP}（${accessContext.clientAllowed ? '当前允许' : '当前不在允许网段'}）`}</AlertTitle>
                      </Alert>
                      <Button type="button" size="sm" variant="outline" onClick={addCurrentIP}>
                        添加当前 IP（{accessContext.clientIP.includes(':') ? '/128' : '/32'}）
                      </Button>
                      <span className="text-sm text-muted-foreground">服务端按 TCP 对端地址识别客户端；反向代理场景下这里显示的是代理地址。</span>
                    </div>
                  ) : null}
                />
                <CidrField control={control} name="listeners.proxyProtocol.trustedCIDRs" label="PROXY 信任网段（每行一个 CIDR）" rows={5} />
              </div>
            </TabsContent>
          </Tabs>
        </form>
      </Form>
    </SectionCard>
  );
};

const SmtpSettings: React.FC = () => {
  const form = useForm<FormValues>({
    defaultValues: { enabled: false, host: '', port: 25, useSsl: true, userName: '', password: '', fromEmail: '', fromName: '' },
  });
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testEmail, setTestEmail] = useState('');

  useEffect(() => {
    void getSmtpSetting().then(rsp => {
      if (rsp.code === 200) {
        form.reset({
          enabled: rsp.data?.enabled ?? false,
          host: rsp.data?.host ?? '',
          port: rsp.data?.port ?? 25,
          useSsl: rsp.data?.useSsl ?? true,
          userName: rsp.data?.userName ?? '',
          password: rsp.data?.password ?? '',
          fromEmail: rsp.data?.fromEmail ?? '',
          fromName: rsp.data?.fromName ?? '',
        });
        setTestEmail(rsp.data?.fromEmail ?? '');
      }
    });
  }, []);

  const save = async () => {
    setLoading(true);
    try {
      const rsp = await saveSmtpSetting(form.getValues() as SmtpSettings);
      if (rsp.code === 200) notifySuccess('通知设置已保存');
    } finally {
      setLoading(false);
    }
  };

  const test = async () => {
    setTesting(true);
    try {
      const rsp = await testSendSmtpSetting({ email: testEmail });
      if (rsp.code === 200) notifySuccess('测试邮件发送成功');
    } finally {
      setTesting(false);
    }
  };

  const values = form.control;

  return (
    <SectionCard title="SMTP 发信服务" description="用于密码恢复和系统通知。">
      <Form {...form} layout="vertical">
        <form className="flex flex-col gap-4" onSubmit={(event) => { event.preventDefault(); void save(); }} noValidate>
          <SwitchField control={values} name="enabled" label="启用 SMTP" />
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <TextField control={values} name="host" label="SMTP Host" className="md:col-span-2" />
            <NumberField control={values} name="port" label="SMTP Port" min={1} max={65535} />
          </div>
          <SwitchField control={values} name="useSsl" label="使用 SSL" />
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <TextField control={values} name="userName" label="用户名" />
            <TextField control={values} name="password" label="密码" type="password" />
            <TextField control={values} name="fromEmail" label="发件人邮箱" />
            <TextField control={values} name="fromName" label="发件人名称" />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="submit" loading={loading}>保存通知设置</Button>
            <Input className="w-64" placeholder="测试收件邮箱" value={testEmail} onChange={event => setTestEmail(event.target.value)} aria-label="测试收件邮箱" />
            <Button type="button" variant="outline" loading={testing} onClick={() => void test()}>发送测试邮件</Button>
          </div>
        </form>
      </Form>
    </SectionCard>
  );
};

const Nodes: React.FC = () => {
  const [rows, setRows] = useState<GatewayNode[]>([]);
  const [loading, setLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const response = await searchGatewayNodes({ pageIndex: 1, pageSize: 200 });
      if (response.code === 200) setRows(response.data.records);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const action = async (promise: Promise<unknown>, message: string) => {
    await promise;
    notifySuccess(message);
    await load();
  };

  const columns = useMemo<ColumnDef<GatewayNode, unknown>[]>(() => [
    {
      accessorKey: 'displayName',
      header: '节点',
      cell: ({ row }) => (
        <span className="flex flex-col">
          <strong>{row.original.displayName || '未命名节点'}</strong>
          <small className="text-muted-foreground">{row.original.nodeId}</small>
        </span>
      ),
      meta: { minWidth: 200 },
    },
    { accessorKey: 'status', header: '状态', cell: ({ row }) => <Badge variant={statusVariant[row.original.status]}>{row.original.status}</Badge> },
    { accessorKey: 'nodeNumber', header: '编号', cell: ({ row }) => row.original.nodeNumber ?? '-' },
    { accessorKey: 'softwareVersion', header: '版本' },
    { accessorKey: 'appliedRevision', header: '配置 Revision' },
    {
      id: 'restart',
      header: '重启状态',
      cell: ({ row }) => row.original.restartGeneration === row.original.observedRestartGeneration ? '正常' : '待重启',
    },
    {
      accessorKey: 'lastHeartbeatAt',
      header: '最近心跳',
      cell: ({ row }) => row.original.lastHeartbeatAt ? new Date(row.original.lastHeartbeatAt * 1000).toLocaleString() : '-',
    },
    {
      id: 'actions',
      header: '操作',
      meta: { sticky: 'end' },
      cell: ({ row }) => {
        const node = row.original;
        return (
          <span className="flex items-center gap-2 whitespace-nowrap">
            {node.status === 'active' ? <Button size="sm" variant="outline" onClick={() => void action(reloadGatewayNode(node.nodeId), '节点 reload 已请求')}>Reload</Button> : null}
            {node.status === 'active' ? <Button size="sm" variant="outline" onClick={() => void action(restartGatewayNode(node.nodeId), '节点重启已排队')}>重启</Button> : null}
            {node.status === 'active' ? <Button size="sm" variant="destructive-outline" onClick={() => void action(disableGatewayNode(node.nodeId, 'operator request'), '节点已禁用')}>禁用</Button> : null}
            {(node.status === 'disabled' || node.stale) ? <Button size="sm" variant="destructive-outline" onClick={() => void action(deleteGatewayNode(node.nodeId), '节点已删除')}>删除</Button> : null}
          </span>
        );
      },
    },
    // action 只依赖稳定的 setState 与 API，列定义无需随其重建
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], []);

  return (
    <SectionCard title="节点列表" description="节点启动后自动注册并承载业务流量。">
      <DataTable columns={columns} data={rows} getRowId={(row) => row.nodeId} loading={loading} showPagination pageSizeOptions={[10, 20, 50]} />
    </SectionCard>
  );
};

export type SystemSettingSection = 'runtime' | 'nodes' | 'notifications';

export const SystemSetting: React.FC<{ section?: SystemSettingSection }> = ({ section }) => {
  const pageMeta = section === 'runtime'
    ? { title: '运行配置', description: '管理所有已批准节点共享的运行参数。' }
    : section === 'nodes'
      ? { title: '节点与发布', description: '执行节点 reload、重启、禁用和清理操作。' }
      : section === 'notifications'
        ? { title: '通知', description: '配置密码恢复和系统通知使用的 SMTP 服务。' }
        : { title: '系统设置', description: '集中管理共享运行参数、通知服务和网关节点。' };
  return (
    <WorkbenchPage eyebrow="SYSTEM / CONTROL PLANE" title={pageMeta.title} description={pageMeta.description}>
      {section === 'runtime' ? <RuntimeSettings /> : section === 'nodes' ? <Nodes /> : section === 'notifications' ? <SmtpSettings /> : (
        <Tabs defaultValue="runtime">
          <TabsList>
            <TabsTrigger value="runtime">运行参数</TabsTrigger>
            <TabsTrigger value="nodes">节点列表</TabsTrigger>
            <TabsTrigger value="notifications">通知设置</TabsTrigger>
          </TabsList>
          <TabsContent value="runtime"><RuntimeSettings /></TabsContent>
          <TabsContent value="nodes"><Nodes /></TabsContent>
          <TabsContent value="notifications"><SmtpSettings /></TabsContent>
        </Tabs>
      )}
    </WorkbenchPage>
  );
};

export default SystemSetting;
