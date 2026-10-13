import { websiteResponseID, websiteUpsert, type WebsiteUpsertInput } from '@/api/website';
import { WorkbenchPage } from '@/componets/Workbench';
import { Button, Form, Input, Space, notification } from 'antd';
import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';

export const Create: React.FC = () => {
  const navigate = useNavigate();
  const [form] = Form.useForm<WebsiteUpsertInput>();
  const [saving, setSaving] = useState(false);
  const create = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      const response = await websiteUpsert(values);
      const id = websiteResponseID(response);
      if (id === null) {
        notification.error({ message: '站点创建失败', description: '服务端未返回有效的站点 ID，请重新登录或稍后重试。' });
        return;
      }
      notification.success({ message: '站点已创建', description: '接下来配置主机、转发规则和静态响应。' });
      navigate(`/website/${id}`);
    } finally { setSaving(false); }
  };
  return <WorkbenchPage eyebrow="CONFIGURATION / NEW WEBSITE" title="创建站点" description="先建立站点标识；其余配置将在专属工作区内完成。" backTo="/website/list" extra={<Space><Button onClick={() => navigate('/website/list')}>取消</Button><Button type="primary" loading={saving} onClick={create}>创建并继续配置</Button></Space>}>
    <Form form={form} layout="vertical" className="create-resource-form"><Form.Item name="name" label="站点名称" rules={[{ required: true, whitespace: true, message: '请输入站点名称' }]} extra="用于在控制台中识别此网关站点。"><Input size="large" autoFocus placeholder="例如：生产 API 网关" /></Form.Item></Form>
  </WorkbenchPage>;
};
