import { Alert, AlertDescription, AlertTitle, Button, Card, CardContent, CardHeader, CardTitle, Empty, Form, FormList, Spin } from '@modra/react';
import { Save } from '@/ui/icons';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { NavLink, useNavigate, useParams } from 'react-router-dom';
import { websiteGet, websiteResponseID, websiteUpsert, type ProxyRule, type SimpleResponse, type WebsiteUpsertInput } from '@/api/website';
import { notifyError, notifySuccess } from '@/ui/notify';
import { LinesField, NumberField, RadioField, SiteFieldsProvider, SwitchField, TextAreaField, TextField } from '@/ui/siteFields';
import { InfoHint, SignalTag, WorkbenchPage } from '@/ui/workbench';
import { HostSection } from './HostSection';
import { hasDuplicateTransformKey, locationRewritePreset, proxyTransformKeys } from './proxyTransforms';
import { RoutesSection } from './RoutesSection';
import { TabRequestHistory } from './TabRequestHistory';
import { websiteUpdatePayload } from './websiteUpdatePayload';
import { AddButton, EditorCard, Eyebrow, FieldGrid, IconRemoveButton, padIndex, type WebsiteForm } from './WorkspaceParts';

type WorkspaceSection = 'overview' | 'hosts' | 'routes' | 'transforms' | 'responses' | 'access' | 'history';
const sections: Array<{ id: WorkspaceSection; label: string; path: string; hint: string }> = [
  { id: 'overview', label: '概览', path: '', hint: '站点入口、转发与访问策略' },
  { id: 'hosts', label: '主机', path: '/hosts', hint: '此站点接收的 Host' },
  { id: 'routes', label: '转发规则', path: '/routes', hint: '匹配请求并选择转发目标' },
  { id: 'transforms', label: '代理转换', path: '/transforms', hint: '改写路径、请求头、响应头与重定向 Location' },
  { id: 'responses', label: '静态响应', path: '/responses', hint: '无需上游的快速响应' },
  { id: 'access', label: '访问控制', path: '/access', hint: '网站级 IP 黑白名单' },
  { id: 'history', label: '请求跟踪', path: '/history', hint: '本站点的历史记录与成功采样' },
];

const TRANSFORM_KEYS_LIST_ID = 'website-proxy-transform-keys';

const TransformsSection: React.FC<{ form: WebsiteForm }> = ({ form }) => (
  <FormList control={form.control} name="proxyTransforms">
    {({ fields, add, remove }) => (
      <div className="flex flex-col gap-3">
        <Alert variant="info">
          <AlertTitle>代理转换作用于站点默认转发（Fallback 规则）</AlertTitle>
          <AlertDescription>带路径匹配的转发规则不继承这里的转换。同一个站点内键名不能重复，所以 Set 由 ResponseHeader、RequestHeader、ResponseLocationHost 等共用同一个值。</AlertDescription>
        </Alert>
        <Alert variant="success">
          <AlertTitle>重定向 Location 改写（ResponseLocationHost）</AlertTitle>
          <AlertDescription>上游返回 301/302/303/307/308 且 Location 为绝对地址时，若其 host 等于“ResponseLocationHost”的值（支持 *.example.com），则把 host 替换为“Set”的值，scheme、路径和签名参数保持不变。示例：ResponseLocationHost = pkg-containers.githubusercontent.com，Set = blob.example.com。</AlertDescription>
        </Alert>
        <datalist id={TRANSFORM_KEYS_LIST_ID}>
          {proxyTransformKeys.map((value) => <option key={value} value={value} />)}
        </datalist>
        {fields.map((field, index) => (
          <div key={field.id} className="grid grid-cols-[2rem_minmax(0,1fr)_2.25rem] items-start gap-2">
            <span className="pt-2 font-mono text-[11px] text-muted-foreground">{padIndex(index)}</span>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-[2fr_3fr]">
              <TextField
                name={`proxyTransforms.${index}.key`}
                placeholder="ResponseLocationHost"
                list={TRANSFORM_KEYS_LIST_ID}
                rules={{
                  validate: (value) => {
                    if (typeof value !== 'string' || value.trim() === '') return '请输入转换键';
                    if (hasDuplicateTransformKey(form.getValues('proxyTransforms'), value, index)) return '转换键不能重复';
                    return true;
                  },
                }}
              />
              <TextField name={`proxyTransforms.${index}.value`} placeholder="值" />
            </div>
            <IconRemoveButton label="删除转换" onClick={() => remove(index)} />
          </div>
        ))}
        <div className="flex flex-wrap gap-2">
          <AddButton block={false} onClick={() => add({ key: '', value: '' })}>添加转换</AddButton>
          <AddButton
            block={false}
            onClick={() => {
              const existing = (form.getValues('proxyTransforms') ?? []) as Array<{ key?: string }>;
              const keys = new Set(existing.map((item) => (item.key ?? '').trim().toLowerCase()));
              locationRewritePreset.filter((item) => !keys.has(item.key.toLowerCase())).forEach((item) => add({ ...item }));
            }}
          >
            添加 Location 改写
          </AddButton>
        </div>
      </div>
    )}
  </FormList>
);

const ResponseHeaders: React.FC<{ form: WebsiteForm; index: number }> = ({ form, index }) => (
  <FormList control={form.control} name={`simpleResponses.${index}.extraHeaders` as `simpleResponses.${number}.extraHeaders`}>
    {({ fields, add, remove }) => (
      <div className="flex flex-col gap-2">
        {fields.map((header, headerIndex) => (
          <div key={header.id} className="grid grid-cols-[1rem_minmax(0,1fr)_2.25rem] items-start gap-2">
            <span className="pt-2 font-mono text-[11px] text-muted-foreground">#</span>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              <TextField
                name={`simpleResponses.${index}.extraHeaders.${headerIndex}.key`}
                placeholder="X-Response-Header"
                rules={{ validate: (value) => (typeof value === 'string' && value.trim() !== '') || '请输入响应头名称' }}
              />
              <TextField name={`simpleResponses.${index}.extraHeaders.${headerIndex}.value`} placeholder="响应头值" />
            </div>
            <IconRemoveButton label="删除响应头" onClick={() => remove(headerIndex)} />
          </div>
        ))}
        <div><AddButton block={false} onClick={() => add({})}>添加响应头</AddButton></div>
      </div>
    )}
  </FormList>
);

const ResponsesSection: React.FC<{ form: WebsiteForm }> = ({ form }) => (
  <FormList control={form.control} name="simpleResponses">
    {({ fields, add, remove }) => (
      <div className="flex flex-col gap-3">
        {fields.map((field, index) => (
          <EditorCard
            key={field.id}
            eyebrow={`响应 ${padIndex(index)}`}
            hint={{ label: '静态响应', content: '直接返回内容，不经过上游。' }}
            onRemove={() => remove(index)}
          >
            <FieldGrid>
              <TextField name={`simpleResponses.${index}.path`} label="路径" placeholder="/health" required rules={{ validate: (value) => (typeof value === 'string' && value !== '') || '请输入路径' }} />
              <TextField name={`simpleResponses.${index}.contentType`} label="内容类型" placeholder="text/plain" defaultValue="text/plain" />
              <NumberField name={`simpleResponses.${index}.statusCode`} label="状态码" defaultValue={200} min={100} max={599} />
              <TextAreaField className="md:col-span-3" name={`simpleResponses.${index}.responseContent`} label="响应内容" rows={4} required rules={{ validate: (value) => (typeof value === 'string' && value !== '') || '请输入响应内容' }} />
              <div className="flex flex-col gap-2 md:col-span-3">
                <span className="text-sm font-medium">响应头</span>
                <ResponseHeaders form={form} index={index} />
              </div>
            </FieldGrid>
          </EditorCard>
        ))}
        <AddButton onClick={() => add({ contentType: 'text/plain', statusCode: 200 } as SimpleResponse)}>添加静态响应</AddButton>
      </div>
    )}
  </FormList>
);

const SubHeading: React.FC<React.PropsWithChildren> = ({ children }) => (
  <h3 className="border-b border-border pb-1 text-sm font-semibold text-foreground">{children}</h3>
);

const OverviewSection: React.FC<{ form: WebsiteForm; websiteId: string }> = ({ form, websiteId }) => {
  const navigate = useNavigate();
  const hosts = (useWatch({ control: form.control, name: 'hostBindings' }) ?? []) as Array<{ hostname?: string; enabled?: boolean; role?: string }>;
  const rules = (useWatch({ control: form.control, name: 'proxyRules' }) ?? []) as ProxyRule[];
  const maintenance = useWatch({ control: form.control, name: 'isMaintenanceMode' });
  const enabledHosts = hosts.filter((host) => host.enabled !== false);
  const primary = hosts.find((host) => host.role === 'primary' && host.enabled !== false);
  const fallback = rules.find((rule) => rule.isFallback && rule.isEnabled);
  return (
    <div className="flex flex-col gap-5">
      <Card className="border-primary/30 bg-primary/5">
        <CardHeader className="flex-row items-center justify-between gap-3 space-y-0">
          <CardTitle className="text-base">站点运行入口</CardTitle>
          <div className="flex items-center gap-1">
            <Button type="button" variant="link" size="sm" onClick={() => navigate(`/website/${websiteId}/hosts`)}>管理主机</Button>
            <Button type="button" variant="link" size="sm" onClick={() => navigate(`/website/${websiteId}/routes`)}>管理转发规则</Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1">
              <Eyebrow>PRIMARY HOST</Eyebrow>
              <strong className="text-xl">{primary?.hostname ?? '未配置'}</strong>
              <span className="text-sm text-muted-foreground">{enabledHosts.length} 个启用入口</span>
            </div>
            <div className="flex flex-col gap-1">
              <Eyebrow>FORWARDING</Eyebrow>
              <strong className="text-xl">{rules.length} 条规则</strong>
              <span className="text-sm text-muted-foreground">{fallback ? '已配置 Fallback' : '未配置 Fallback'}</span>
            </div>
          </div>
        </CardContent>
      </Card>
      <FieldGrid className="lg:grid-cols-2">
        <TextField name="name" label="站点名称" placeholder="生产 API 网关" required rules={{ required: '请输入站点名称' }} />
        <TextField name="webProxy" label="出站代理" placeholder="http://proxy.example:8888" />
        <TextField name="authorizationPolicy" label="访问策略" placeholder="例如 BasicAuthPolicy" />
      </FieldGrid>
      <div className="flex flex-col gap-3">
        <SubHeading>访问状态</SubHeading>
        <SwitchField name="isMaintenanceMode" label="维护模式（停止访问）" onText="维护中" offText="正常" />
        {maintenance ? (
          <Alert variant="warning">
            <AlertTitle>维护模式已开启</AlertTitle>
            <AlertDescription>保存后，匹配该站点的请求将直接返回 503，不会进入静态响应、转发规则或上游服务。ACME 证书校验不受影响。</AlertDescription>
          </Alert>
        ) : null}
      </div>
      <div className="flex flex-col gap-3">
        <SubHeading>连接行为</SubHeading>
        <div className="flex flex-wrap gap-x-10 gap-y-4">
          <SwitchField name="preserveOriginalHost" label="保留原始 Host" onText="保留" offText="替换" />
          <SwitchField name="allowInvalidTlsCertificate" label="忽略上游 TLS 证书错误" onText="忽略" offText="校验" />
          <SwitchField name="redirectHttpToHttps" label="HTTP 自动跳转 HTTPS" onText="开启" offText="关闭" />
        </div>
      </div>
      <FieldGrid className="lg:grid-cols-2">
        <TextField
          name="responseHeaderTimeout"
          label="上游响应头超时"
          placeholder="例如 600s"
          extra="上游在该时间内未返回响应头则返回 502。留空则使用全局设置（proxy.responseHeaderTimeout，默认 30s）；最大 1h。"
          rules={{ pattern: { value: /^(\d+(\.\d+)?(ms|s|m|h))+$/, message: '请输入时长，例如 600s 或 10m' } }}
        />
      </FieldGrid>
    </div>
  );
};

const AccessControlSection: React.FC = () => (
  <div className="flex flex-col gap-4">
    <div><InfoHint label="访问控制" content="访问控制只影响当前站点。ACME challenge 不受此设置影响。" /></div>
    <RadioField
      name="ipAccessMode"
      label="网站级模式"
      defaultValue="disabled"
      options={[{ label: '不限制', value: 'disabled' }, { label: '黑名单', value: 'blacklist' }, { label: '白名单', value: 'whitelist' }]}
    />
    <LinesField name="ipAccessCIDRs" label="IP / CIDR 列表" rows={8} extra="每行填写一个 IPv4、IPv6 地址或 CIDR 网段。" placeholder={'203.0.113.10\n2001:db8::/32'} />
  </div>
);

export const WebsiteWorkspace: React.FC<{ section: WorkspaceSection }> = ({ section }) => {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const form = useForm<WebsiteUpsertInput>({ mode: 'onChange', defaultValues: {} });
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [loadState, setLoadState] = useState<'loading' | 'ready' | 'missing' | 'error'>('loading');
  const [loadError, setLoadError] = useState('');
  const [websiteName, setWebsiteName] = useState('');
  const loadVersionRef = useRef(0);
  const requestRef = useRef<AbortController | null>(null);
  const readyRef = useRef(false);
  readyRef.current = loadState === 'ready';
  const active = useMemo(() => sections.find((item) => item.id === section)!, [section]);

  const cancelPendingLoad = () => {
    loadVersionRef.current += 1;
    requestRef.current?.abort();
    requestRef.current = null;
  };

  const load = async () => {
    cancelPendingLoad();
    const version = loadVersionRef.current + 1;
    loadVersionRef.current = version;
    const controller = new AbortController();
    requestRef.current = controller;
    setLoadState('loading');
    setLoadError('');
    try {
      const response = await websiteGet({ id }, { signal: controller.signal });
      if (version !== loadVersionRef.current) return;
      if (response.code !== 200) {
        setLoadError(response.code === 401 ? '登录已失效，请重新登录后再试。' : '无法加载该站点配置，请稍后重新加载。');
        setLoadState('error');
        return;
      }
      if (response.data === null || response.data === undefined) {
        form.reset({});
        setWebsiteName('');
        setDirty(false);
        setLoadState('missing');
        return;
      }
      const legacyHosts = response.data.defaultHost ? [response.data.defaultHost, ...(response.data.hosts ?? [])] : (response.data.hosts ?? []);
      const seenHosts = new Set<string>();
      const hostBindings = response.data.hostBindings?.length ? response.data.hostBindings : legacyHosts.filter((hostname) => { const key = hostname.toLowerCase().replace(/\.$/, ''); if (seenHosts.has(key)) return false; seenHosts.add(key); return true; }).map((hostname, index) => ({ hostname, enabled: true, role: index === 0 ? 'primary' as const : 'alias' as const, accessMode: 'proxy' as const, redirectStatusCode: 308 as const }));
      const legacyPool = response.data.defaultDestination ? [{ id: 'legacy-default', name: '兼容默认上游', strategy: 'weightedRoundRobin' as const, endpoints: [{ address: response.data.defaultDestination, host: response.data.defaultDestinationHost ?? undefined, weight: 1, enabled: true }]}] : [];
      const configuredPools = response.data.upstreamPools ?? [];
      const upstreamPools = response.data.defaultDestination && !configuredPools.some((pool) => pool.id === 'legacy-default')
        ? [...configuredPools, ...legacyPool]
        : (configuredPools.length ? configuredPools : legacyPool);
      const hasFallback = (response.data.proxyRules ?? []).some((rule) => rule.isEnabled && rule.isFallback);
      const loadedRules = (response.data.proxyRules ?? []).map((rule) => ({ ...rule, targetType: rule.upstreamPoolId ? 'pool' as const : 'direct' as const })).sort((a, b) => Number(Boolean(a.isFallback)) - Number(Boolean(b.isFallback)));
      const proxyRules = loadedRules.length ? (response.data.defaultDestination && !hasFallback ? [...loadedRules, { isEnabled: true, isFallback: true, upstreamPoolId: 'legacy-default', targetType: 'pool' as const }] : loadedRules) : (upstreamPools.length ? [{ isEnabled: true, isFallback: true, upstreamPoolId: upstreamPools[0].id, targetType: 'pool' as const }] : []);
      form.reset({ ...response.data, hostBindings, upstreamPools, proxyRules, requestHistory: response.data.requestHistory ?? { mode: 'inherit' } });
      setWebsiteName(response.data.name ?? '');
      setDirty(false);
      setLoadState('ready');
    } catch (error) {
      if (version !== loadVersionRef.current || (error instanceof Error && error.name === 'AbortError')) return;
      setLoadError('无法加载该站点配置，请检查网络连接或稍后重新加载。');
      setLoadState('error');
    }
  };
  useEffect(() => {
    void load();
    return cancelPendingLoad;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  // 仅用户编辑（带字段名的变更）才标记为未保存；reset 不带字段名，不会触发。
  useEffect(() => {
    const subscription = form.watch((_values, info) => {
      if (!readyRef.current || !info.name) return;
      setWebsiteName(form.getValues('name') ?? '');
      setDirty(true);
    });
    return () => subscription.unsubscribe();
  }, [form]);

  const save = async (values: WebsiteUpsertInput) => {
    if (loadState !== 'ready') return;
    const primaryHosts = (values.hostBindings ?? []).filter((host) => host.role === 'primary');
    if (primaryHosts.length > 1) {
      notifyError('主机配置无效', '每个站点最多只能有一个主站。');
      return;
    }
    if (primaryHosts.some((host) => host.accessMode === 'redirect')) {
      notifyError('主机配置无效', '主站不能配置为跳转模式。');
      return;
    }
    const fallbackRules = (values.proxyRules ?? []).filter((rule) => rule.isEnabled && rule.isFallback);
    if (fallbackRules.length > 1) {
      notifyError('转发规则无效', '每个站点最多只能有一个启用的 Fallback 规则。');
      return;
    }
    setSaving(true);
    cancelPendingLoad();
    try {
      const response = await websiteUpsert(websiteUpdatePayload(id, values));
      if (websiteResponseID(response) === null) return;
      setDirty(false);
      notifySuccess('站点配置已保存', '已使用服务端返回的最新配置同步工作区。');
      await load();
    }
    finally { setSaving(false); }
  };

  const content = section === 'hosts' ? <HostSection form={form} />
    : section === 'routes' ? <RoutesSection form={form} />
    : section === 'transforms' ? <TransformsSection form={form} />
    : section === 'access' ? <AccessControlSection />
    : section === 'history' ? TabRequestHistory.children
    : section === 'responses' ? <ResponsesSection form={form} />
    : <OverviewSection form={form} websiteId={id} />;
  const isReady = loadState === 'ready';
  const status = loadState === 'loading' ? { tone: 'warning' as const, label: '正在加载' } : loadState === 'missing' ? { tone: 'warning' as const, label: '站点不存在' } : loadState === 'error' ? { tone: 'danger' as const, label: '加载失败' } : dirty ? { tone: 'warning' as const, label: '存在未保存变更' } : { tone: 'success' as const, label: '已同步' };
  return (
    <WorkbenchPage
      eyebrow="CONFIGURATION / WEBSITE"
      title={websiteName || '站点工作区'}
      description={active.hint}
      backTo="/website/list"
      extra={<>
        <SignalTag tone={status.tone}>{status.label}</SignalTag>
        <Button variant="outline" disabled={saving} loading={loadState === 'loading'} onClick={() => load()}>重新加载</Button>
        <Button type="submit" form="website-workspace-form" disabled={!isReady} loading={saving}><Save className="size-4" aria-hidden="true" />保存所有变更</Button>
      </>}
    >
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-[220px_minmax(0,1fr)]">
        <nav aria-label="站点配置分区" className="grid grid-cols-1 gap-px rounded-md border border-border bg-card p-2 sm:grid-cols-2 lg:flex lg:flex-col">
          {sections.map((item) => (
            <NavLink
              key={item.id}
              to={`/website/${id}${item.path}`}
              end
              className={({ isActive }) => `relative flex items-center justify-between rounded-sm px-3 py-2 text-[13px] font-semibold no-underline transition-colors hover:bg-muted ${isActive ? 'bg-muted text-primary before:absolute before:left-0 before:top-1/2 before:h-4 before:w-[3px] before:-translate-y-1/2 before:rounded-r-sm before:bg-primary' : 'text-foreground'}`}
            >
              <span>{item.label}</span>
              <InfoHint label={`${item.label}分区`} content={item.hint} stopClick />
            </NavLink>
          ))}
        </nav>
        <div className="min-h-[440px] rounded-md border border-border bg-card p-5">
          {loadState === 'loading' ? <div className="flex justify-center py-16"><Spin /></div>
            : loadState === 'missing' ? <Empty description="网站不存在或已被删除"><Button onClick={() => navigate('/website/list')}>返回网站列表</Button></Empty>
            : loadState === 'error' ? <Empty description={loadError}><Button onClick={() => load()}>重新加载</Button></Empty>
            : (
              <Form {...form} layout="vertical" disabled={!isReady || saving}>
                <SiteFieldsProvider control={form.control}>
                  <form
                    id="website-workspace-form"
                    noValidate
                    onSubmit={form.handleSubmit(save)}
                    onKeyDown={(event) => { if (event.key === 'Enter' && (event.target as HTMLElement).tagName === 'INPUT') event.preventDefault(); }}
                  >
                    <fieldset disabled={!isReady || saving} className="m-0 flex min-w-0 flex-col gap-5 border-0 p-0">
                      <div className="flex items-center gap-2">
                        <Eyebrow>{active.label}</Eyebrow>
                        <InfoHint label={`${active.label}分区`} content={active.hint} />
                        {section === 'routes' ? <InfoHint label="路径匹配" content="当前版本不支持正则匹配；例如 /api 会匹配 /api 及其下级路径。" /> : null}
                      </div>
                      {content}
                    </fieldset>
                  </form>
                </SiteFieldsProvider>
              </Form>
            )}
        </div>
      </div>
    </WorkbenchPage>
  );
};
