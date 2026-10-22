import { Badge, Button, Card, CardContent, CardHeader, CardTitle, Descriptions, Dialog, DialogContent, DialogHeader, DialogTitle, FormList, Tooltip, TooltipContent, TooltipTrigger, toast } from '@modra/react';
import React, { useEffect, useRef, useState } from 'react';
import { useWatch } from 'react-hook-form';
import { webDomainCertificateStatus, webDomainRequestCertificate, type AcmeStatusResult, type WebsiteHost } from '@/api/website';
import { notifyError, notifySuccess } from '@/ui/notify';
import { RadioField, SwitchField, TextField } from '@/ui/siteFields';
import { AddButton, EditorCard, FieldGrid, padIndex, type WebsiteForm } from './WorkspaceParts';

const hostKey = (hostname: string) => hostname.trim().toLowerCase().replace(/\.$/, '');

type BadgeVariant = 'default' | 'secondary' | 'success' | 'warning' | 'destructive' | 'info' | 'outline';

const acmeStatusTag = (status: AcmeStatusResult | undefined) => {
  if (!status || !status.acmeOrder?.exists) return <Badge variant="secondary">暂无 ACME 订单</Badge>;
  const label: Record<string, { variant: BadgeVariant; text: string }> = {
    pending: { variant: 'warning', text: '等待处理' },
    locked: { variant: 'warning', text: '申请中' },
    authorizing: { variant: 'warning', text: '验证中' },
    finalizing: { variant: 'warning', text: '签发中' },
    retry_wait: { variant: 'warning', text: '失败，等待重试' },
    valid: { variant: 'success', text: '最近一次申请已完成' },
    failed: { variant: 'destructive', text: '最近一次申请失败' },
    canceled: { variant: 'secondary', text: '已取消' },
  };
  const info = label[status.acmeOrder.status ?? ''] ?? { variant: 'secondary' as const, text: status.acmeOrder.status ?? '未知' };
  const tag = <Badge variant={info.variant}>{info.text}</Badge>;
  const error = status.acmeOrder.errorMessage || status.errorMessage;
  return error ? (
    <Tooltip>
      <TooltipTrigger asChild><span>{tag}</span></TooltipTrigger>
      <TooltipContent className="max-w-xs">{error}</TooltipContent>
    </Tooltip>
  ) : tag;
};

const certificateStatusTag = (status: AcmeStatusResult | undefined, loading: boolean) => {
  if (loading) return <Badge variant="secondary">读取中…</Badge>;
  const certificate = status?.certificate;
  if (!certificate?.exists) return <Badge variant="secondary">未发现证书</Badge>;
  if (certificate.validity === 'valid') {
    const variant: BadgeVariant = certificate.remainingDays <= 7 ? 'destructive' : certificate.remainingDays <= 30 ? 'warning' : 'success';
    return <Badge variant={variant}>证书有效 · 剩余 {Math.max(0, certificate.remainingDays)} 天</Badge>;
  }
  return <Badge variant="destructive">{certificate.validity === 'expired' ? '证书已过期' : '证书尚未生效'}</Badge>;
};

interface HostRowProps {
  form: WebsiteForm;
  index: number;
  statusByHost: Record<string, AcmeStatusResult>;
  loadingByHost: Record<string, boolean>;
  errorByHost: Record<string, boolean>;
  requestingHost: string | null;
  onRemove: () => void;
  onShowCertificate: (certificate: NonNullable<AcmeStatusResult['certificate']>) => void;
  onRefresh: (hostname: string) => void;
  onRequestCertificate: (hostname: string) => void;
}

const HostRow: React.FC<HostRowProps> = ({ form, index, statusByHost, loadingByHost, errorByHost, requestingHost, onRemove, onShowCertificate, onRefresh, onRequestCertificate }) => {
  const binding = useWatch({ control: form.control, name: `hostBindings.${index}` }) as Partial<WebsiteHost> | undefined;
  const hostname = binding?.hostname;
  const key = hostname ? hostKey(hostname) : '';
  const status = key ? statusByHost[key] : undefined;
  const cert = status?.certificate;
  const order = status?.acmeOrder;
  const isPrimary = binding?.role === 'primary';

  const makePrimary = () => {
    const bindings = (form.getValues('hostBindings') ?? []).map((item, itemIndex) => ({
      ...item,
      role: itemIndex === index ? 'primary' as const : 'alias' as const,
      accessMode: itemIndex === index ? 'proxy' as const : item.accessMode,
    }));
    form.setValue('hostBindings', bindings, { shouldDirty: true });
  };

  return (
    <EditorCard
      eyebrow={`主机 ${padIndex(index)}`}
      hint={{ label: '主机配置', content: 'Host 决定请求是否进入当前站点，不决定请求转发到哪个服务。' }}
      actions={isPrimary
        ? <Badge variant="info">主站</Badge>
        : <Button type="button" variant="link" size="sm" onClick={makePrimary}>设为主站</Button>}
      onRemove={onRemove}
    >
      <FieldGrid>
        <TextField name={`hostBindings.${index}.hostname`} label="主机名" placeholder="app.example.com" required rules={{ validate: (value) => (typeof value === 'string' && value.trim() !== '') || '请输入主机名' }} />
        <RadioField name={`hostBindings.${index}.role`} label="角色" defaultValue="alias" options={[{ label: '主站', value: 'primary' }, { label: '别名', value: 'alias' }]} />
        <RadioField name={`hostBindings.${index}.accessMode`} label="访问方式" defaultValue="proxy" options={[{ label: '正常访问', value: 'proxy' }, { label: '跳转', value: 'redirect' }]} />
        <SwitchField name={`hostBindings.${index}.enabled`} label="状态" defaultValue onText="启用" offText="停用" />
        <SwitchField name={`hostBindings.${index}.certificateMode`} label="ACME 证书自动续期" defaultValue="manual" valueMap={{ on: 'request', off: 'manual' }} onText="自动续期" offText="不自动" />
        <div className="md:col-span-3">
          <Card>
            <CardHeader className="space-y-0 py-3"><CardTitle className="text-sm">安全状态</CardTitle></CardHeader>
            <CardContent className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                {certificateStatusTag(status, !!hostname && !!loadingByHost[key])}
                <Button type="button" size="sm" variant="outline" disabled={!cert?.exists} onClick={() => cert?.exists && onShowCertificate(cert)}>证书详情</Button>
                {acmeStatusTag(status)}
                <Button type="button" size="sm" variant="outline" disabled={!hostname || binding?.certificateMode !== 'request'} loading={requestingHost === hostname} onClick={() => hostname && onRequestCertificate(hostname)}>立即申请证书</Button>
                {order?.exists ? <a className="text-sm text-primary hover:underline" href={`/systems/acme?domain=${encodeURIComponent(hostname ?? '')}`} target="_blank" rel="noreferrer">查看 ACME 订单</a> : null}
                {errorByHost[key] ? <Button type="button" size="sm" variant="link" onClick={() => hostname && onRefresh(hostname)}>重试</Button> : null}
              </div>
              {status ? (
                <p className="text-sm text-muted-foreground">
                  状态更新时间：{new Date(status.checkedAt).toLocaleString()}
                  {order?.isInProgress && order.nextAttemptAt ? ` · 下次重试：${new Date(order.nextAttemptAt).toLocaleString()}` : ''}
                  {order?.errorMessage ? ` · ${order.errorMessage}` : ''}
                </p>
              ) : errorByHost[key] ? <p className="text-sm text-destructive">状态暂时不可用</p> : null}
            </CardContent>
          </Card>
        </div>
        {binding?.accessMode === 'redirect' ? (
          <>
            <RadioField name={`hostBindings.${index}.redirectStatusCode`} label="跳转状态码" defaultValue={308} options={[{ label: '308', value: 308 }, { label: '301', value: 301 }]} />
            <TextField name={`hostBindings.${index}.redirectTargetHost`} label="目标 Host（留空使用主站）" placeholder="www.example.com" />
          </>
        ) : null}
      </FieldGrid>
    </EditorCard>
  );
};

const certificateRequestOutcome = (data: { status: string; message: string }) => {
  const outcome: Record<string, { type: 'success' | 'info' | 'error'; message: string }> = {
    issued: { type: 'success', message: '证书申请成功' },
    already_valid: { type: 'info', message: '证书已有效，无需重新申请' },
    lock_busy: { type: 'info', message: '该域名正在被其他节点处理，请稍后重试' },
    order_in_progress: { type: 'info', message: '该域名已有一次申请正在进行中' },
    permanent_failure: { type: 'error', message: data.message || '证书申请失败' },
    temporary_failure: { type: 'error', message: data.message || '证书申请暂时失败，稍后会自动重试' },
    invalid_domain: { type: 'error', message: data.message || '域名格式无效' },
  };
  return outcome[data.status] ?? { type: 'info' as const, message: data.message || '已提交申请' };
};

export const HostSection: React.FC<{ form: WebsiteForm }> = ({ form }) => {
  const watchedBindings = (useWatch({ control: form.control, name: 'hostBindings' }) ?? []) as Array<Partial<WebsiteHost>>;
  const [statusByHost, setStatusByHost] = useState<Record<string, AcmeStatusResult>>({});
  const [loadingByHost, setLoadingByHost] = useState<Record<string, boolean>>({});
  const [errorByHost, setErrorByHost] = useState<Record<string, boolean>>({});
  const [certificateDetails, setCertificateDetails] = useState<AcmeStatusResult['certificate']>();
  const statusRef = useRef<Record<string, AcmeStatusResult>>({});
  const [requestingHost, setRequestingHost] = useState<string | null>(null);

  const refreshStatus = async (hostname: string) => {
    if (!hostname) return;
    const key = hostKey(hostname);
    setLoadingByHost((prev) => ({ ...prev, [key]: true }));
    try {
      const response = await webDomainCertificateStatus({ name: hostname });
      if (response.code === 200 && response.data) {
        statusRef.current = { ...statusRef.current, [key]: response.data! };
        setStatusByHost((prev) => ({ ...prev, [key]: response.data! }));
        setErrorByHost((prev) => ({ ...prev, [key]: false }));
      } else setErrorByHost((prev) => ({ ...prev, [key]: true }));
    } catch { setErrorByHost((prev) => ({ ...prev, [key]: true })); }
    finally { setLoadingByHost((prev) => ({ ...prev, [key]: false })); }
  };
  const refreshAll = () => {
    const bindings: Array<{ hostname?: string }> = form.getValues('hostBindings') ?? [];
    const currentKeys = new Set(bindings.map((binding) => binding.hostname?.trim() ? hostKey(binding.hostname) : '').filter(Boolean));
    statusRef.current = Object.fromEntries(Object.entries(statusRef.current).filter(([key]) => currentKeys.has(key)));
    setStatusByHost((prev) => Object.fromEntries(Object.entries(prev).filter(([key]) => currentKeys.has(key))));
    bindings.forEach((binding) => { if (binding.hostname?.trim()) void refreshStatus(binding.hostname); });
  };
  useEffect(() => {
    refreshAll();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [watchedBindings.map((binding) => binding?.hostname ?? '').join('|')]);
  useEffect(() => {
    const timer = window.setInterval(() => {
      if (Object.values(statusRef.current).some((status) => status.acmeOrder?.isInProgress)) refreshAll();
    }, 15000);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const requestCertificate = async (hostname: string) => {
    if (!hostname || requestingHost) return;
    setRequestingHost(hostname);
    try {
      const response = await webDomainRequestCertificate({ name: hostname });
      if (response.code === 200 && response.data) {
        const result = certificateRequestOutcome(response.data);
        if (result.type === 'success') notifySuccess('证书申请', result.message);
        else if (result.type === 'error') notifyError('证书申请', result.message);
        else toast({ title: '证书申请', description: result.message, duration: 5000 });
      } else {
        notifyError('证书申请失败', response.errMsg || '请求未能完成，请稍后重试。');
      }
    } finally {
      await refreshStatus(hostname);
      setRequestingHost(null);
    }
  };

  const details = certificateDetails;
  return (
    <>
      <FormList control={form.control} name="hostBindings">
        {({ fields, add, remove }) => (
          <div className="flex flex-col gap-3">
            {fields.map((field, index) => (
              <HostRow
                key={field.id}
                form={form}
                index={index}
                statusByHost={statusByHost}
                loadingByHost={loadingByHost}
                errorByHost={errorByHost}
                requestingHost={requestingHost}
                onRemove={() => remove(index)}
                onShowCertificate={setCertificateDetails}
                onRefresh={(hostname) => void refreshStatus(hostname)}
                onRequestCertificate={(hostname) => void requestCertificate(hostname)}
              />
            ))}
            <AddButton onClick={() => add({ enabled: true, role: 'alias', accessMode: 'proxy', redirectStatusCode: 308, certificateMode: 'manual' } as WebsiteHost)}>添加主机</AddButton>
          </div>
        )}
      </FormList>
      <Dialog open={!!details} onOpenChange={(open) => { if (!open) setCertificateDetails(undefined); }}>
        <DialogContent>
          <DialogHeader><DialogTitle>证书详情</DialogTitle></DialogHeader>
          {details ? (
            <Descriptions
              bordered
              column={1}
              items={[
                { key: 'san', label: 'Host / SAN', children: details.dnsNames.join(', ') || '—' },
                { key: 'validity', label: '状态', children: details.validity },
                { key: 'issuer', label: '签发者', children: details.issuer || '—' },
                { key: 'source', label: '来源', children: details.source || '—' },
                { key: 'serial', label: '序列号', children: details.serialNumber || '—' },
                { key: 'fingerprint', label: 'SHA-256 指纹', children: <span className="break-all">{details.fingerprint || '—'}</span> },
                { key: 'period', label: '有效期', children: `${new Date(details.notBefore).toLocaleString()} → ${new Date(details.notAfter).toLocaleString()}` },
                { key: 'remaining', label: '剩余时间', children: `${details.remainingDays} 天` },
                { key: 'active', label: '当前活动证书', children: details.isActive ? '是' : '否' },
              ]}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
};
