import {
  Badge, Button, DataTable, Form, FormActions, FormControl, FormField, FormItem, FormLabel, FormMessage, Input,
} from '@modra/react';
import type { ColumnDef } from '@tanstack/react-table';
import React, { useEffect, useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useNavigate } from 'react-router-dom';
import { getProfile, updateProfile, listSessions, revokeSession, revokeOtherSessions, SessionSummary } from '@/api/account';
import { useAuth } from '@/layouts/RootAuthProvider';
import { confirmDanger } from '@/ui/confirmAction';
import { notifySuccess } from '@/ui/notify';
import { SectionCard, WorkbenchPage, WorkbenchTabs } from '@/ui/workbench';

interface ProfileValues {
  email: string;
  oldPassword: string;
  newPassword: string;
  confirmPassword: string;
}

const EMPTY_VALUES: ProfileValues = { email: '', oldPassword: '', newPassword: '', confirmPassword: '' };

const AuthInfo: React.FC = () => {
  const { logout } = useAuth();
  const navigate = useNavigate();
  const form = useForm<ProfileValues>({ defaultValues: EMPTY_VALUES });
  const [loadedEmail, setLoadedEmail] = useState('');
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);

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
    void loadSessions();
    void (async () => {
      const rsp = await getProfile();
      if (rsp && rsp.code === 200) {
        const email = rsp.data?.email ?? '';
        setLoadedEmail(email);
        form.reset({ ...EMPTY_VALUES, email });
      }
    })();
    // 只在挂载时加载一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onSubmit = async (values: ProfileValues) => {
    const rsp = await updateProfile({
      email: values.email,
      oldPassword: values.oldPassword,
      newPassword: values.newPassword,
      confirmPassword: values.confirmPassword,
    });
    if (rsp && rsp.code === 200) {
      notifySuccess('更新成功', '账号信息已更新，请重新登录。');
      logout();
      navigate('/login', { replace: true });
    }
  };

  const revokeOthers = () => confirmDanger({
    title: '退出其它所有设备？',
    description: '当前设备保持登录，其它所有会话将立即失效。',
    onConfirm: async () => {
      const rsp = await revokeOtherSessions();
      if (rsp && rsp.code === 200) {
        notifySuccess('已退出其它所有设备');
        await loadSessions();
      }
    },
  });

  const revokeOne = (id: string) => confirmDanger({
    title: '退出该设备？',
    onConfirm: async () => {
      const rsp = await revokeSession(id);
      if (rsp && rsp.code === 200) {
        notifySuccess('已退出该设备');
        await loadSessions();
      }
    },
  });

  const columns = useMemo<ColumnDef<SessionSummary, unknown>[]>(() => [
    {
      accessorKey: 'userAgent',
      header: '设备',
      cell: ({ row }) => (
        <span className="flex flex-wrap items-center gap-2">
          <span>{row.original.userAgent || '未知设备'}</span>
          {row.original.current && <Badge variant="info">当前设备</Badge>}
          {row.original.remembered && <Badge variant="secondary">已记住</Badge>}
        </span>
      ),
    },
    { accessorKey: 'clientIp', header: 'IP', cell: ({ row }) => row.original.clientIp || '-' },
    {
      accessorKey: 'lastUsedAt',
      header: '最近使用',
      cell: ({ row }) => (row.original.lastUsedAt ? new Date(row.original.lastUsedAt).toLocaleString() : '-'),
    },
    {
      id: 'actions',
      header: '操作',
      cell: ({ row }) => row.original.current
        ? <span>—</span>
        : <Button variant="destructive-ghost" size="sm" onClick={() => revokeOne(row.original.id)}>退出</Button>,
    },
    // revokeOne 只依赖稳定的 setState 与 API，列定义无需随其重建
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], []);

  const credentialsCard = (
    <SectionCard title="登录凭据" description="请使用可长期访问的邮箱，以便接收密码恢复邮件。">
      <Form {...form} layout="vertical">
        <form className="flex max-w-xl flex-col gap-4" onSubmit={form.handleSubmit(onSubmit)} noValidate>
          <FormField
            control={form.control}
            name="email"
            rules={{ required: '请输入邮箱' }}
            render={({ field }) => (
              <FormItem>
                <FormLabel>邮箱</FormLabel>
                <FormControl><Input {...field} type="email" placeholder="请输入邮箱" autoComplete="username" /></FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="oldPassword"
            rules={{ required: '请输入旧密码' }}
            render={({ field }) => (
              <FormItem>
                <FormLabel>旧密码</FormLabel>
                <FormControl><Input {...field} type="password" placeholder="请输入旧密码" autoComplete="current-password" /></FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="newPassword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>新密码</FormLabel>
                <FormControl><Input {...field} type="password" placeholder="不修改请留空" autoComplete="new-password" /></FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="confirmPassword"
            rules={{
              validate: (value, values) => {
                if (!values.newPassword && !value) return true;
                return values.newPassword === value || '两次输入的新密码不一致';
              },
            }}
            render={({ field }) => (
              <FormItem dependencies={['newPassword']}>
                <FormLabel>确认新密码</FormLabel>
                <FormControl><Input {...field} type="password" placeholder="请再次输入新密码" autoComplete="new-password" /></FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormActions align="start">
            <Button type="submit" loading={form.formState.isSubmitting}>保存账号信息</Button>
            <Button type="button" variant="outline" onClick={() => form.reset({ ...EMPTY_VALUES, email: loadedEmail })}>
              还原本次修改
            </Button>
          </FormActions>
        </form>
      </Form>
    </SectionCard>
  );

  const sessionsCard = (
    <SectionCard
      title="活跃会话"
      description="列出当前仍能用来登录本账号的设备/浏览器；不认识的设备可以单独退出。"
      extra={<Button variant="destructive-outline" disabled={sessions.length <= 1} onClick={revokeOthers}>退出其它所有设备</Button>}
    >
      <DataTable columns={columns} data={sessions} getRowId={(row) => row.id} loading={sessionsLoading} />
    </SectionCard>
  );

  return (
    <WorkbenchPage eyebrow="SYSTEM / ACCOUNT" title="账号与安全" description="更新登录邮箱或密码后，为保护账号安全会要求重新登录。">
      <WorkbenchTabs
        items={[
          { key: 'credentials', label: '登录凭据', children: credentialsCard },
          { key: 'sessions', label: '活跃会话', children: sessionsCard },
        ]}
      />
    </WorkbenchPage>
  );
};

export default AuthInfo;
