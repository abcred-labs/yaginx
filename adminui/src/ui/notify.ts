import { toast } from '@modra/react';

const DURATION_MS = 5000;

/** 组件树外也可调用（请求层使用），内部依赖根部挂载的 <Toaster />。 */
export const notifyError = (title: string, description?: string) =>
  toast({ variant: 'destructive', title, description, duration: DURATION_MS });

export const notifySuccess = (title: string, description?: string) =>
  toast({ variant: 'success', title, description, duration: DURATION_MS });

export const notifyWarning = (title: string, description?: string) =>
  toast({ variant: 'warning', title, description, duration: DURATION_MS });
