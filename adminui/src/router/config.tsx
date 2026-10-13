import React from 'react';
import { Navigate } from 'react-router-dom';
export interface IRouteConfig {
  key?: string;
  // 路由路径, 必要时会带上*等规则匹配模式
  routePath?: string;
  // 导航路径, 浏览器地址栏显示的地址
  navPath?: string;
  // 路由组件
  element?: any;
  // 302 跳转到其他地方
  redirect?: string;
  // 仅出现在Route, 不在Nav中出现
  isHide?: boolean;

  // 不是一个Link, 只做显示
  notLink?: boolean;
  // 路由信息
  title?: string;
  icon?: string;
  // 是否校验权限, false 为不校验, 不存在该属性或者为true 为校验, 子路由会继承父路由的 auth 属性
  auth?: boolean;
  children?: IRouteConfig[];
}

const layouts: IRouteConfig[] = [
  {
    routePath: '/',
    title: '/',
    isHide: true,
    element: React.lazy(async () => await import('@/layouts/RootLayout')),
    children: [
      {
        routePath: '/login',
        title: '登录',
        isHide: true,
        element: React.lazy(async () => await import('@/pages/Login')),
      },
      {
        // routePath: '*',
        // title: '授权页面模板',
        // isHide: true,
        element: React.lazy(async () => await import('@/layouts/UserLayout')),
        children: [
          {
            routePath: '/',
            // title: '仪表盘',
            // icon: 'BankOutlined',
            element: React.lazy(async () => await import('@/pages/Home')),
          },
          {
            routePath: 'website/*',
            title: '网站列表',
            icon: 'BankOutlined',
            element: React.lazy(async () => await import('@/pages/Websites')),
          },
          {
            routePath: 'domain/*',
            // title: '网站列表',
            // icon: 'BankOutlined',
            element: React.lazy(async () => await import('@/pages/WebDomains')),
          },
          {
            routePath: 'access-control/ip-blacklist',
            title: '访问控制',
            element: React.lazy(async () => await import('@/pages/Monitors/IPBlacklist/List')),
          },
          {
            routePath: 'resource/hosts',
            title: 'Host 资源',
            element: React.lazy(async () => await import('@/pages/ResourceViews/HostCatalog')),
          },
          {
            routePath: 'resource/hosts/view/:host',
            title: 'Host 详情',
            element: React.lazy(async () => await import('@/pages/ResourceViews/DomainDetail')),
          },
          {
            routePath: 'monitor/*',
            // title: '网站列表',
            // icon: 'BankOutlined',
            element: React.lazy(async () => await import('@/pages/Monitors')),
          },
          {
            routePath: 'monitor/traffic/*',
            element: React.lazy(async () => await import('@/pages/Monitors/HostTraffics')),
          },
          {
            routePath: 'systems',
            element: () => <Navigate to="/systems/runtime" replace />,
          },
          {
            routePath: 'systems/auth-info',
            element: () => <Navigate to="/systems/account" replace />,
          },
          {
            routePath: 'systems/account',
            title: '账号与安全',
            element: React.lazy(async () => await import('@/pages/System/AuthInfo')),
          },
          {
            routePath: 'systems/setting',
            element: () => <Navigate to="/systems/runtime" replace />,
          }
          ,{
            routePath: 'systems/credentials',
            element: () => <Navigate to="/systems/integrations" replace />,
          }
          ,{
            routePath: 'systems/runtime',
            title: '运行配置',
            element: React.lazy(async () => await import('@/pages/System/RuntimeSettings')),
          },{
            routePath: 'systems/nodes',
            title: '节点与发布',
            element: React.lazy(async () => await import('@/pages/System/NodeSettings')),
          },{
            routePath: 'systems/acme',
            title: 'ACME 订单',
            element: React.lazy(async () => await import('@/pages/System/ACMEOrders')),
          },{
            routePath: 'systems/notifications',
            title: '通知',
            element: React.lazy(async () => await import('@/pages/System/NotificationSettings')),
          },{
            routePath: 'systems/integrations',
            title: 'API 集成',
            element: React.lazy(async () => await import('@/pages/System/Credentials')),
          },{
            routePath: 'systems/database',
            title: '数据库诊断',
            element: React.lazy(async () => await import('@/pages/Monitors/DatabasePerformance')),
          },{
            routePath: 'systems/mcp',
            element: () => <Navigate to="/systems/integrations" replace />,
          }
        ]
      },

    ]
  }
];

export default layouts;
