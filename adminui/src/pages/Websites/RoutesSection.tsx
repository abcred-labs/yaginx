import { Accordion, AccordionContent, AccordionItem, AccordionTrigger, Badge, FormList } from '@modra/react';
import React from 'react';
import { useWatch } from 'react-hook-form';
import type { ProxyRule, UpstreamEndpoint, UpstreamPool } from '@/api/website';
import { NumberField, RadioField, SelectField, SwitchField, TextField } from '@/ui/siteFields';
import { InfoHint } from '@/ui/workbench';
import { AddButton, EditorCard, Eyebrow, FieldGrid, IconRemoveButton, padIndex, type WebsiteForm } from './WorkspaceParts';

const requiredText = (message: string) => ({ validate: (value: unknown) => (typeof value === 'string' && value.trim() !== '') || message });

const RuleRow: React.FC<{ form: WebsiteForm; index: number; onRemove: () => void }> = ({ form, index, onRemove }) => {
  const targetType = useWatch({ control: form.control, name: `proxyRules.${index}.targetType` });
  const pools = (useWatch({ control: form.control, name: 'upstreamPools' }) ?? []) as UpstreamPool[];
  const prefix = `proxyRules.${index}`;
  return (
    <EditorCard
      eyebrow={`规则 ${padIndex(index)}`}
      hint={{ label: '转发规则', content: '先匹配请求，再选择直接地址或上游池作为转发目标。' }}
      onRemove={onRemove}
    >
      <FieldGrid>
        <TextField name={`${prefix}.name`} label="规则备注（可选）" placeholder="API 服务" />
        <TextField name={`${prefix}.pathPattern`} label="匹配路径" placeholder="/api/{**remainder}；Fallback 留空" />
        <SwitchField name={`${prefix}.isFallback`} label="Fallback 规则" extra="启用后不能填写普通匹配条件" onText="是" offText="否" />
        <RadioField name={`${prefix}.targetType`} label="转发目标" defaultValue="direct" options={[{ label: '直接地址', value: 'direct' }, { label: '上游池', value: 'pool' }]} />
        {targetType === 'pool' ? (
          <SelectField
            className="md:col-span-2"
            name={`${prefix}.upstreamPoolId`}
            label="上游池"
            placeholder="选择或新建上游池"
            clearLabel="不选择"
            options={pools.map((pool) => ({ value: pool.id, label: `${pool.name || pool.id} · ${pool.endpoints?.length ?? 0} 个端点` }))}
          />
        ) : (
          <TextField className="md:col-span-2" name={`${prefix}.destination`} label="上游地址" placeholder="http://127.0.0.1:8080" required rules={requiredText('请输入上游地址')} />
        )}
        <TextField name={`${prefix}.destinationHost`} label="上游 Host" placeholder="留空则使用目标地址" />
        <TextField name={`${prefix}.removePrefix`} label="移除路径前缀" placeholder="/api" />
        <TextField name={`${prefix}.webProxy`} label="出站代理" placeholder="http://proxy.example:8888" />
        <SwitchField name={`${prefix}.preserveOriginalHost`} label="保留原始 Host" onText="保留" offText="替换" />
        <SwitchField name={`${prefix}.allowInvalidTlsCertificate`} label="忽略上游 TLS 证书错误" onText="忽略" offText="校验" />
        <SwitchField name={`${prefix}.isEnabled`} label="启用" onText="启用" offText="停用" />
      </FieldGrid>
    </EditorCard>
  );
};

const PoolRow: React.FC<{ form: WebsiteForm; index: number; onRemove: () => void }> = ({ form, index, onRemove }) => {
  const poolId = useWatch({ control: form.control, name: `upstreamPools.${index}.id` });
  const prefix = `upstreamPools.${index}`;
  return (
    <EditorCard
      eyebrow={`上游池 ${padIndex(index)}`}
      badges={poolId === 'legacy-default' ? <Badge variant="warning">兼容迁移</Badge> : null}
      onRemove={onRemove}
    >
      <FieldGrid>
        <TextField name={`${prefix}.id`} label="池 ID" placeholder="api-prod" required rules={requiredText('请输入池 ID')} />
        <TextField name={`${prefix}.name`} label="名称" placeholder="API 生产集群" />
        <SelectField
          name={`${prefix}.strategy`}
          label="负载策略"
          defaultValue="weightedRoundRobin"
          options={[
            { value: 'weightedRoundRobin', label: '加权轮询' },
            { value: 'leastConnections', label: '最少连接' },
            { value: 'random', label: '随机' },
            { value: 'failover', label: '主备切换' },
          ]}
        />
        <div className="md:col-span-3">
          <FormList control={form.control} name={`${prefix}.endpoints` as `upstreamPools.${number}.endpoints`}>
            {({ fields, add, remove }) => (
              <div className="flex flex-col gap-3">
                {fields.map((endpoint, endpointIndex) => {
                  const endpointPrefix = `${prefix}.endpoints.${endpointIndex}`;
                  return (
                    <div key={endpoint.id} className="flex flex-col gap-2 rounded-md border border-border bg-card p-3">
                      <Eyebrow>端点 {padIndex(endpointIndex)}</Eyebrow>
                      <div className="grid grid-cols-1 items-end gap-2 sm:grid-cols-2 lg:grid-cols-[minmax(180px,2fr)_minmax(150px,1.5fr)_100px_100px_auto_auto]">
                        <TextField name={`${endpointPrefix}.address`} label="地址" placeholder="http://127.0.0.1:5000" required rules={requiredText('请输入上游地址')} />
                        <TextField name={`${endpointPrefix}.host`} label="Host" placeholder="留空使用目标地址" />
                        <NumberField name={`${endpointPrefix}.weight`} label="权重" defaultValue={1} min={1} />
                        <NumberField name={`${endpointPrefix}.priority`} label="优先级" defaultValue={0} min={0} />
                        <SwitchField name={`${endpointPrefix}.enabled`} label="状态" defaultValue onText="启用" offText="停用" />
                        <div className="pb-0.5"><IconRemoveButton label="删除上游" onClick={() => remove(endpointIndex)} /></div>
                      </div>
                    </div>
                  );
                })}
                <AddButton onClick={() => add({ weight: 1, priority: 0, enabled: true } as UpstreamEndpoint)}>添加上游端点</AddButton>
              </div>
            )}
          </FormList>
        </div>
      </FieldGrid>
    </EditorCard>
  );
};

const UpstreamResourcesSection: React.FC<{ form: WebsiteForm }> = ({ form }) => (
  <FormList control={form.control} name="upstreamPools">
    {({ fields, add, remove }) => (
      <div className="flex flex-col gap-3 p-1">
        <div className="flex items-start justify-between gap-3 rounded-md border border-border bg-card p-3">
          <div className="flex flex-col gap-1">
            <Eyebrow>转发目标资源</Eyebrow>
            <span className="text-sm text-muted-foreground">单节点服务可以直接填写地址，多节点服务在这里维护可复用的上游池。</span>
          </div>
          <InfoHint label="转发目标资源" content="上游池只负责多个端点的负载与健康状态，具体请求是否使用它由上方转发规则决定。" />
        </div>
        {fields.map((field, index) => <PoolRow key={field.id} form={form} index={index} onRemove={() => remove(index)} />)}
        <AddButton onClick={() => add({ strategy: 'weightedRoundRobin', endpoints: [] } as unknown as UpstreamPool)}>添加上游池</AddButton>
      </div>
    )}
  </FormList>
);

export const RoutesSection: React.FC<{ form: WebsiteForm }> = ({ form }) => (
  <div className="flex flex-col gap-4">
    <FormList control={form.control} name="proxyRules">
      {({ fields, add, remove }) => (
        <div className="flex flex-col gap-3">
          {fields.map((field, index) => <RuleRow key={field.id} form={form} index={index} onRemove={() => remove(index)} />)}
          <AddButton onClick={() => add({ isEnabled: true, isFallback: false, targetType: 'direct' } as ProxyRule)}>添加转发规则</AddButton>
        </div>
      )}
    </FormList>
    <Accordion type="single" collapsible className="rounded-md border border-border px-4">
      <AccordionItem value="upstreams" className="border-b-0">
        <AccordionTrigger>上游池资源（多节点场景）</AccordionTrigger>
        <AccordionContent><UpstreamResourcesSection form={form} /></AccordionContent>
      </AccordionItem>
    </Accordion>
  </div>
);
