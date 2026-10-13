import {
  LockOutlined,
  UserOutlined,
} from '@ant-design/icons';
import {
  LoginFormPage,
  ProConfigProvider,
  ProFormCheckbox,
  ProFormText,
} from '@ant-design/pro-components';
import { Button, Form, Input, Modal, Tabs, notification, theme } from 'antd';
import React, { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { login, resetPasswordRecovery, startPasswordRecovery, verifyPasswordRecoveryEmail } from '@/api/account';
import { useAuth } from '@/layouts/RootAuthProvider';
import { isSafeRedirect } from '@/utils/safeRedirect';

const Page = () => {
  const { token } = theme.useToken();

  const [forgotOpen, setForgotOpen] = useState(false);
  const [forgotStep, setForgotStep] = useState(0);
  const [forgotLoading, setForgotLoading] = useState(false);
  const [forgotRecoveryId, setForgotRecoveryId] = useState('');
  const [maskedEmail, setMaskedEmail] = useState('');
  const [forgotForm] = Form.useForm();

  const { login: localLogin }: any = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const onSubmit = async (values: any) => {
    const rsp = await login({ email: values.email, password: values.password, remember: !!values.autoLogin });
    if (rsp === null) {
      // 网络/传输层异常：FetchJson 已经弹过错误通知，这里无需重复处理。
      return;
    }
    if (rsp.code !== 200) {
      // 业务失败（含 401 等）：FetchJson 已经弹过通知，留在登录页让用户重试即可。
      return;
    }
    localLogin(rsp.data);
    const redirect = searchParams.get('redirect');
    navigate(isSafeRedirect(redirect) ? redirect : '/', { replace: true });
  };

  const onSubmitFailed = (errorInfo: any) => {
    console.log('Failed:', errorInfo);
  };

  const resetForgotState = () => {
    setForgotStep(0);
    setForgotRecoveryId('');
    setMaskedEmail('');
    forgotForm.resetFields();
  };

  const openForgotDialog = () => {
    resetForgotState();
    setForgotOpen(true);
  };

  const closeForgotDialog = () => {
    setForgotOpen(false);
    resetForgotState();
  };

  const handleForgotNext = async () => {
    setForgotLoading(true);
    try {
      if (forgotStep === 0) {
        const values = await forgotForm.validateFields(['accountEmail']);
        const rsp = await startPasswordRecovery({ accountEmail: values.accountEmail });
        if (rsp && rsp.code === 200) {
          setForgotRecoveryId(rsp.data.recoveryId);
          setMaskedEmail(rsp.data.maskedEmail);
          setForgotStep(1);
        }
        return;
      }

      if (forgotStep === 1) {
        const values = await forgotForm.validateFields(['email']);
        const rsp = await verifyPasswordRecoveryEmail({
          recoveryId: forgotRecoveryId,
          email: values.email,
        });
        if (rsp && rsp.code === 200) {
          notification.success({ message: '验证码已发送', description: '请前往该账号的邮箱查收。' });
          setForgotStep(2);
        }
        return;
      }

      const values = await forgotForm.validateFields(['code', 'newPassword', 'confirmPassword']);
      const rsp = await resetPasswordRecovery({
        recoveryId: forgotRecoveryId,
        code: values.code,
        newPassword: values.newPassword,
        confirmPassword: values.confirmPassword,
      });

      if (rsp && rsp.code === 200) {
        notification.success({
          message: '密码已重置',
          description: '请使用新密码登录。',
        });
        closeForgotDialog();
      }
    } finally {
      setForgotLoading(false);
    }
  };

  return (
    <div
      className="login-page"
    >
      <LoginFormPage
        title="YAGINX"
        onFinish={onSubmit}
        onFinishFailed={onSubmitFailed}
        subTitle="网关运维控制台"
        submitter={{ searchConfig: { submitText: '登录' } }}
      >
        <ProFormText
          name="email"
          fieldProps={{
            size: 'large',
            prefix: (
              <UserOutlined
                style={{
                  color: token.colorText,
                }}
                className={'prefixIcon'}
              />
            ),
          }}
          placeholder={'邮箱地址'}
          rules={[
            {
              required: true,
              message: '请输入邮箱地址',
            },
            {
              type: 'email',
              message: '请输入有效的邮箱地址',
            },
          ]}
        />
        <ProFormText.Password
          name="password"
          fieldProps={{
            size: 'large',
            prefix: (
              <LockOutlined
                style={{
                  color: token.colorText,
                }}
                className={'prefixIcon'}
              />
            ),
          }}
          placeholder={'密码'}
          rules={[
            {
              required: true,
              message: '请输入密码',
            },
          ]}
        />
        <div
          style={{
            marginBlockEnd: 24,
          }}
        >
          <ProFormCheckbox noStyle name="autoLogin">
            保持登录
          </ProFormCheckbox>
          <a
            onClick={openForgotDialog}
            style={{
              float: 'right',
            }}
          >
            重置密码
          </a>
        </div>
      </LoginFormPage>

      <Modal
        open={forgotOpen}
        title="重置密码"
        onCancel={closeForgotDialog}
        footer={[
          <Button key="cancel" onClick={closeForgotDialog}>取消</Button>,
          forgotStep > 0 ? (
            <Button key="prev" onClick={() => setForgotStep((s) => s - 1)}>上一步</Button>
          ) : null,
          <Button key="next" type="primary" loading={forgotLoading} onClick={handleForgotNext}>
            {forgotStep < 2 ? '继续' : '重置密码'}
          </Button>,
        ]}
        destroyOnClose
      >
        <Tabs
          activeKey={String(forgotStep)}
          items={[
            { key: '0', label: '账号' },
            { key: '1', label: '验证邮箱' },
            { key: '2', label: '设置新密码' },
          ]}
        />
        <Form form={forgotForm} layout="vertical">
          {forgotStep === 0 && (
            <Form.Item
              label="账号邮箱"
              name="accountEmail"
              rules={[{ required: true, message: '请输入账号邮箱' }]}
            >
              <Input placeholder="name@example.com" />
            </Form.Item>
          )}

          {forgotStep === 1 && (
            <>
              <div style={{ marginBottom: 'var(--space-3)' }}>请输入完整邮箱以验证：{maskedEmail}</div>
              <Form.Item
                label="完整邮箱"
                name="email"
                rules={[{ required: true, message: '请输入完整邮箱' }]}
              >
                <Input placeholder="name@example.com" />
              </Form.Item>
            </>
          )}

          {forgotStep === 2 && (
            <>
              <Form.Item
                label="验证码"
                name="code"
                rules={[{ required: true, message: '请输入验证码' }]}
              >
                <Input placeholder="6 位验证码" maxLength={6} />
              </Form.Item>
              <Form.Item
                label="新密码"
                name="newPassword"
                rules={[{ required: true, message: '请输入新密码' }]}
              >
                <Input.Password placeholder="新密码" />
              </Form.Item>
              <Form.Item
                label="确认新密码"
                name="confirmPassword"
                dependencies={["newPassword"]}
                rules={[
                  { required: true, message: '请再次输入新密码' },
                  ({ getFieldValue }) => ({
                    validator(_, value) {
                      if (!value || getFieldValue('newPassword') === value) {
                        return Promise.resolve();
                      }
                      return Promise.reject(new Error('两次输入的密码不一致'));
                    },
                  }),
                ]}
              >
                <Input.Password placeholder="再次输入新密码" />
              </Form.Item>
            </>
          )}
        </Form>
      </Modal>
    </div>
  );
};

export default () => {
  return (
    <ProConfigProvider>
      <Page />
    </ProConfigProvider>
  );
};
