import { Badge, Card, CardContent, CardHeader, CardTitle, Tooltip, TooltipContent, TooltipTrigger } from '@modra/react';
import { CircleAlert, CircleCheck, CircleX, Info } from '@/ui/icons';
import React from 'react';
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
  // 面包屑与返回入口统一上移到顶栏，页头只保留标题、说明入口和页面级操作。
  usePublishPageMeta({ trail: eyebrow, title, backTo });
  return (
    <section className={`flex flex-col gap-4 ${className ?? ''}`}>
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <h2 className="text-xl font-semibold">{title}</h2>
          {description ? <InfoHint label="页面" content={description} /> : null}
        </div>
        {extra ? <div className="flex flex-wrap items-center gap-2">{extra}</div> : null}
      </header>
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

export const DataToolbar: React.FC<React.PropsWithChildren<{ actions?: React.ReactNode; className?: string }>> = ({ children, actions, className }) => (
  <div className={`flex flex-wrap items-center justify-between gap-2 ${className ?? ''}`}>
    <div className="flex flex-wrap items-center gap-2">{children}</div>
    {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
  </div>
);
