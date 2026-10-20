import { DeleteOutlined, PlusOutlined, SaveOutlined } from '@ant-design/icons';
import { websiteGet, websiteResponseID, websiteUpsert, webDomainRequestCertificate, webDomainCertificateStatus, type WebsiteUpsertInput, type AcmeStatusResult } from '@/api/website';
import { InfoHint, SignalTag, WorkbenchPage } from '@/componets/Workbench';
import { websiteUpdatePayload } from './websiteUpdatePayload';
import { TabRequestHistory } from './TabRequestHistory';
import { hasDuplicateTransformKey, locationRewritePreset, proxyTransformKeys } from './proxyTransforms';
import { Alert, AutoComplete, Button, Card, Col, Collapse, Descriptions, Divider, Empty, Form, Input, InputNumber, Modal, Radio, Row, Select, Space, Spin, Switch, Tag, Tooltip, Typography, notification } from 'antd';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { NavLink, useNavigate, useParams } from 'react-router-dom';
import { SPACE } from '@/utils/spacing';
import './workspace.less';

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

const hostKey = (hostname: string) => hostname.trim().toLowerCase().replace(/\.$/, '');
const acmeStatusTag = (status: AcmeStatusResult | undefined) => {
  if (!status || !status.acmeOrder?.exists) return <Tag color="default">暂无 ACME 订单</Tag>;
  const label: Record<string, { color: string; text: string }> = {
    pending: { color: 'gold', text: '等待处理' },
    locked: { color: 'gold', text: '申请中' },
    authorizing: { color: 'gold', text: '验证中' },
    finalizing: { color: 'gold', text: '签发中' },
    retry_wait: { color: 'orange', text: '失败，等待重试' },
    valid: { color: 'green', text: '最近一次申请已完成' },
    failed: { color: 'red', text: '最近一次申请失败' },
    canceled: { color: 'default', text: '已取消' },
  };
  const info = label[status.acmeOrder.status ?? ''] ?? { color: 'default', text: status.acmeOrder.status ?? '未知' };
  const tag = <Tag color={info.color}>{info.text}</Tag>;
  const error = status.acmeOrder.errorMessage || status.errorMessage;
  return error ? <Tooltip title={error}>{tag}</Tooltip> : tag;
};

const certificateStatusTag = (status: AcmeStatusResult | undefined, loading: boolean) => {
  if (loading) return <Tag>读取中…</Tag>;
  const certificate = status?.certificate;
  if (!certificate?.exists) return <Tag>未发现证书</Tag>;
  if (certificate.validity === 'valid') {
    const tone = certificate.remainingDays <= 7 ? 'red' : certificate.remainingDays <= 30 ? 'orange' : 'green';
    return <Tag color={tone}>证书有效 · 剩余 {Math.max(0, certificate.remainingDays)} 天</Tag>;
  }
  return <Tag color="red">{certificate.validity === 'expired' ? '证书已过期' : '证书尚未生效'}</Tag>;
};

const HostSection = () => {
  const form = Form.useFormInstance<WebsiteUpsertInput>();
  const watchedBindings = Form.useWatch('hostBindings', form) ?? [];
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
    const bindings: Array<{ hostname?: string }> = form.getFieldValue('hostBindings') ?? [];
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
        const outcome: Record<string, { type: 'success' | 'info' | 'error'; message: string }> = {
          issued: { type: 'success', message: '证书申请成功' },
          already_valid: { type: 'info', message: '证书已有效，无需重新申请' },
          lock_busy: { type: 'info', message: '该域名正在被其他节点处理，请稍后重试' },
          order_in_progress: { type: 'info', message: '该域名已有一次申请正在进行中' },
          permanent_failure: { type: 'error', message: response.data.message || '证书申请失败' },
          temporary_failure: { type: 'error', message: response.data.message || '证书申请暂时失败，稍后会自动重试' },
          invalid_domain: { type: 'error', message: response.data.message || '域名格式无效' },
        };
        const result = outcome[response.data.status] ?? { type: 'info' as const, message: response.data.message || '已提交申请' };
        notification[result.type]({ message: '证书申请', description: result.message });
      } else {
        notification.error({ message: '证书申请失败', description: response.errMsg || '请求未能完成，请稍后重试。' });
      }
    } finally {
      await refreshStatus(hostname);
      setRequestingHost(null);
    }
  };
  return <><Form.List name="hostBindings">{(fields, { add, remove }) => <div className="editor-list">
  {fields.map((field, index) => <section className="rule-editor host-editor" key={field.key}><header><div className="rule-editor__heading"><span className="signal-eyebrow">主机 {String(index + 1).padStart(2, '0')}</span><InfoHint label="主机配置" content="Host 决定请求是否进入当前站点，不决定请求转发到哪个服务。" /></div><Space><Form.Item noStyle shouldUpdate>{({ getFieldValue }) => getFieldValue(['hostBindings', field.name, 'role']) !== 'primary' ? <Button type="link" onClick={() => { const bindings = (form.getFieldValue('hostBindings') ?? []).map((binding: Record<string, unknown>, bindingIndex: number) => ({ ...binding, role: bindingIndex === field.name ? 'primary' : 'alias', accessMode: bindingIndex === field.name ? 'proxy' : binding.accessMode })); form.setFieldValue('hostBindings', bindings); }}>设为主站</Button> : <Tag color="blue">主站</Tag>}</Form.Item><Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)}>删除</Button></Space></header><Row gutter={[SPACE.space3, 0]}>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'hostname']} label="主机名" rules={[{ required: true, whitespace: true, message: '请输入主机名' }]}><Input placeholder="app.example.com" /></Form.Item></Col>
    <Col xs={24} md={5}><Form.Item name={[field.name, 'role']} label="角色" initialValue="alias"><Radio.Group optionType="button" buttonStyle="solid" options={[{ label: '主站', value: 'primary' }, { label: '别名', value: 'alias' }]} /></Form.Item></Col>
    <Col xs={24} md={6}><Form.Item name={[field.name, 'accessMode']} label="访问方式" initialValue="proxy"><Radio.Group optionType="button" buttonStyle="solid" options={[{ label: '正常访问', value: 'proxy' }, { label: '跳转', value: 'redirect' }]} /></Form.Item></Col>
    <Col xs={24} md={5}><Form.Item name={[field.name, 'enabled']} label="状态" valuePropName="checked" initialValue><Switch checkedChildren="启用" unCheckedChildren="停用" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'certificateMode']} label="ACME 证书自动续期" valuePropName="checked" initialValue="manual" getValueProps={(value: string) => ({ checked: value === 'request' })} getValueFromEvent={(checked: boolean) => checked ? 'request' : 'manual'}><Switch checkedChildren="自动续期" unCheckedChildren="不自动" /></Form.Item></Col>
    <Col span={24}><Form.Item noStyle shouldUpdate={(prev, next) => prev.hostBindings?.[field.name]?.certificateMode !== next.hostBindings?.[field.name]?.certificateMode || prev.hostBindings?.[field.name]?.hostname !== next.hostBindings?.[field.name]?.hostname}>{({ getFieldValue }) => {
      const hostname: string | undefined = getFieldValue(['hostBindings', field.name, 'hostname']);
      const certificateMode = getFieldValue(['hostBindings', field.name, 'certificateMode']);
      const key = hostname ? hostKey(hostname) : '';
      const status = key ? statusByHost[key] : undefined;
      const cert = status?.certificate;
      const order = status?.acmeOrder;
      return <Card size="small" title="安全状态" style={{ marginBottom: SPACE.space3 }}><Space wrap>{certificateStatusTag(status, !!hostname && !!loadingByHost[key])}<Button size="small" disabled={!cert?.exists} onClick={() => cert?.exists && setCertificateDetails(cert)}>证书详情</Button>{acmeStatusTag(status)}<Button size="small" disabled={!hostname || certificateMode !== 'request'} loading={requestingHost === hostname} onClick={() => hostname && requestCertificate(hostname)}>立即申请证书</Button>{order?.exists ? <a href={`/systems/acme?domain=${encodeURIComponent(hostname ?? '')}`} target="_blank" rel="noreferrer">查看 ACME 订单</a> : null}{errorByHost[key] ? <Button type="link" size="small" onClick={() => hostname && refreshStatus(hostname)}>重试</Button> : null}</Space>{status ? <Typography.Paragraph type="secondary" style={{ margin: '8px 0 0' }}>状态更新时间：{new Date(status.checkedAt).toLocaleString()}{order?.isInProgress && order.nextAttemptAt ? ` · 下次重试：${new Date(order.nextAttemptAt).toLocaleString()}` : ''}{order?.errorMessage ? ` · ${order.errorMessage}` : ''}</Typography.Paragraph> : errorByHost[key] ? <Typography.Text type="danger">状态暂时不可用</Typography.Text> : null}</Card>;
    }}</Form.Item></Col>
    <Col span={24}><Form.Item noStyle shouldUpdate={(prev, next) => prev.hostBindings?.[field.name]?.accessMode !== next.hostBindings?.[field.name]?.accessMode}>{({ getFieldValue }) => getFieldValue(['hostBindings', field.name, 'accessMode']) === 'redirect' ? <Row gutter={[SPACE.space3, 0]}><Col xs={24} md={8}><Form.Item name={[field.name, 'redirectStatusCode']} label="跳转状态码" initialValue={308}><Radio.Group optionType="button" buttonStyle="solid" options={[{ label: '308', value: 308 }, { label: '301', value: 301 }]} /></Form.Item></Col><Col xs={24} md={8}><Form.Item name={[field.name, 'redirectTargetHost']} label="目标 Host（留空使用主站）"><Input placeholder="www.example.com" /></Form.Item></Col></Row> : null}</Form.Item></Col>
  </Row></section>)}
  <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ enabled: true, role: 'alias', accessMode: 'proxy', redirectStatusCode: 308, certificateMode: 'manual' })}>添加主机</Button>
  </div>}</Form.List><Modal open={!!certificateDetails} title="证书详情" footer={null} onCancel={() => setCertificateDetails(undefined)}>{certificateDetails ? <Descriptions size="small" column={1} bordered><Descriptions.Item label="Host / SAN">{certificateDetails.dnsNames.join(', ') || '—'}</Descriptions.Item><Descriptions.Item label="状态">{certificateDetails.validity}</Descriptions.Item><Descriptions.Item label="签发者">{certificateDetails.issuer || '—'}</Descriptions.Item><Descriptions.Item label="来源">{certificateDetails.source || '—'}</Descriptions.Item><Descriptions.Item label="序列号">{certificateDetails.serialNumber || '—'}</Descriptions.Item><Descriptions.Item label="SHA-256 指纹">{certificateDetails.fingerprint || '—'}</Descriptions.Item><Descriptions.Item label="有效期">{new Date(certificateDetails.notBefore).toLocaleString()} → {new Date(certificateDetails.notAfter).toLocaleString()}</Descriptions.Item><Descriptions.Item label="剩余时间">{certificateDetails.remainingDays} 天</Descriptions.Item><Descriptions.Item label="当前活动证书">{certificateDetails.isActive ? '是' : '否'}</Descriptions.Item></Descriptions> : null}</Modal></>;
};

const UpstreamResourcesSection = () => <Form.List name="upstreamPools">{(fields, { add, remove }) => <div className="editor-list upstream-resources">
  <div className="website-workspace__section-intro"><div><span className="signal-eyebrow">转发目标资源</span><Typography.Text type="secondary">单节点服务可以直接填写地址，多节点服务在这里维护可复用的上游池。</Typography.Text></div><InfoHint label="转发目标资源" content="上游池只负责多个端点的负载与健康状态，具体请求是否使用它由上方转发规则决定。" /></div>
  {fields.map((field, index) => <section className="rule-editor upstream-pool-editor" key={field.key}><header><div className="rule-editor__heading"><span className="signal-eyebrow">上游池 {String(index + 1).padStart(2, '0')}</span><Form.Item noStyle shouldUpdate>{({ getFieldValue }) => getFieldValue(['upstreamPools', field.name, 'id']) === 'legacy-default' ? <Tag color="gold">兼容迁移</Tag> : null}</Form.Item></div><Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)}>删除</Button></header><Row gutter={[SPACE.space3, 0]}>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'id']} label="池 ID" rules={[{ required: true, whitespace: true, message: '请输入池 ID' }]}><Input placeholder="api-prod" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'name']} label="名称"><Input placeholder="API 生产集群" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'strategy']} label="负载策略" initialValue="weightedRoundRobin"><Select options={[{ value: 'weightedRoundRobin', label: '加权轮询' }, { value: 'leastConnections', label: '最少连接' }, { value: 'random', label: '随机' }, { value: 'failover', label: '主备切换' }]} /></Form.Item></Col>
    <Col span={24}><Form.List name={[field.name, 'endpoints']}>{(endpointFields, { add: addEndpoint, remove: removeEndpoint }) => <div className="endpoint-list">{endpointFields.map((endpoint, endpointIndex) => <div className="endpoint-card" key={endpoint.key}><div className="endpoint-card__index">端点 {String(endpointIndex + 1).padStart(2, '0')}</div><Form.Item name={[endpoint.name, 'address']} label="地址" rules={[{ required: true, whitespace: true, message: '请输入上游地址' }]}><Input placeholder="http://127.0.0.1:5000" /></Form.Item><Form.Item name={[endpoint.name, 'host']} label="Host"><Input placeholder="留空使用目标地址" /></Form.Item><Form.Item name={[endpoint.name, 'weight']} label="权重" initialValue={1}><InputNumber min={1} style={{ width: '100%' }} /></Form.Item><Form.Item name={[endpoint.name, 'priority']} label="优先级" initialValue={0}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item><Form.Item name={[endpoint.name, 'enabled']} label="状态" valuePropName="checked" initialValue><Switch checkedChildren="启用" unCheckedChildren="停用" /></Form.Item><Button type="text" danger aria-label="删除上游" icon={<DeleteOutlined />} onClick={() => removeEndpoint(endpoint.name)}>删除端点</Button></div>)}<Button type="dashed" onClick={() => addEndpoint({ weight: 1, priority: 0, enabled: true })}>添加上游端点</Button></div>}</Form.List></Col>
  </Row></section>)}
  <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add({ strategy: 'weightedRoundRobin', endpoints: [] })}>添加上游池</Button>
</div>}</Form.List>;

const RoutesSection = () => <div className="editor-list"><Form.List name="proxyRules">{(fields, { add, remove }) => <div className="editor-list">
  {fields.map((field, index) => <section className="rule-editor" key={field.key}><header><div className="rule-editor__heading"><span className="signal-eyebrow">规则 {String(index + 1).padStart(2, '0')}</span><InfoHint label="转发规则" content="先匹配请求，再选择直接地址或上游池作为转发目标。" /></div><Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)}>删除</Button></header><Row gutter={[SPACE.space3, 0]}>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'name']} label="规则备注（可选）"><Input placeholder="API 服务" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'pathPattern']} label="匹配路径"><Input placeholder="/api/{**remainder}；Fallback 留空" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'isFallback']} label="Fallback 规则" valuePropName="checked" extra="启用后不能填写普通匹配条件"><Switch checkedChildren="是" unCheckedChildren="否" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'targetType']} label="转发目标" initialValue="direct"><Radio.Group optionType="button" buttonStyle="solid" options={[{ label: '直接地址', value: 'direct' }, { label: '上游池', value: 'pool' }]} /></Form.Item></Col>
    <Form.Item noStyle shouldUpdate={(prev, next) => prev.proxyRules?.[field.name]?.targetType !== next.proxyRules?.[field.name]?.targetType || prev.upstreamPools !== next.upstreamPools}>{({ getFieldValue }) => getFieldValue(['proxyRules', field.name, 'targetType']) === 'pool' ? <Col xs={24} md={16}><Form.Item name={[field.name, 'upstreamPoolId']} label="上游池"><Select allowClear placeholder="选择或新建上游池" options={(getFieldValue('upstreamPools') ?? []).map((pool: { id: string; name?: string; endpoints?: unknown[] }) => ({ value: pool.id, label: `${pool.name || pool.id} · ${pool.endpoints?.length ?? 0} 个端点` }))} /></Form.Item></Col> : <Col xs={24} md={16}><Form.Item name={[field.name, 'destination']} label="上游地址" rules={[{ required: true, whitespace: true, message: '请输入上游地址' }]}><Input placeholder="http://127.0.0.1:8080" /></Form.Item></Col>}</Form.Item>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'destinationHost']} label="上游 Host"><Input placeholder="留空则使用目标地址" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'removePrefix']} label="移除路径前缀"><Input placeholder="/api" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'webProxy']} label="出站代理"><Input placeholder="http://proxy.example:8888" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'preserveOriginalHost']} label="保留原始 Host" valuePropName="checked"><Switch checkedChildren="保留" unCheckedChildren="替换" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'allowInvalidTlsCertificate']} label="忽略上游 TLS 证书错误" valuePropName="checked"><Switch checkedChildren="忽略" unCheckedChildren="校验" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'isEnabled']} label="启用" valuePropName="checked"><Switch checkedChildren="启用" unCheckedChildren="停用" /></Form.Item></Col>
  </Row></section>)}
  <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add({ isEnabled: true, isFallback: false, targetType: 'direct' })}>添加转发规则</Button>
</div>}</Form.List><Collapse className="website-workspace__nested-resources" items={[{ key: 'upstreams', label: '上游池资源（多节点场景）', children: <UpstreamResourcesSection /> }]} /></div>;

const TransformsSection = () => {
  const form = Form.useFormInstance<WebsiteUpsertInput>();
  return <Form.List name="proxyTransforms">{(fields, { add, remove }) => <div className="editor-list">
    <Alert type="info" showIcon message="代理转换作用于站点默认转发（Fallback 规则）" description="带路径匹配的转发规则不继承这里的转换。同一个站点内键名不能重复，所以 Set 由 ResponseHeader、RequestHeader、ResponseLocationHost 等共用同一个值。" />
    <Alert type="success" showIcon message="重定向 Location 改写（ResponseLocationHost）" description={<>上游返回 301/302/303/307/308 且 Location 为绝对地址时，若其 host 等于“ResponseLocationHost”的值（支持 *.example.com），则把 host 替换为“Set”的值，scheme、路径和签名参数保持不变。示例：ResponseLocationHost = pkg-containers.githubusercontent.com，Set = blob.example.com。</>} />
    {fields.map((field, index) => <div className="editor-row" key={field.key}><span className="editor-row__number">{String(index + 1).padStart(2, '0')}</span><Space.Compact style={{ width: '100%' }}>
      <Form.Item name={[field.name, 'key']} noStyle rules={[{ required: true, whitespace: true, message: '请输入转换键' }, { validator: async (_, value: string) => { if (hasDuplicateTransformKey(form.getFieldValue('proxyTransforms'), value ?? '', field.name)) throw new Error('转换键不能重复'); } }]}><AutoComplete style={{ width: '40%' }} placeholder="ResponseLocationHost" options={proxyTransformKeys.map((value) => ({ value }))} filterOption={(input, option) => (option?.value ?? '').toLowerCase().includes(input.toLowerCase())} /></Form.Item>
      <Form.Item name={[field.name, 'value']} noStyle><Input style={{ width: '60%' }} placeholder="值" /></Form.Item>
    </Space.Compact><Button type="text" danger aria-label="删除转换" icon={<DeleteOutlined />} onClick={() => remove(field.name)} /></div>)}
    <Space wrap>
      <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ key: '', value: '' })}>添加转换</Button>
      <Button type="dashed" icon={<PlusOutlined />} onClick={() => { const existing = (form.getFieldValue('proxyTransforms') ?? []) as Array<{ key?: string }>; const keys = new Set(existing.map((item) => (item.key ?? '').trim().toLowerCase())); locationRewritePreset.filter((item) => !keys.has(item.key.toLowerCase())).forEach((item) => add({ ...item })); }}>添加 Location 改写</Button>
    </Space>
  </div>}</Form.List>;
};

const ResponsesSection = () => <Form.List name="simpleResponses">{(fields, { add, remove }) => <div className="editor-list">
  {fields.map((field, index) => <section className="rule-editor" key={field.key}><header><div className="rule-editor__heading"><span className="signal-eyebrow">响应 {String(index + 1).padStart(2, '0')}</span><InfoHint label="静态响应" content="直接返回内容，不经过上游。" /></div><Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)}>删除</Button></header><Row gutter={[SPACE.space3, 0]}>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'path']} label="路径" rules={[{ required: true, message: '请输入路径' }]}><Input placeholder="/health" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'contentType']} label="内容类型" initialValue="text/plain"><Input placeholder="text/plain" /></Form.Item></Col>
    <Col xs={24} md={8}><Form.Item name={[field.name, 'statusCode']} label="状态码" initialValue={200}><InputNumber min={100} max={599} style={{ width: '100%' }} /></Form.Item></Col>
    <Col span={24}><Form.Item name={[field.name, 'responseContent']} label="响应内容" rules={[{ required: true, message: '请输入响应内容' }]}><Input.TextArea rows={4} /></Form.Item></Col>
    <Col span={24}><Typography.Text>响应头</Typography.Text><Form.List name={[field.name, 'extraHeaders']}>{(headerFields, { add: addHeader, remove: removeHeader }) => <div className="editor-list">
      {headerFields.map((header) => <div className="editor-row" key={header.key}><span className="editor-row__number">#</span><Space.Compact><Form.Item name={[header.name, 'key']} noStyle rules={[{ required: true, whitespace: true, message: '请输入响应头名称' }]}><Input placeholder="X-Response-Header" /></Form.Item><Form.Item name={[header.name, 'value']} noStyle><Input placeholder="响应头值" /></Form.Item></Space.Compact><Button type="text" danger aria-label="删除响应头" icon={<DeleteOutlined />} onClick={() => removeHeader(header.name)} /></div>)}
      <Button type="dashed" icon={<PlusOutlined />} onClick={() => addHeader()}>添加响应头</Button>
    </div>}</Form.List></Col>
  </Row></section>)}
  <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add({ contentType: 'text/plain', statusCode: 200 })}>添加静态响应</Button>
</div>}</Form.List>;

const OverviewSection: React.FC<{ websiteId: string }> = ({ websiteId }) => <>
  <Form.Item noStyle shouldUpdate>{({ getFieldValue }) => {
    const hosts = getFieldValue('hostBindings') ?? [];
    const enabledHosts = hosts.filter((host: { enabled?: boolean }) => host.enabled !== false);
    const primary = hosts.find((host: { role?: string; enabled?: boolean }) => host.role === 'primary' && host.enabled !== false);
    const rules = getFieldValue('proxyRules') ?? [];
    const fallback = rules.find((rule: { isFallback?: boolean; isEnabled?: boolean }) => rule.isFallback && rule.isEnabled);
    return <Card className="overview-signal-card" title="站点运行入口" extra={<Space><Button type="link" onClick={() => window.location.hash = `#/website/${websiteId}/hosts`}>管理主机</Button><Button type="link" onClick={() => window.location.hash = `#/website/${websiteId}/routes`}>管理转发规则</Button></Space>}>
      <div className="overview-signal-grid"><div><span className="signal-eyebrow">PRIMARY HOST</span><strong>{primary?.hostname ?? '未配置'}</strong><Typography.Text type="secondary">{enabledHosts.length} 个启用入口</Typography.Text></div><div><span className="signal-eyebrow">FORWARDING</span><strong>{rules.length} 条规则</strong><Typography.Text type="secondary">{fallback ? '已配置 Fallback' : '未配置 Fallback'}</Typography.Text></div></div>
    </Card>;
  }}</Form.Item>
  <Row gutter={[SPACE.space4, 0]}>
  <Col xs={24} lg={12}><Form.Item name="name" label="站点名称" rules={[{ required: true, message: '请输入站点名称' }]}><Input placeholder="生产 API 网关" /></Form.Item></Col>
  <Col xs={24} lg={12}><Form.Item name="webProxy" label="出站代理"><Input placeholder="http://proxy.example:8888" /></Form.Item></Col>
  <Col xs={24} lg={12}><Form.Item name="authorizationPolicy" label="访问策略"><Input placeholder="例如 BasicAuthPolicy" /></Form.Item></Col>
  <Col span={24}><Divider orientation="left">访问状态</Divider><Space size={[SPACE.space6, SPACE.space3]} wrap>
    <Form.Item name="isMaintenanceMode" label="维护模式（停止访问）" valuePropName="checked"><Switch checkedChildren="维护中" unCheckedChildren="正常" /></Form.Item>
  </Space></Col>
  <Col span={24}><Form.Item noStyle shouldUpdate={(previous, next) => previous.isMaintenanceMode !== next.isMaintenanceMode}>{({ getFieldValue }) => getFieldValue('isMaintenanceMode') ? <Alert type="warning" showIcon message="维护模式已开启" description="保存后，匹配该站点的请求将直接返回 503，不会进入静态响应、转发规则或上游服务。ACME 证书校验不受影响。" /> : null}</Form.Item></Col>
  <Col span={24}><Divider orientation="left">连接行为</Divider><Space size={[SPACE.space6, SPACE.space3]} wrap>
    <Form.Item name="preserveOriginalHost" label="保留原始 Host" valuePropName="checked"><Switch checkedChildren="保留" unCheckedChildren="替换" /></Form.Item>
    <Form.Item name="allowInvalidTlsCertificate" label="忽略上游 TLS 证书错误" valuePropName="checked"><Switch checkedChildren="忽略" unCheckedChildren="校验" /></Form.Item>
    <Form.Item name="redirectHttpToHttps" label="HTTP 自动跳转 HTTPS" valuePropName="checked"><Switch checkedChildren="开启" unCheckedChildren="关闭" /></Form.Item>
  </Space></Col>
  <Col xs={24} lg={12}><Form.Item name="responseHeaderTimeout" label="上游响应头超时" extra="上游在该时间内未返回响应头则返回 502。留空则使用全局设置（proxy.responseHeaderTimeout，默认 30s）；最大 1h。" rules={[{ pattern: /^(\d+(\.\d+)?(ms|s|m|h))+$/, message: '请输入时长，例如 600s 或 10m' }]}><Input placeholder="例如 600s" allowClear /></Form.Item></Col>
</Row></>;

const AccessControlSection = () => <div>
  <div className="website-workspace__section-hint"><InfoHint label="访问控制" content="访问控制只影响当前站点。ACME challenge 不受此设置影响。" /></div>
  <Form.Item name="ipAccessMode" label="网站级模式" initialValue="disabled"><Radio.Group optionType="button" buttonStyle="solid" options={[{ label: '不限制', value: 'disabled' }, { label: '黑名单', value: 'blacklist' }, { label: '白名单', value: 'whitelist' }]} /></Form.Item>
  <Form.Item name="ipAccessCIDRs" label="IP / CIDR 列表" extra="每行填写一个 IPv4、IPv6 地址或 CIDR 网段。" getValueProps={(value: string[] | undefined) => ({ value: Array.isArray(value) ? value.join('\n') : '' })} getValueFromEvent={(event: React.ChangeEvent<HTMLTextAreaElement>) => event.target.value.split(/\r?\n/)}><Input.TextArea autoSize={{ minRows: 5, maxRows: 12 }} placeholder={'203.0.113.10\n2001:db8::/32'} /></Form.Item>
</div>;

export const WebsiteWorkspace: React.FC<{ section: WorkspaceSection }> = ({ section }) => {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [form] = Form.useForm<WebsiteUpsertInput>();
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [loadState, setLoadState] = useState<'loading' | 'ready' | 'missing' | 'error'>('loading');
  const [loadError, setLoadError] = useState('');
  const [websiteName, setWebsiteName] = useState('');
  const loadVersionRef = useRef(0);
  const requestRef = useRef<AbortController | null>(null);
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
        form.resetFields();
        setWebsiteName('');
        setDirty(false);
        setLoadState('missing');
        return;
      }
      form.resetFields();
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
      form.setFieldsValue({ ...response.data, hostBindings, upstreamPools, proxyRules, requestHistory: response.data.requestHistory ?? { mode: 'inherit' } });
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
  }, [id]);
  const onValuesChange = () => {
    if (loadState !== 'ready') return;
    const values = form.getFieldsValue(true);
    setWebsiteName(values.name ?? '');
    setDirty(true);
  };
  const save = async () => {
    if (loadState !== 'ready') return;
    await form.validateFields();
    const values = form.getFieldsValue(true);
    const primaryHosts = (values.hostBindings ?? []).filter((host: { role?: string }) => host.role === 'primary');
    if (primaryHosts.length > 1) {
      notification.error({ message: '主机配置无效', description: '每个站点最多只能有一个主站。' });
      return;
    }
    if (primaryHosts.some((host: { accessMode?: string }) => host.accessMode === 'redirect')) {
      notification.error({ message: '主机配置无效', description: '主站不能配置为跳转模式。' });
      return;
    }
    const fallbackRules = (values.proxyRules ?? []).filter((rule: { isEnabled?: boolean; isFallback?: boolean }) => rule.isEnabled && rule.isFallback);
    if (fallbackRules.length > 1) {
      notification.error({ message: '转发规则无效', description: '每个站点最多只能有一个启用的 Fallback 规则。' });
      return;
    }
    setSaving(true);
    cancelPendingLoad();
    try {
      const response = await websiteUpsert(websiteUpdatePayload(id, values));
      if (websiteResponseID(response) === null) return;
      setDirty(false);
      notification.success({ message: '站点配置已保存', description: '已使用服务端返回的最新配置同步工作区。' });
      await load();
    }
    finally { setSaving(false); }
  };
  const content = section === 'overview' ? <OverviewSection websiteId={id} /> : section === 'hosts' ? <HostSection /> : section === 'routes' ? <RoutesSection /> : section === 'transforms' ? <TransformsSection /> : section === 'access' ? <AccessControlSection /> : section === 'history' ? TabRequestHistory.children : section === 'responses' ? <ResponsesSection /> : <OverviewSection websiteId={id} />;
  const isReady = loadState === 'ready';
  const status = loadState === 'loading' ? { tone: 'warning' as const, label: '正在加载' } : loadState === 'missing' ? { tone: 'warning' as const, label: '站点不存在' } : loadState === 'error' ? { tone: 'danger' as const, label: '加载失败' } : dirty ? { tone: 'warning' as const, label: '存在未保存变更' } : { tone: 'success' as const, label: '已同步' };
  return <WorkbenchPage eyebrow="CONFIGURATION / WEBSITE" title={websiteName || '站点工作区'} description={active.hint} backTo="/website/list" extra={<Space><SignalTag tone={status.tone}>{status.label}</SignalTag><Button disabled={saving} loading={loadState === 'loading'} onClick={() => load()}>重新加载</Button><Button type="primary" icon={<SaveOutlined />} disabled={!isReady} loading={saving} onClick={save}>保存所有变更</Button></Space>}>
    <div className="website-workspace">
      <nav className="website-workspace__nav" aria-label="站点配置分区">{sections.map((item) => <NavLink key={item.id} to={`/website/${id}${item.path}`} className={({ isActive }) => isActive ? 'is-active' : ''}><span>{item.label}</span><InfoHint label={`${item.label}分区`} content={item.hint} /></NavLink>)}</nav>
      <div className="website-workspace__form">
        {loadState === 'loading' ? <Spin /> : loadState === 'missing' ? <Empty description="网站不存在或已被删除"><Button type="primary" onClick={() => navigate('/website/list')}>返回网站列表</Button></Empty> : loadState === 'error' ? <Empty description={loadError}><Button type="primary" onClick={() => load()}>重新加载</Button></Empty> : <Form form={form} disabled={!isReady || saving} layout="vertical" onValuesChange={onValuesChange}><div className="website-workspace__form-header"><span className="signal-eyebrow">{active.label}</span><InfoHint label={`${active.label}分区`} content={active.hint} />{section === 'routes' ? <InfoHint label="路径匹配" content="当前版本不支持正则匹配；例如 /api 会匹配 /api 及其下级路径。" /> : null}</div>{content}</Form>}
      </div>
    </div>
  </WorkbenchPage>;
};
