import { AppShell, Menu, SiderCollapseButton, useSiderToggle } from '@modra/react';
import type { MenuItemType } from '@modra/react';
import React, { Suspense, useEffect, useMemo, useState } from 'react';
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { ErrorBoundary } from 'react-error-boundary';
import { IAuthContext, useAuth } from './RootAuthProvider';
import Loading from '@/components/Loading';
import MenuIcon from '@/ui/menuIcons';
import SidebarAccount from './SidebarAccount';
import AppTopbar from './AppTopbar';
import { PageMetaProvider } from './PageMeta';
import { IMenuLink, menus } from '@/router/menu';
import { persistSidebarCollapsed, readSidebarCollapsed } from './sidebarPreference';
import { buildLoginRedirectPath } from '@/utils/safeRedirect';

const ErrorFallback = ({ error, resetErrorBoundary }: any) => (
  <div className="mx-auto flex max-w-md flex-col items-center gap-3 py-16 text-center" role="alert">
    <span className="text-xs font-medium tracking-wide text-destructive">连接中断</span>
    <h2 className="text-lg font-semibold">此页面暂时无法加载</h2>
    <p className="text-sm text-muted-foreground">{error?.message || '请检查网络或服务状态后重新加载。'}</p>
    <button
      className="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:opacity-90"
      onClick={resetErrorBoundary}
    >
      重新加载
    </button>
  </div>
);

const toLeaf = (item: IMenuLink, parentPath = ''): MenuItemType => {
  const targetPath = item.path.startsWith('/') ? item.path : `${parentPath}/${item.path}`;
  return {
    key: item.path,
    title: item.title,
    label: <Link to={targetPath}>{item.title}</Link>,
    icon: item.icon ? <MenuIcon name={item.icon} /> : undefined,
  };
};

// 二级菜单拍平成分组，省掉一次手风琴点击；收起态由 Menu 自动退化成图标加分隔线。
const menuItems: MenuItemType[] = menus.map((section) =>
  section.children?.length
    ? {
      key: `group-${section.path}`,
      type: 'group',
      label: section.title,
      children: section.children.map((child) => toLeaf(child, section.path)),
    }
    : toLeaf(section));

// 详情页/创建页不在菜单里，退回到最长前缀匹配，避免导航整体失去选中态。
const menuKeys = menus
  .flatMap((section) => (section.children?.length ? section.children : [section]))
  .map((item) => item.path);

const findSelectedKey = (pathname: string): string | undefined => {
  const exact = menuKeys.find((key) => key === pathname);
  if (exact) return exact;
  return menuKeys
    .filter((key) => key !== '/' && pathname.startsWith(key))
    .sort((a, b) => b.length - a.length)[0];
};

const Brand: React.FC = () => {
  const { collapsed } = useSiderToggle();
  return (
    <div className={`flex items-center gap-2 px-3 ${collapsed ? 'flex-col justify-center py-2' : 'h-14 justify-between'}`}>
      <span className="truncate text-base font-semibold tracking-wider" aria-label="YAGINX">
        {collapsed ? 'YG' : 'YAGINX'}
      </span>
      <SiderCollapseButton aria-label="切换导航" />
    </div>
  );
};

const UserLayout: React.FC = () => {
  const location = useLocation();
  const navigate = useNavigate();
  const { isAuthorized, authReady }: IAuthContext = useAuth();
  const [collapsed, setCollapsed] = useState(readSidebarCollapsed);
  const updateCollapsed = (next: boolean) => {
    setCollapsed(next);
    persistSidebarCollapsed(next);
  };

  useEffect(() => {
    // 等静默刷新跑完再决定要不要踢到登录页：token 已经不在 localStorage 里了，
    // 挂载瞬间 isAuthorized 可能暂时是 false，但 refresh cookie 可能仍然有效。
    if (!authReady) return;
    if (!isAuthorized) {
      const redirect = buildLoginRedirectPath(location.pathname, location.search);
      navigate(`/login?redirect=${encodeURIComponent(redirect)}`, { replace: true });
    }
  }, [authReady, isAuthorized, navigate, location.pathname, location.search]);

  const selectedKey = useMemo(() => findSelectedKey(location.pathname), [location.pathname]);

  if (!authReady) {
    // 静默刷新还没跑完：不知道到底算不算登录，先别渲染任何东西，也别跳转。
    return <Loading />;
  }

  if (!isAuthorized) {
    const redirect = buildLoginRedirectPath(location.pathname, location.search);
    return <Navigate to={`/login?redirect=${encodeURIComponent(redirect)}`} replace />;
  }

  return (
    <PageMetaProvider>
      <AppShell
        fullscreen
        brand={<span className="text-base font-semibold tracking-wider">YAGINX</span>}
        siderCollapsed={collapsed}
        onSiderCollapse={updateCollapsed}
        siderTrigger={null}
        header={<AppTopbar />}
        sidebar={(
          <div className="flex h-full min-h-0 flex-col">
            <Brand />
            <div className="min-h-0 flex-1 overflow-y-auto">
              <Menu mode="inline" items={menuItems} selectedKeys={selectedKey ? [selectedKey] : []} />
            </div>
            <SidebarAccount />
          </div>
        )}
      >
        <div className="w-full p-3">
          <ErrorBoundary FallbackComponent={ErrorFallback} resetKeys={[location.pathname]} onReset={() => window.location.reload()}>
            <Suspense fallback={<Loading />}>
              <Outlet />
            </Suspense>
          </ErrorBoundary>
        </div>
      </AppShell>
    </PageMetaProvider>
  );
};

export default UserLayout;
