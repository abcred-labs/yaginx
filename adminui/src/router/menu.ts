export interface IMenuLink {
  path: string;
  title: string;
  icon?: string;
  children?: IMenuLink[];
}

export const menus: IMenuLink[] = [
  {
    path: "/",
    title: "概览",
    icon: "HomeOutlined",
  },
  {
    path: "/website",
    title: "配置",
    icon: "ClusterOutlined",
    children: [
      { path: "/website", title: "站点", icon: "GlobalOutlined" },
      { path: "/access-control/ip-blacklist", title: "访问控制", icon: "StopOutlined" }
    ]
  },
  {
    path: "/monitor/traffic/request-trend",
    title: "可观测性",
    icon: "RadarChartOutlined",
    children: [
      {
        path: "/monitor/traffic/request-trend",
        title: "流量分析",
        icon: "LineChartOutlined",
      },
      {
        path: "/monitor/requestHistory/list",
        title: "请求追踪",
        icon: "ProfileOutlined",
      },
      {
        path: "/resource/hosts",
        title: "Host 资源",
        icon: "GlobalOutlined",
      },
    ],
  },
  {
    path: "/systems/runtime",
    title: "系统",
    icon: "SettingOutlined",
    children: [
      {
        path: "/systems/runtime",
        title: "运行配置",
        icon: "SettingOutlined",
      },
      {
        path: "/systems/nodes",
        title: "节点与发布",
        icon: "ClusterOutlined",
      },
      { path: "/systems/acme", title: "ACME 订单", icon: "SafetyCertificateOutlined" },
      {
        path: "/systems/notifications",
        title: "通知",
        icon: "MailOutlined",
      },
      {
        path: "/systems/integrations",
        title: "API 集成",
        icon: "ApiOutlined",
      },
      {
        path: "/systems/database",
        title: "数据库诊断",
        icon: "DatabaseOutlined",
      }
    ],
  },
];
