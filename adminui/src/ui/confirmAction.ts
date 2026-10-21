import { confirm } from '@modra/react';
import type { ConfirmOptions } from '@modra/react';

/** 危险操作确认：默认按 destructive 样式展示，异步 onConfirm 期间按钮显示加载态。 */
export const confirmDanger = (options: ConfirmOptions) => confirm({ destructive: true, ...options });
