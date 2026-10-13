import React, { useEffect, useState } from "react";
import { WorkbenchPage, SectionCard } from "@/componets/Workbench";
import { Button, Form, Input, Popconfirm, Space, Table, Tag, notification } from "antd";
import { getProfile, updateProfile, listSessions, revokeSession, revokeOtherSessions, SessionSummary } from "@/api/account";
import { useAuth } from "@/layouts/RootAuthProvider";
import { useNavigate } from "react-router-dom";

const AuthInfo: React.FC = () => {
  const [form] = Form.useForm();
  const { logout } = useAuth();
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const [revokingOthers, setRevokingOthers] = useState(false);

  const loadSessions = async () => {
    setSessionsLoading(true);
    try {
      const rsp = await listSessions();
      if (rsp && rsp.code === 200 && rsp.data) {
        setSessions(rsp.data);
      }
    } finally {
      setSessionsLoading(false);
    }
  };

  useEffect(() => {
    loadSessions();
  }, []);

  const onRevokeSession = async (id: string) => {
    setRevokingId(id);
    try {
      const rsp = await revokeSession(id);
      if (rsp && rsp.code === 200) {
        notification.success({ message: "已退出该设备" });
        await loadSessions();
      }
    } finally {
      setRevokingId(null);
    }
  };

  const onRevokeOthers = async () => {
    setRevokingOthers(true);
    try {
      const rsp = await revokeOtherSessions();
      if (rsp && rsp.code === 200) {
        notification.success({ message: "已退出其它所有设备" });
        await loadSessions();
      }
    } finally {
      setRevokingOthers(false);
    }
  };

  const loadProfile = async () => {
    const rsp = await getProfile();
    if (rsp && rsp.code === 200) {
      form.setFieldsValue({
        email: rsp.data?.email ?? "",
      });
    }
  };

  useEffect(() => {
    loadProfile();
  }, []);

  const onFinish = async (values: any) => {
    setLoading(true);
    try {
      const payload = {
        email: values.email,
        oldPassword: values.oldPassword,
        newPassword: values.newPassword,
        confirmPassword: values.confirmPassword,
      };
      const rsp = await updateProfile(payload);
      if (rsp && rsp.code === 200) {
        notification.success({
          message: "更新成功",
          description: "账号信息已更新，请重新登录。",
        });
        logout();
        navigate("/login", { replace: true });
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <WorkbenchPage eyebrow="SYSTEM / ACCOUNT" title="账号与安全" description="更新登录邮箱或密码后，为保护账号安全会要求重新登录。">
      <SectionCard title="登录凭据" description="请使用可长期访问的邮箱，以便接收密码恢复邮件。">
      <Form form={form} layout="vertical" onFinish={onFinish}>
        <Form.Item
          label="邮箱"
          name="email"
          rules={[{ required: true, message: "请输入邮箱" }]}
        >
          <Input placeholder="请输入邮箱" />
        </Form.Item>
        <Form.Item
          label="旧密码"
          name="oldPassword"
          rules={[{ required: true, message: "请输入旧密码" }]}
        >
          <Input.Password placeholder="请输入旧密码" />
        </Form.Item>
        <Form.Item label="新密码" name="newPassword">
          <Input.Password placeholder="不修改请留空" />
        </Form.Item>
        <Form.Item
          label="确认新密码"
          name="confirmPassword"
          dependencies={["newPassword"]}
          rules={[
            ({ getFieldValue }) => ({
              validator(_, value) {
                const newPassword = getFieldValue("newPassword");
                if (!newPassword && !value) {
                  return Promise.resolve();
                }
                if (newPassword === value) {
                  return Promise.resolve();
                }
                return Promise.reject(new Error("两次输入的新密码不一致"));
              },
            }),
          ]}
        >
          <Input.Password placeholder="请再次输入新密码" />
        </Form.Item>
        <Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={loading}>保存账号信息</Button>
            <Button onClick={() => form.resetFields()}>还原本次修改</Button>
          </Space>
        </Form.Item>
      </Form>
      </SectionCard>
      <SectionCard
        title="活跃会话"
        description="列出当前仍能用来登录本账号的设备/浏览器；不认识的设备可以单独退出。"
        extra={
          <Popconfirm
            title="退出其它所有设备？"
            description="当前设备保持登录，其它所有会话将立即失效。"
            onConfirm={onRevokeOthers}
            okButtonProps={{ loading: revokingOthers }}
          >
            <Button danger disabled={sessions.length <= 1}>退出其它所有设备</Button>
          </Popconfirm>
        }
      >
        <Table<SessionSummary>
          rowKey="id"
          size="small"
          loading={sessionsLoading}
          dataSource={sessions}
          pagination={false}
          columns={[
            {
              title: "设备",
              dataIndex: "userAgent",
              render: (userAgent: string, record) => (
                <Space>
                  <span>{userAgent || "未知设备"}</span>
                  {record.current && <Tag color="blue">当前设备</Tag>}
                  {record.remembered && <Tag>已记住</Tag>}
                </Space>
              ),
            },
            { title: "IP", dataIndex: "clientIp", render: (ip: string) => ip || "-" },
            {
              title: "最近使用",
              dataIndex: "lastUsedAt",
              render: (value: string) => (value ? new Date(value).toLocaleString() : "-"),
            },
            {
              title: "操作",
              key: "actions",
              render: (_: unknown, record) =>
                record.current ? (
                  <span>—</span>
                ) : (
                  <Popconfirm title="退出该设备？" onConfirm={() => onRevokeSession(record.id)}>
                    <Button type="link" danger loading={revokingId === record.id}>退出</Button>
                  </Popconfirm>
                ),
            },
          ]}
        />
      </SectionCard>
    </WorkbenchPage>
  );
};

export default AuthInfo;
