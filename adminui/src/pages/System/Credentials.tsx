import React, { useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Modal, Space, Table, Tag, Typography, message } from 'antd';
import { PlusOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons';
import { SectionCard, WorkbenchPage } from '@/componets/Workbench';
import {
  createAPICredential,
  listAPICredentials,
  resetAPICredential,
  revokeAPICredential,
  type APICredential,
  type APICredentialSecret,
} from '@/api/systemCredential';

const formatTime = (value: number | null) => value ? new Date(value * 1000).toLocaleString() : '—';

const SecretNotice: React.FC<{ secret: APICredentialSecret | null; onClose: () => void }> = ({ secret, onClose }) => {
  if (!secret) return null;
  const copy = async () => {
    await navigator.clipboard.writeText(secret.token);
    message.success('Token 已复制');
  };
  return <Alert
    type="warning"
    showIcon
    closable
    onClose={onClose}
    message="请立即复制并保存 Token，关闭后无法再次查看"
    description={<Space direction="vertical" style={{ width: '100%' }}>
      <Input.Password readOnly value={secret.token} />
      <Button onClick={() => void copy()}>复制 Token</Button>
    </Space>}
  />;
};

const Credentials: React.FC = () => {
  const [credentials, setCredentials] = useState<APICredential[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm<{ name: string; purpose: string }>();
  const [secret, setSecret] = useState<APICredentialSecret | null>(null);

  const load = async () => {
    setLoading(true);
    try {
      const response = await listAPICredentials();
      if (response.code === 200) setCredentials(response.data ?? []);
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, []);

  const create = async () => {
    const values = await form.validateFields();
    const response = await createAPICredential(values);
    if (response.code === 200) {
      setModalOpen(false);
      form.resetFields();
      setSecret(response.data);
      await load();
      message.success('凭据已创建');
    }
  };

  const reset = (item: APICredential) => Modal.confirm({
    title: '重置凭据',
    content: `重置后 ${item.tokenPrefix} 立即失效，并生成新的 Token。`,
    okText: '重置',
    cancelText: '取消',
    onOk: async () => {
      const response = await resetAPICredential(item.id);
      if (response.code === 200) {
        setSecret(response.data);
        await load();
        message.success('凭据已重置');
      }
    },
  });

  const revoke = (item: APICredential) => Modal.confirm({
    title: '吊销凭据',
    content: `吊销 ${item.tokenPrefix} 后不可恢复，确认继续吗？`,
    okText: '吊销',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      const response = await revokeAPICredential(item.id);
      if (response.code === 200) {
        await load();
        message.success('凭据已吊销');
      }
    },
  });

  return <WorkbenchPage eyebrow="SYSTEM / CREDENTIALS" title="API 凭据" description="统一管理 MCP 和未来接口使用的 Bearer token。">
    <SectionCard title="凭据列表" description="数据库只保存 hash。Token 只在创建或重置后显示一次。">
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Space>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalOpen(true)}>创建凭据</Button>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={() => void load()}>刷新</Button>
        </Space>
        {secret ? <SecretNotice secret={secret} onClose={() => setSecret(null)} /> : null}
        <Table<APICredential>
          rowKey="id"
          size="small"
          loading={loading}
          dataSource={credentials}
          pagination={false}
          columns={[
            { title: '名称', dataIndex: 'name' },
            { title: '用途', dataIndex: 'purpose' },
            { title: 'Token 前缀', dataIndex: 'tokenPrefix', render: (value: string) => <Typography.Text code>{value}</Typography.Text> },
            { title: '状态', dataIndex: 'status', render: (value: APICredential['status']) => <Tag color={value === 'active' ? 'green' : 'red'}>{value === 'active' ? '启用' : '已吊销'}</Tag> },
            { title: '创建时间', dataIndex: 'createdAt', render: formatTime },
            { title: '更新时间', dataIndex: 'updatedAt', render: formatTime },
            { title: '操作', key: 'actions', render: (_: unknown, item: APICredential) => item.status === 'active' ? <Space><Button size="small" onClick={() => reset(item)}>重置</Button><Button size="small" danger icon={<StopOutlined />} onClick={() => revoke(item)}>吊销</Button></Space> : null },
          ]}
        />
      </Space>
    </SectionCard>
    <SectionCard title="MCP 使用说明" description="MCP 复用现有管理端口。创建用途为 mcp 的凭据后，在 Agent 中使用以下地址。">
      <Typography.Paragraph copyable>{`${window.location.origin}/mcp`}</Typography.Paragraph>
      <Typography.Paragraph copyable>{`${window.location.origin}/.well-known/mcp.json`}</Typography.Paragraph>
      <Typography.Text type="secondary">请求头格式：Authorization: Bearer &lt;token&gt;</Typography.Text>
    </SectionCard>
    <Modal open={modalOpen} title="创建 API 凭据" okText="创建" cancelText="取消" onCancel={() => setModalOpen(false)} onOk={() => void create()}>
      <Form form={form} layout="vertical">
        <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true, message: '请输入名称' }]}><Input placeholder="生产 MCP" /></Form.Item>
        <Form.Item name="purpose" label="用途" rules={[{ required: true, whitespace: true, message: '请输入用途' }]}><Input placeholder="mcp" /></Form.Item>
      </Form>
    </Modal>
  </WorkbenchPage>;
};

export default Credentials;
