import {
  Avatar, AvatarFallback, Descriptions, Dialog, DialogContent, DialogHeader, DialogTitle,
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
  useSiderToggle,
} from '@modra/react';
import { ChevronUp, LogOut, Moon, UserRound, Sun, Timer } from 'lucide-react';
import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAppearance } from '@/appearance';
import { useAuth } from './RootAuthProvider';
import { formatSessionRemaining, getAccountDisplay, getSessionRemainingSeconds } from './accountDisplay';

/** 会话健康度：用于头像角标，避免把“还剩多久续期”藏在弹窗里。 */
const sessionTone = (remaining: number): 'ok' | 'soon' | 'expired' => {
  if (remaining <= 0) return 'expired';
  if (remaining <= 300) return 'soon';
  return 'ok';
};

const TONE_DOT: Record<ReturnType<typeof sessionTone>, string> = {
  ok: 'bg-success',
  soon: 'bg-warning',
  expired: 'bg-destructive',
};

const SidebarAccount: React.FC = () => {
  const { authInfo, logout } = useAuth();
  const navigate = useNavigate();
  const { effectiveTheme, setPreference } = useAppearance();
  const { collapsed } = useSiderToggle();
  const [sessionOpen, setSessionOpen] = useState(false);
  const [now, setNow] = useState(Date.now());
  const account = getAccountDisplay(authInfo.name);
  const sessionRemaining = getSessionRemainingSeconds(authInfo.tokenExpireTime, now);
  const tone = sessionTone(sessionRemaining);
  const isDark = effectiveTheme === 'dark';

  useEffect(() => {
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, []);

  return (
    <section className="border-t border-border p-2">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            className="flex w-full items-center gap-2 rounded-md p-1.5 text-left hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            aria-label={`打开 ${account.email} 的账号菜单`}
          >
            <span className="relative shrink-0">
              <Avatar className="size-8">
                <AvatarFallback>{account.initial}</AvatarFallback>
              </Avatar>
              <i aria-hidden="true" className={`absolute -bottom-0.5 -end-0.5 size-2.5 rounded-full ring-2 ring-background ${TONE_DOT[tone]}`} />
            </span>
            {!collapsed && (
              <>
                <span className="min-w-0 flex-1 leading-tight">
                  <span className="block truncate text-sm font-medium">{account.localName}</span>
                  {account.domainName && (
                    <span className="block truncate text-xs text-muted-foreground">@{account.domainName}</span>
                  )}
                </span>
                <ChevronUp className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              </>
            )}
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent side={collapsed ? 'right' : 'top'} align="start" className="w-60">
          <div className="px-2 py-1.5 text-sm">
            <div className="font-medium">{account.localName}</div>
            {account.domainName && <div className="text-xs text-muted-foreground">@{account.domainName}</div>}
            <p className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
              <i aria-hidden="true" className={`size-2 rounded-full ${TONE_DOT[tone]}`} />
              {tone === 'expired' ? '会话待刷新' : `${formatSessionRemaining(sessionRemaining)} 后续期`}
            </p>
          </div>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => navigate('/systems/account')}>
            <UserRound className="size-4" aria-hidden="true" />账号与安全
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => setSessionOpen(true)}>
            <Timer className="size-4" aria-hidden="true" />会话信息
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => setPreference(isDark ? 'light' : 'dark')}>
            {isDark ? <Sun className="size-4" aria-hidden="true" /> : <Moon className="size-4" aria-hidden="true" />}
            切换到{isDark ? '浅色' : '深色'}模式
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onSelect={() => logout()}>
            <LogOut className="size-4" aria-hidden="true" />退出登录
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={sessionOpen} onOpenChange={setSessionOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>会话信息</DialogTitle>
          </DialogHeader>
          <Descriptions
            column={1}
            items={[
              { key: 'account', label: '账号', children: account.email },
              { key: 'userId', label: '用户 ID', children: authInfo.userId || '-' },
              { key: 'remaining', label: '续期剩余', children: formatSessionRemaining(sessionRemaining) },
              {
                key: 'expireAt',
                label: '续期时间',
                children: authInfo.tokenExpireTime > 0
                  ? new Date(authInfo.tokenExpireTime * 1000).toLocaleString()
                  : '-',
              },
            ]}
          />
        </DialogContent>
      </Dialog>
    </section>
  );
};

export default SidebarAccount;
