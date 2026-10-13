import {
  BulbFilled,
  BulbOutlined,
  LogoutOutlined,
  UpOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { Avatar, Descriptions, Dropdown, Modal, Tooltip } from "antd";
import type { MenuProps } from "antd";
import React, { useEffect, useState } from "react";
import { useAppearance } from "@/appearance";
import { useAuth } from "./RootAuthProvider";
import { useNavigate } from "react-router-dom";
import { formatSessionRemaining, getAccountDisplay, getSessionRemainingSeconds } from "./accountDisplay";

interface SidebarAccountProps {
  collapsed?: boolean;
}

/** 会话健康度：用于头像角标，避免把“还剩多久续期”藏在弹窗里。 */
const sessionTone = (remaining: number): "ok" | "soon" | "expired" => {
  if (remaining <= 0) return "expired";
  if (remaining <= 300) return "soon";
  return "ok";
};

const SidebarAccount: React.FC<SidebarAccountProps> = ({ collapsed = false }) => {
  const { authInfo, logout } = useAuth();
  const navigate = useNavigate();
  const { effectiveTheme, setPreference } = useAppearance();
  const [sessionOpen, setSessionOpen] = useState(false);
  const [now, setNow] = useState(Date.now());
  const account = getAccountDisplay(authInfo.name);
  const sessionRemaining = getSessionRemainingSeconds(authInfo.tokenExpireTime, now);
  const tone = sessionTone(sessionRemaining);

  useEffect(() => {
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, []);

  const handleMenuClick: MenuProps["onClick"] = ({ key }) => {
    if (key === "session") {
      setSessionOpen(true);
      return;
    }

    if (key === "account") {
      navigate("/systems/account");
      return;
    }

    if (key === "theme") {
      setPreference(effectiveTheme === "dark" ? "light" : "dark");
      return;
    }

    if (key === "signout") {
      logout();
    }
  };

  const menu: MenuProps = {
    items: [
      { key: "account", label: "账号与安全", icon: <UserOutlined /> },
      { key: "session", label: "会话信息", icon: <UserOutlined /> },
      {
        key: "theme",
        label: `切换到${effectiveTheme === "dark" ? "浅色" : "深色"}模式`,
        icon: effectiveTheme === "dark" ? <BulbOutlined /> : <BulbFilled />,
      },
      { type: "divider" },
      { key: "signout", label: "退出登录", icon: <LogoutOutlined />, danger: true },
    ],
    onClick: handleMenuClick,
  };

  const dropdownContent = (menuNode: React.ReactNode) => (
    <div className="sidebar-account__popup">
      <div className="sidebar-account__popup-summary">
        <Avatar className="sidebar-account__avatar" size={34}>{account.initial}</Avatar>
        <div className="sidebar-account__popup-identity">
          <span>{account.localName}</span>
          {account.domainName && <small>@{account.domainName}</small>}
        </div>
        <p className={`sidebar-account__session sidebar-account__session--${tone}`}>
          <i aria-hidden="true" />
          {tone === "expired" ? "会话待刷新" : `${formatSessionRemaining(sessionRemaining)} 后续期`}
        </p>
      </div>
      {menuNode}
    </div>
  );

  const avatar = (
    <span className={`sidebar-account__avatar-wrap sidebar-account__avatar-wrap--${tone}`}>
      <Avatar className="sidebar-account__avatar" size={collapsed ? 32 : 30}>{account.initial}</Avatar>
    </span>
  );

  const trigger = collapsed ? (
    <button className="sidebar-account__trigger sidebar-account__trigger--collapsed" aria-label={`打开 ${account.email} 的账号菜单`}>
      {avatar}
    </button>
  ) : (
    <button className="sidebar-account__trigger" aria-label={`打开 ${account.email} 的账号菜单`}>
      {avatar}
      <span className="sidebar-account__identity">
        <span className="sidebar-account__local-name">{account.localName}</span>
        {account.domainName && <span className="sidebar-account__domain-name">@{account.domainName}</span>}
      </span>
      <UpOutlined className="sidebar-account__chevron" />
    </button>
  );

  return (
    <>
      <section className={`sidebar-account${collapsed ? " sidebar-account--collapsed" : ""}`}>
        <Dropdown
          menu={menu}
          trigger={["click"]}
          placement={collapsed ? "topLeft" : "topRight"}
          dropdownRender={dropdownContent}
        >
          {collapsed ? <Tooltip title={account.email} placement="right">{trigger}</Tooltip> : trigger}
        </Dropdown>
      </section>

      <Modal
        className="session-info-modal"
        title="会话信息"
        open={sessionOpen}
        footer={null}
        onCancel={() => setSessionOpen(false)}
      >
        <Descriptions column={1} size="small">
          <Descriptions.Item label="账号">{account.email}</Descriptions.Item>
          <Descriptions.Item label="用户 ID">{authInfo.userId || "-"}</Descriptions.Item>
          <Descriptions.Item label="续期剩余">{formatSessionRemaining(sessionRemaining)}</Descriptions.Item>
          <Descriptions.Item label="续期时间">
            {authInfo.tokenExpireTime > 0
              ? new Date(authInfo.tokenExpireTime * 1000).toLocaleString()
              : "-"}
          </Descriptions.Item>
        </Descriptions>
      </Modal>
    </>
  );
};

export default SidebarAccount;
