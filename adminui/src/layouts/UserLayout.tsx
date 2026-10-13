import { Drawer, Grid, Layout, Menu, Tooltip } from 'antd';
import React, { Suspense, useEffect, useMemo, useState } from 'react';
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { ErrorBoundary } from 'react-error-boundary';
import Sider from 'antd/es/layout/Sider';
import { MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons';
import { IAuthContext, useAuth } from './RootAuthProvider';
import Loading from '@/componets/Loading';
import MenuIcon from '@/MenuIcon';
import SidebarAccount from './SidebarAccount';
import AppTopbar from './AppTopbar';
import { PageMetaProvider } from './PageMeta';
import { IMenuLink, menus } from '@/router/menu';
import { useAppearance } from '@/appearance';
import { persistSidebarCollapsed, readSidebarCollapsed } from './sidebarPreference';
import { buildLoginRedirectPath } from '@/utils/safeRedirect';

const { Content } = Layout;

const ErrorFallback = ({ error, resetErrorBoundary }: any) => (
  <div className="app-error-state" role="alert">
    <span className="app-error-state__eyebrow">连接中断</span>
    <h2>此页面暂时无法加载</h2>
    <p>{error?.message || '请检查网络或服务状态后重新加载。'}</p>
    <button className="app-error-state__action" onClick={resetErrorBoundary}>重新加载</button>
  </div>
);

const UserLayout: React.FC = () => {
  const location = useLocation();
  const navigate = useNavigate();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const { isAuthorized, authReady }: IAuthContext = useAuth();
  const { effectiveTheme } = useAppearance();
  const [collapsed, setCollapsed] = useState(readSidebarCollapsed);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const updateCollapsed = (nextCollapsed: boolean) => {
    setCollapsed(nextCollapsed);
    persistSidebarCollapsed(nextCollapsed);
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

  useEffect(() => {
    if (isMobile) {
      setMobileMenuOpen(false);
    }
  }, [isMobile, location.pathname]);

  // 展开态把二级菜单拍平成分组，省掉一次手风琴点击；收起态只剩图标，用分隔线保留分组感。
  const { groupedItems, flatItems } = useMemo(() => {
    const toLeaf = (item: IMenuLink, parentPath = '') => {
      const targetPath = item.path.startsWith('/') ? item.path : `${parentPath}/${item.path}`;
      return {
        key: item.path,
        label: <Link to={targetPath}>{item.title}</Link>,
        icon: item.icon ? <MenuIcon name={item.icon} /> : undefined,
      };
    };

    const grouped: any[] = [];
    const flat: any[] = [];

    menus.forEach((section, index) => {
      if (!section.children?.length) {
        grouped.push(toLeaf(section));
        flat.push(toLeaf(section));
        return;
      }

      grouped.push({
        key: `group-${section.path}`,
        type: 'group',
        label: <span className="sider-menu__group-label">{section.title}</span>,
        children: section.children.map((child) => toLeaf(child, section.path)),
      });

      if (index > 0) flat.push({ key: `divider-${section.path}`, type: 'divider' });
      section.children.forEach((child) => flat.push(toLeaf(child, section.path)));
    });

    return { groupedItems: grouped, flatItems: flat };
  }, []);

  // 详情页/创建页不在菜单里，退回到最长前缀匹配，避免导航整体失去选中态。
  const selectedKey = useMemo(() => {
    const keys = menus.flatMap((section) => (section.children?.length ? section.children : [section])).map((item) => item.path);
    const exact = keys.find((key) => key === location.pathname);
    if (exact) return exact;
    return keys
      .filter((key) => key !== '/' && location.pathname.startsWith(key))
      .sort((a, b) => b.length - a.length)[0];
  }, [location.pathname]);

  const navMenu = (compact = false) => (
    <Menu
      className={`sider-menu${compact ? ' sider-menu--compact' : ''}`}
      theme={effectiveTheme}
      mode="inline"
      selectedKeys={selectedKey ? [selectedKey] : []}
      items={compact ? flatItems : groupedItems}
    />
  );

  if (!authReady) {
    // 静默刷新还没跑完：不知道到底算不算登录，先别渲染任何东西，也别跳转。
    return <Loading />;
  }

  if (!isAuthorized) {
    const redirect = buildLoginRedirectPath(location.pathname, location.search);
    return <Navigate to={`/login?redirect=${encodeURIComponent(redirect)}`} replace />;
  }

  const sideWidth = collapsed ? 72 : 232;
  return (
    <PageMetaProvider>
      <Layout className="admin-shell" hasSider={!isMobile}>
        {isMobile ? (
          <Drawer
            className="app-mobile-nav"
            open={mobileMenuOpen}
            onClose={() => setMobileMenuOpen(false)}
            placement="left"
            width={264}
            closable={false}
            styles={{ body: { padding: 0 } }}
          >
            <div className="app-sider__brand">
              <div className="logo">YAGINX</div>
            </div>
            {navMenu()}
            <SidebarAccount />
          </Drawer>
        ) : (
          <Sider
            className="app-sider"
            collapsible
            collapsed={collapsed}
            onCollapse={updateCollapsed}
            trigger={null}
            width={232}
            collapsedWidth={72}
            style={{ overflow: 'hidden', height: '100vh', position: 'fixed', left: 0, top: 0, bottom: 0, zIndex: 1000 }}
          >
            <div className={`app-sider__brand${collapsed ? ' app-sider__brand--collapsed' : ''}`}>
              {collapsed ? (
                <div className="logo logo--collapsed" aria-label="YAGINX">YG</div>
              ) : (
                <div className="logo">YAGINX</div>
              )}
              <Tooltip title={collapsed ? '展开导航' : '收起导航'} placement="right">
                <button
                  className="app-sider__toggle"
                  aria-label={collapsed ? '展开导航' : '收起导航'}
                  aria-expanded={!collapsed}
                  onClick={() => updateCollapsed(!collapsed)}
                >
                  {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                </button>
              </Tooltip>
            </div>

            {navMenu(collapsed)}
            <SidebarAccount collapsed={collapsed} />
          </Sider>
        )}

        <Layout className="app-main-layout" style={{ marginLeft: isMobile ? 0 : sideWidth, minHeight: '100vh' }}>
          <AppTopbar isMobile={isMobile} onOpenNavigation={() => setMobileMenuOpen(true)} />

          <Content className="app-content">
            <div className="app-content__surface">
              <ErrorBoundary FallbackComponent={ErrorFallback} onReset={() => window.location.reload()}>
                <Suspense fallback={<Loading />}>
                  <Outlet />
                </Suspense>
              </ErrorBoundary>
            </div>
          </Content>
        </Layout>
      </Layout>
    </PageMetaProvider>
  );
};

export default UserLayout;
