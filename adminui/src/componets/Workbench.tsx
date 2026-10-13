import { CheckCircleFilled, CloseCircleFilled, ExclamationCircleFilled, InfoCircleFilled, InfoCircleOutlined } from '@ant-design/icons';
import { Card, Space, Tag, Tooltip, Typography } from 'antd';
import React from 'react';
import { usePublishPageMeta } from '@/layouts/PageMeta';
import './workbench.less';

export type SignalTone = 'flow' | 'success' | 'warning' | 'danger' | 'neutral';

const toneIcon = {
  flow: <InfoCircleFilled />,
  success: <CheckCircleFilled />,
  warning: <ExclamationCircleFilled />,
  danger: <CloseCircleFilled />,
  neutral: <InfoCircleFilled />,
};

export const SignalTag: React.FC<{ tone?: SignalTone; children: React.ReactNode }> = ({ tone = 'neutral', children }) => (
  <Tag className={`signal-tag signal-tag--${tone}`} icon={toneIcon[tone]}>{children}</Tag>
);

export const InfoHint: React.FC<{ content: React.ReactNode; label: string; stopClick?: boolean }> = ({ content, label, stopClick = false }) => (
  <Tooltip title={content} trigger={['hover', 'focus', 'click']} overlayClassName="info-hint__tooltip">
    <button className="info-hint" type="button" aria-label={`查看${label}说明`} onClick={stopClick ? (event) => event.preventDefault() : undefined}><InfoCircleOutlined /></button>
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
  return <section className={`workbench-page ${className ?? ''}`}>
    <header className="workbench-page__header">
      <div className="workbench-page__heading">
        <Typography.Title level={2}>{title}</Typography.Title>
        {description ? <InfoHint label="页面" content={description} /> : null}
      </div>
      {extra ? <div className="workbench-page__actions">{extra}</div> : null}
    </header>
    {children}
  </section>;
};

export const SectionCard: React.FC<{ title: React.ReactNode; description?: React.ReactNode; extra?: React.ReactNode; children: React.ReactNode; className?: string }> = ({ title, description, extra, children, className }) => (
  <Card className={`section-card ${className ?? ''}`} title={<div className="section-card__heading"><span className="section-card__title">{title}</span>{description ? <InfoHint label="卡片" content={description} /> : null}</div>} extra={extra}>
    {children}
  </Card>
);

export const DataToolbar: React.FC<React.PropsWithChildren<{ actions?: React.ReactNode; className?: string }>> = ({ children, actions, className }) => (
  <div className={`data-toolbar ${className ?? ''}`}><Space size={[8, 8]} wrap>{children}</Space>{actions ? <div className="data-toolbar__actions">{actions}</div> : null}</div>
);
