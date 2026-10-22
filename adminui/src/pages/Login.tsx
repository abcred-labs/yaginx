import {
  AuthLayout, Button, Checkbox, Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
  Form, FormControl, FormField, FormItem, FormLabel, FormMessage, Input, Steps, StepsItem,
} from '@modra/react';
import { Lock, User } from '@/ui/icons';
import React, { useState } from 'react';
import { useForm } from 'react-hook-form';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { login, resetPasswordRecovery, startPasswordRecovery, verifyPasswordRecoveryEmail } from '@/api/account';
import { useAuth } from '@/layouts/RootAuthProvider';
import { notifySuccess } from '@/ui/notify';
import { isSafeRedirect } from '@/utils/safeRedirect';

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface LoginValues {
  email: string;
  password: string;
  autoLogin: boolean;
}

interface RecoveryValues {
  accountEmail: string;
  email: string;
  code: string;
  newPassword: string;
  confirmPassword: string;
}

const RECOVERY_STEPS = ['账号', '验证邮箱', '设置新密码'];

const IconInput = React.forwardRef<HTMLInputElement, React.ComponentProps<typeof Input> & { icon: React.ReactNode }>(
  ({ icon, className, ...props }, ref) => (
    <div className="relative">
      <span className="pointer-events-none absolute inset-y-0 start-3 flex items-center text-muted-foreground">{icon}</span>
      <Input ref={ref} size="lg" className={`ps-9 ${className ?? ''}`} {...props} />
    </div>
  ),
);
IconInput.displayName = 'IconInput';

const PasswordRecoveryDialog: React.FC<{ open: boolean; onClose: () => void }> = ({ open, onClose }) => {
  const [step, setStep] = useState(0);
  const [loading, setLoading] = useState(false);
  const [recoveryId, setRecoveryId] = useState('');
  const [maskedEmail, setMaskedEmail] = useState('');
  const form = useForm<RecoveryValues>({
    defaultValues: { accountEmail: '', email: '', code: '', newPassword: '', confirmPassword: '' },
  });

  const close = () => {
    setStep(0);
    setRecoveryId('');
    setMaskedEmail('');
    form.reset();
    onClose();
  };

  const next = async () => {
    const fields: (keyof RecoveryValues)[][] = [['accountEmail'], ['email'], ['code', 'newPassword', 'confirmPassword']];
    if (!(await form.trigger(fields[step]))) return;
    const values = form.getValues();
    setLoading(true);
    try {
      if (step === 0) {
        const rsp = await startPasswordRecovery({ accountEmail: values.accountEmail });
        if (rsp && rsp.code === 200) {
          setRecoveryId(rsp.data.recoveryId);
          setMaskedEmail(rsp.data.maskedEmail);
          setStep(1);
        }
      } else if (step === 1) {
        const rsp = await verifyPasswordRecoveryEmail({ recoveryId, email: values.email });
        if (rsp && rsp.code === 200) {
          notifySuccess('验证码已发送', '请前往该账号的邮箱查收。');
          setStep(2);
        }
      } else {
        const rsp = await resetPasswordRecovery({
          recoveryId,
          code: values.code,
          newPassword: values.newPassword,
          confirmPassword: values.confirmPassword,
        });
        if (rsp && rsp.code === 200) {
          notifySuccess('密码已重置', '请使用新密码登录。');
          close();
        }
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) close(); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>重置密码</DialogTitle>
        </DialogHeader>
        <Steps>
          {RECOVERY_STEPS.map((title, index) => (
            <StepsItem key={title} title={title} status={index < step ? 'complete' : index === step ? 'current' : 'pending'} />
          ))}
        </Steps>
        <Form {...form} layout="vertical">
          <form className="flex flex-col gap-4" onSubmit={(event) => { event.preventDefault(); void next(); }}>
            {step === 0 && (
              <FormField
                control={form.control}
                name="accountEmail"
                rules={{ required: '请输入账号邮箱' }}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>账号邮箱</FormLabel>
                    <FormControl><Input {...field} placeholder="name@example.com" /></FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            {step === 1 && (
              <>
                <p className="text-sm text-muted-foreground">请输入完整邮箱以验证：{maskedEmail}</p>
                <FormField
                  control={form.control}
                  name="email"
                  rules={{ required: '请输入完整邮箱' }}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>完整邮箱</FormLabel>
                      <FormControl><Input {...field} placeholder="name@example.com" /></FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </>
            )}
            {step === 2 && (
              <>
                <FormField
                  control={form.control}
                  name="code"
                  rules={{ required: '请输入验证码' }}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>验证码</FormLabel>
                      <FormControl><Input {...field} placeholder="6 位验证码" maxLength={6} /></FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="newPassword"
                  rules={{ required: '请输入新密码' }}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>新密码</FormLabel>
                      <FormControl><Input {...field} type="password" placeholder="新密码" autoComplete="new-password" /></FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="confirmPassword"
                  rules={{
                    required: '请再次输入新密码',
                    validate: (value, values) => value === values.newPassword || '两次输入的密码不一致',
                  }}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>确认新密码</FormLabel>
                      <FormControl><Input {...field} type="password" placeholder="再次输入新密码" autoComplete="new-password" /></FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </>
            )}
            <button type="submit" className="hidden" aria-hidden="true" tabIndex={-1} />
          </form>
        </Form>
        <DialogFooter>
          <Button variant="outline" onClick={close}>取消</Button>
          {step > 0 && <Button variant="outline" onClick={() => setStep((s) => s - 1)}>上一步</Button>}
          <Button loading={loading} onClick={() => void next()}>{step < 2 ? '继续' : '重置密码'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

const Login: React.FC = () => {
  const [forgotOpen, setForgotOpen] = useState(false);
  const { login: localLogin }: any = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const form = useForm<LoginValues>({ defaultValues: { email: '', password: '', autoLogin: false } });

  const onSubmit = async (values: LoginValues) => {
    const rsp = await login({ email: values.email, password: values.password, remember: values.autoLogin });
    // 传输层异常或业务失败（含 401）时 FetchJson 已经弹过通知，留在登录页让用户重试即可。
    if (rsp === null || rsp.code !== 200) return;
    localLogin(rsp.data);
    const redirect = searchParams.get('redirect');
    navigate(isSafeRedirect(redirect) ? redirect : '/', { replace: true });
  };

  return (
    <>
      <AuthLayout title="YAGINX" description="网关运维控制台">
        <Form {...form} layout="vertical">
          <form className="flex flex-col gap-4" onSubmit={form.handleSubmit(onSubmit)} noValidate>
            <FormField
              control={form.control}
              name="email"
              rules={{ required: '请输入邮箱地址', pattern: { value: EMAIL_PATTERN, message: '请输入有效的邮箱地址' } }}
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <IconInput {...field} icon={<User className="size-4" />} type="email" placeholder="邮箱地址" aria-label="邮箱地址" autoComplete="username" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="password"
              rules={{ required: '请输入密码' }}
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <IconInput {...field} icon={<Lock className="size-4" />} type="password" placeholder="密码" aria-label="密码" autoComplete="current-password" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <div className="flex items-center justify-between text-sm">
              <FormField
                control={form.control}
                name="autoLogin"
                render={({ field }) => (
                  <label className="flex cursor-pointer items-center gap-2">
                    <Checkbox checked={field.value} onCheckedChange={(checked) => field.onChange(checked === true)} />
                    保持登录
                  </label>
                )}
              />
              <button type="button" className="text-primary hover:underline" onClick={() => setForgotOpen(true)}>
                重置密码
              </button>
            </div>
            <Button type="submit" size="lg" loading={form.formState.isSubmitting}>登录</Button>
          </form>
        </Form>
      </AuthLayout>
      <PasswordRecoveryDialog open={forgotOpen} onClose={() => setForgotOpen(false)} />
    </>
  );
};

export default Login;
