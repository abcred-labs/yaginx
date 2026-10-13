import { ArrowLeftOutlined, BulbFilled, BulbOutlined, MenuUnfoldOutlined } from '@ant-design/icons';
import { Tooltip } from 'antd';
import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useAppearance } from '@/appearance';
import { splitTrail, usePageMeta } from './PageMeta';

interface AppTopbarProps {
  isMobile?: boolean;
  onOpenNavigation?: () => void;
}

const AppTopbar: React.FC<AppTopbarProps> = ({ isMobile = false, onOpenNavigation }) => {
  const navigate = useNavigate();
  const { effectiveTheme, setPreference } = useAppearance();
  const { trail, title, backTo } = usePageMeta();
  const segments = splitTrail(trail);
  const isDark = effectiveTheme === 'dark';

  return (
    <header className="app-topbar">
      <div className="app-topbar__lead">
        {isMobile && (
          <button className="app-topbar__icon-btn" aria-label="打开导航" onClick={onOpenNavigation}>
            <MenuUnfoldOutlined />
          </button>
        )}

        {backTo && (
          <Tooltip title="返回上一级">
            <button className="app-topbar__icon-btn" aria-label="返回上一级" onClick={() => navigate(backTo)}>
              <ArrowLeftOutlined />
            </button>
          </Tooltip>
        )}

        <nav className="app-topbar__crumbs" aria-label="页面路径">
          {segments.map((segment) => (
            <React.Fragment key={segment}>
              <span className="app-topbar__crumb">{segment}</span>
              <span className="app-topbar__crumb-sep" aria-hidden="true">/</span>
            </React.Fragment>
          ))}
          <span className="app-topbar__crumb app-topbar__crumb--current">{title}</span>
        </nav>
      </div>

      <div className="app-topbar__trail">
        <Tooltip title={isDark ? '切换到浅色模式' : '切换到深色模式'}>
          <button
            className="app-topbar__icon-btn"
            aria-label={isDark ? '切换到浅色模式' : '切换到深色模式'}
            onClick={() => setPreference(isDark ? 'light' : 'dark')}
          >
            {isDark ? <BulbOutlined /> : <BulbFilled />}
          </button>
        </Tooltip>
      </div>
    </header>
  );
};

export default AppTopbar;
