import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@modra/react';
import { ArrowLeft, Moon, Sun } from 'lucide-react';
import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useAppearance } from '@/appearance';
import { splitTrail, usePageMeta } from './PageMeta';

const IconTip: React.FC<React.PropsWithChildren<{ label: string }>> = ({ label, children }) => (
  <Tooltip>
    <TooltipTrigger asChild>{children}</TooltipTrigger>
    <TooltipContent>{label}</TooltipContent>
  </Tooltip>
);

const AppTopbar: React.FC = () => {
  const navigate = useNavigate();
  const { effectiveTheme, setPreference } = useAppearance();
  const { trail, title, backTo } = usePageMeta();
  const segments = splitTrail(trail);
  const isDark = effectiveTheme === 'dark';
  const themeLabel = isDark ? '切换到浅色模式' : '切换到深色模式';

  return (
    <header className="flex h-12 items-center justify-between gap-3 px-4">
      <div className="flex min-w-0 items-center gap-2">
        {backTo && (
          <IconTip label="返回上一级">
            <Button variant="ghost" size="icon" aria-label="返回上一级" onClick={() => navigate(backTo)}>
              <ArrowLeft className="size-4" />
            </Button>
          </IconTip>
        )}
        <nav className="flex min-w-0 items-center gap-1.5 text-sm" aria-label="页面路径">
          {segments.map((segment) => (
            <React.Fragment key={segment}>
              <span className="hidden truncate text-muted-foreground md:inline">{segment}</span>
              <span className="hidden text-muted-foreground md:inline" aria-hidden="true">/</span>
            </React.Fragment>
          ))}
          <span className="truncate font-medium">{title}</span>
        </nav>
      </div>
      <IconTip label={themeLabel}>
        <Button variant="ghost" size="icon" aria-label={themeLabel} onClick={() => setPreference(isDark ? 'light' : 'dark')}>
          {isDark ? <Sun className="size-4" /> : <Moon className="size-4" />}
        </Button>
      </IconTip>
    </header>
  );
};

export default AppTopbar;
