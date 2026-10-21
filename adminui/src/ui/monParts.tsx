import {
  Alert,
  AlertDescription,
  AlertTitle,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  DateTimeRangePicker,
  Skeleton,
  ToggleGroup,
  ToggleGroupItem,
} from '@modra/react';
import dayjs, { type Dayjs } from 'dayjs';
import React from 'react';

/** 观测类页面共用的小部件：替代 antd 的 Card(loading)、Segmented、Alert(action)、RangePicker。 */

export const cx = (...parts: Array<string | false | null | undefined>) => parts.filter(Boolean).join(' ');

export const MonCard: React.FC<{
  title?: React.ReactNode;
  extra?: React.ReactNode;
  loading?: boolean;
  className?: string;
  bodyClassName?: string;
  children?: React.ReactNode;
}> = ({ title, extra, loading = false, className, bodyClassName, children }) => (
  <Card className={className}>
    {title || extra ? (
      <CardHeader className="flex-row items-center justify-between gap-3 space-y-0">
        <CardTitle className="text-base">{title}</CardTitle>
        {extra ? <div className="flex flex-wrap items-center gap-2">{extra}</div> : null}
      </CardHeader>
    ) : null}
    <CardContent className={cx(!title && !extra && 'pt-6', bodyClassName)}>
      {loading ? (
        <div className="flex flex-col gap-2" role="status" aria-busy="true">
          <Skeleton className="h-4 w-1/3" />
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
        </div>
      ) : children}
    </CardContent>
  </Card>
);

export interface MonSegmentedOption<T extends string | number> {
  label: React.ReactNode;
  value: T;
}

/** antd Segmented 的等价物：单选 ToggleGroup，点击已选项不会取消选择。 */
export const MonSegmented = <T extends string | number>({
  options,
  value,
  onChange,
  className,
  block = false,
  ariaLabel,
}: {
  options: Array<MonSegmentedOption<T>>;
  value: T;
  onChange: (value: T) => void;
  className?: string;
  block?: boolean;
  ariaLabel?: string;
}) => (
  <ToggleGroup
    type="single"
    aria-label={ariaLabel}
    value={String(value)}
    onValueChange={(next) => {
      if (!next) return;
      const match = options.find((option) => String(option.value) === next);
      if (match) onChange(match.value);
    }}
    className={cx('flex-wrap justify-start', block && 'w-full', className)}
  >
    {options.map((option) => (
      <ToggleGroupItem key={String(option.value)} variant="outline" value={String(option.value)} className={cx('whitespace-nowrap', block && 'flex-1')}>
        {option.label}
      </ToggleGroupItem>
    ))}
  </ToggleGroup>
);

export const MonAlert: React.FC<{
  type?: 'error' | 'warning' | 'info' | 'success';
  message: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}> = ({ type = 'error', message, description, action, className }) => (
  <Alert variant={type === 'error' ? 'destructive' : type} className={className}>
    <div className="flex items-start justify-between gap-3">
      <div>
        <AlertTitle>{message}</AlertTitle>
        {description ? <AlertDescription>{description}</AlertDescription> : null}
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  </Alert>
);

export type MonDayjsRange = [Dayjs | null, Dayjs | null];

export interface MonRangePreset {
  label: string;
  value: [Dayjs, Dayjs];
}

/** 带时间的区间选择，对外保持 dayjs 元组，调用方原有的换算逻辑无需改动。 */
export const MonDateTimeRange: React.FC<{
  value: MonDayjsRange | null;
  onChange: (value: MonDayjsRange | null) => void;
  presets?: MonRangePreset[];
  className?: string;
  clearable?: boolean;
  ariaLabel?: string;
}> = ({ value, onChange, presets, className, clearable = true, ariaLabel = '时间范围' }) => (
  <DateTimeRangePicker
    aria-label={ariaLabel}
    className={className}
    clearable={clearable}
    dateFormat="yyyy-MM-dd"
    timeFormat="HH:mm"
    value={value ? { from: value[0]?.toDate(), to: value[1]?.toDate() } : undefined}
    presets={presets?.map((preset) => ({ label: preset.label, value: { from: preset.value[0].toDate(), to: preset.value[1].toDate() } }))}
    onValueChange={(next) => {
      if (!next || (!next.from && !next.to)) {
        onChange(null);
        return;
      }
      onChange([next.from ? dayjs(next.from) : null, next.to ? dayjs(next.to) : null]);
    }}
  />
);

/** 细进度条：用于占比、连接池使用率等，颜色按语义 token。 */
export const MonBar: React.FC<{ ratio: number; tone?: 'flow' | 'warning'; className?: string }> = ({ ratio, tone = 'flow', className }) => (
  <span className={cx('block h-1 min-w-12 flex-1 overflow-hidden rounded-full bg-muted', className)} aria-hidden="true">
    <i className={cx('block h-full rounded-full', tone === 'warning' ? 'bg-warning' : 'bg-primary')} style={{ width: `${Math.max(0, Math.min(100, ratio * 100))}%` }} />
  </span>
);

/** 标题前的状态小圆点，沿用旧版 KPI 卡片的语义。 */
export const toneDotClass = {
  flow: 'bg-primary',
  signal: 'bg-success',
  warning: 'bg-warning',
  alert: 'bg-destructive',
} as const;

export const StatDot: React.FC<{ tone: keyof typeof toneDotClass }> = ({ tone }) => (
  <span className={cx('inline-block size-1.5 shrink-0 rounded-full', toneDotClass[tone])} aria-hidden="true" />
);

/** 等宽小号大写标签（旧 .signal-eyebrow）。 */
export const eyebrowClass = 'font-mono text-[11px] font-semibold uppercase tracking-[.08em] text-muted-foreground';

/** 轻量 Descriptions：columnsClass 由调用方给出响应式列数，例如 md:grid-cols-3。 */
export const MonDescriptions: React.FC<{
  items: Array<{ key: React.Key; label: React.ReactNode; children: React.ReactNode }>;
  columnsClass?: string;
}> = ({ items, columnsClass = 'md:grid-cols-3' }) => (
  <dl className={cx('m-0 grid grid-cols-1 gap-x-6 gap-y-2 text-sm', columnsClass)}>
    {items.map((item) => (
      <div key={item.key} className="flex flex-wrap items-center gap-x-2">
        <dt className="text-muted-foreground">{item.label}</dt>
        <dd className="m-0">{item.children}</dd>
      </div>
    ))}
  </dl>
);
