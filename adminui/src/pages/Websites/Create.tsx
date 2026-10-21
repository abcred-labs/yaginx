import { Button, Form, FormControl, FormField, FormItem, FormLabel, FormMessage, Input } from '@modra/react';
import React from 'react';
import { useForm } from 'react-hook-form';
import { useNavigate } from 'react-router-dom';
import { websiteResponseID, websiteUpsert, type WebsiteUpsertInput } from '@/api/website';
import { notifyError, notifySuccess } from '@/ui/notify';
import { WorkbenchPage } from '@/ui/workbench';

export const Create: React.FC = () => {
  const navigate = useNavigate();
  const form = useForm<WebsiteUpsertInput>({ defaultValues: { name: '' } });
  const create = async (values: WebsiteUpsertInput) => {
    const response = await websiteUpsert(values);
    const id = websiteResponseID(response);
    if (id === null) {
      notifyError('站点创建失败', '服务端未返回有效的站点 ID，请重新登录或稍后重试。');
      return;
    }
    notifySuccess('站点已创建', '接下来配置主机、转发规则和静态响应。');
    navigate(`/website/${id}`);
  };
  return (
    <WorkbenchPage
      eyebrow="CONFIGURATION / NEW WEBSITE"
      title="创建站点"
      description="先建立站点标识；其余配置将在专属工作区内完成。"
      backTo="/website/list"
      extra={<>
        <Button variant="outline" onClick={() => navigate('/website/list')}>取消</Button>
        <Button type="submit" form="create-website-form" loading={form.formState.isSubmitting}>创建并继续配置</Button>
      </>}
    >
      <Form {...form} layout="vertical">
        <form id="create-website-form" className="max-w-xl" noValidate onSubmit={form.handleSubmit(create)}>
          <FormField
            control={form.control}
            name="name"
            rules={{ validate: (value) => (typeof value === 'string' && value.trim() !== '') || '请输入站点名称' }}
            render={({ field }) => (
              <FormItem required extra="用于在控制台中识别此网关站点。">
                <FormLabel>站点名称</FormLabel>
                <FormControl><Input {...field} autoFocus placeholder="例如：生产 API 网关" autoComplete="off" /></FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </WorkbenchPage>
  );
};
