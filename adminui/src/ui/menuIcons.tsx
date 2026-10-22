import {
  Ban, ClipboardList, Database, Globe, Home, LineChart, Mail, Network, Plug, Radar,
  Settings, ShieldCheck, TriangleAlert,
} from '@/ui/icons';
import type { Icon } from '@/ui/icons';
import React from 'react';

/** router/menu.ts 里的 icon 字段仍沿用历史名称，这里统一映射到 ui/icons 的图标。 */
const ICONS: Record<string, Icon> = {
  HomeOutlined: Home,
  ClusterOutlined: Network,
  GlobalOutlined: Globe,
  StopOutlined: Ban,
  RadarChartOutlined: Radar,
  LineChartOutlined: LineChart,
  ProfileOutlined: ClipboardList,
  SettingOutlined: Settings,
  SafetyCertificateOutlined: ShieldCheck,
  MailOutlined: Mail,
  ApiOutlined: Plug,
  DatabaseOutlined: Database,
};

export const MenuIcon: React.FC<{ name?: string }> = ({ name }) => {
  const Icon = (name && ICONS[name]) || TriangleAlert;
  return <Icon className="size-4" aria-hidden="true" />;
};

export default MenuIcon;
