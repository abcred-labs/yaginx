import { Button } from '@modra/react';
import { Plus, Trash2 } from 'lucide-react';
import React from 'react';
import type { UseFormReturn } from 'react-hook-form';
import type { WebsiteUpsertInput } from '@/api/website';
import { InfoHint } from '@/ui/workbench';

export type WebsiteForm = UseFormReturn<WebsiteUpsertInput>;

/** 编号标题，例如 “主机 01”。 */
export const Eyebrow: React.FC<React.PropsWithChildren> = ({ children }) => (
  <span className="font-mono text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">{children}</span>
);

export const padIndex = (index: number) => String(index + 1).padStart(2, '0');

/** 列表中的单个可编辑条目：带编号标题、说明提示和删除按钮。 */
export const EditorCard: React.FC<{
  eyebrow: React.ReactNode;
  hint?: { label: string; content: React.ReactNode };
  badges?: React.ReactNode;
  actions?: React.ReactNode;
  onRemove?: () => void;
  removeLabel?: string;
  children: React.ReactNode;
}> = ({ eyebrow, hint, badges, actions, onRemove, removeLabel = '删除', children }) => (
  <section className="rounded-md border border-border bg-muted/40 p-4">
    <header className="mb-4 flex items-start justify-between gap-3">
      <div className="flex flex-wrap items-center gap-1">
        <Eyebrow>{eyebrow}</Eyebrow>
        {hint ? <InfoHint label={hint.label} content={hint.content} /> : null}
        {badges}
      </div>
      <div className="flex items-center gap-1">
        {actions}
        {onRemove ? (
          <Button type="button" variant="destructive-ghost" size="sm" onClick={onRemove}>
            <Trash2 className="size-4" aria-hidden="true" />
            {removeLabel}
          </Button>
        ) : null}
      </div>
    </header>
    {children}
  </section>
);

export const FieldGrid: React.FC<React.HTMLAttributes<HTMLDivElement>> = ({ className = '', ...props }) => (
  <div className={`grid grid-cols-1 gap-x-3 gap-y-4 md:grid-cols-3 ${className}`} {...props} />
);

export const AddButton: React.FC<{ onClick: () => void; children: React.ReactNode; block?: boolean }> = ({ onClick, children, block = true }) => (
  <Button type="button" variant="outline" className={`border-dashed ${block ? 'w-full' : ''}`} onClick={onClick}>
    <Plus className="size-4" aria-hidden="true" />
    {children}
  </Button>
);

export const IconRemoveButton: React.FC<{ label: string; onClick: () => void }> = ({ label, onClick }) => (
  <Button type="button" variant="destructive-ghost" size="icon" aria-label={label} onClick={onClick}>
    <Trash2 className="size-4" aria-hidden="true" />
  </Button>
);
