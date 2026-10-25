import { Badge, Card, CardContent, CardHeader, CardTitle, Tabs, TabsContent, TabsList, TabsTrigger, Tooltip, TooltipContent, TooltipTrigger } from '@modra/react';
import { CircleAlert, CircleCheck, CircleX, Info } from '@/ui/icons';
import React, { useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import { usePublishPageMeta } from '@/layouts/PageMeta';

/**
 * 页面骨架：WorkbenchPage 发布顶栏面包屑，SectionCard、DataToolbar、SignalTag、InfoHint 提供各页共用的卡片与提示。
 */

export type SignalTone = 'flow' | 'success' | 'warning' | 'danger' | 'neutral';

const TONE_VARIANT = {
  flow: 'info',
  success: 'success',
  warning: 'warning',
  danger: 'destructive',
  neutral: 'secondary',
} as const;

const TONE_ICON = {
  flow: Info,
  success: CircleCheck,
  warning: CircleAlert,
  danger: CircleX,
  neutral: Info,
} as const;

export const SignalTag: React.FC<{ tone?: SignalTone; children: React.ReactNode }> = ({ tone = 'neutral', children }) => {
  const Icon = TONE_ICON[tone];
  return (
    <Badge variant={TONE_VARIANT[tone]} className="gap-1">
      <Icon className="size-3" aria-hidden="true" />
      {children}
    </Badge>
  );
};

export const InfoHint: React.FC<{ content: React.ReactNode; label: string; stopClick?: boolean }> = ({ content, label, stopClick = false }) => (
  <Tooltip>
    <TooltipTrigger asChild>
      <button
        type="button"
        className="inline-flex size-5 items-center justify-center rounded-full text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        aria-label={`查看${label}说明`}
        onClick={stopClick ? (event) => event.preventDefault() : undefined}
      >
        <Info className="size-4" aria-hidden="true" />
      </button>
    </TooltipTrigger>
    <TooltipContent className="max-w-xs">{content}</TooltipContent>
  </Tooltip>
);

interface WorkbenchPageProps {
  title: React.ReactNode;
  description?: React.ReactNode;
  eyebrow?: string;
  extra?: React.ReactNode;
  backTo?: string;
  children: React.ReactNode;
  className?: string;
}

export const WorkbenchPage: React.FC<WorkbenchPageProps> = ({ title, description, eyebrow, extra, backTo, children, className }) => {
  // 面包屑、页面说明与返回入口统一上移到顶栏，页头只保留页面级操作。
  usePublishPageMeta({ trail: eyebrow, title, description, backTo });
  return (
    <section className={`flex flex-col gap-3 ${className ?? ''}`}>
      {extra ? <header className="flex flex-wrap items-center justify-end gap-2">{extra}</header> : null}
      {children}
    </section>
  );
};

export const SectionCard: React.FC<{ title: React.ReactNode; description?: React.ReactNode; extra?: React.ReactNode; children: React.ReactNode; className?: string }> = ({ title, description, extra, children, className }) => (
  <Card className={className}>
    <CardHeader className="flex-row items-center justify-between gap-3 space-y-0">
      <div className="flex items-center gap-2">
        <CardTitle className="text-base">{title}</CardTitle>
        {description ? <InfoHint label="卡片" content={description} /> : null}
      </div>
      {extra ? <div className="flex items-center gap-2">{extra}</div> : null}
    </CardHeader>
    <CardContent>{children}</CardContent>
  </Card>
);

export interface WorkbenchTabItem {
  key: string;
  label: React.ReactNode;
  children: React.ReactNode;
}

/**
 * Tab 选中项与 URL 查询参数同步：可直接分享、刷新保持。
 * 首个 tab 视为默认值，不写入 URL；非法取值回退到默认值；切换时保留其他查询参数且不增加历史记录。
 */
export const useUrlTab = (keys: string[], param = 'tab'): [string, (key: string) => void] => {
  const [searchParams, setSearchParams] = useSearchParams();
  const raw = searchParams.get(param);
  const value = raw && keys.includes(raw) ? raw : keys[0];
  const setValue = useCallback((key: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (key === keys[0]) next.delete(param); else next.set(param, key);
      return next;
    }, { replace: true });
  }, [keys, param, setSearchParams]);
  return [value, setValue];
};

/** 页面内并列职责的统一 Tab 组织：一行 TabsList + 可选右侧操作区，内容互斥展示；选中项同步到 URL（默认 ?tab=）。 */
export const WorkbenchTabs: React.FC<{ items: WorkbenchTabItem[]; urlParam?: string; extra?: React.ReactNode; className?: string }> = ({ items, urlParam, extra, className }) => {
  const [active, setActive] = useUrlTab(items.map(item => item.key), urlParam);
  return (
    <Tabs value={active} onValueChange={setActive} className={className}>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <TabsList>
          {items.map(item => <TabsTrigger key={item.key} value={item.key}>{item.label}</TabsTrigger>)}
        </TabsList>
        {extra ? <div className="flex flex-wrap items-center gap-2">{extra}</div> : null}
      </div>
      {items.map(item => <TabsContent key={item.key} value={item.key}>{item.children}</TabsContent>)}
    </Tabs>
  );
};

export const DataToolbar: React.FC<React.PropsWithChildren<{ actions?: React.ReactNode; className?: string }>> = ({ children, actions, className }) => (
  <div className={`flex flex-wrap items-center justify-between gap-2 ${className ?? ''}`}>
    <div className="flex flex-wrap items-center gap-2">{children}</div>
    {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
  </div>
);
