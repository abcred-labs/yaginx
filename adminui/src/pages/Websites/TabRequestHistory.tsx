import React from 'react';
import { useFormContext, useWatch } from 'react-hook-form';
import {
  FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage, InputNumber,
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@modra/react';

const MODE_OPTIONS = [
  { value: 'inherit', label: '继承系统设置' },
  { value: 'errorsOnly', label: '仅错误' },
  { value: 'all', label: '全量请求' },
];

/**
 * 站点「请求跟踪」策略字段。必须渲染在 Modra `<Form {...form}>`（react-hook-form 上下文）内，
 * 字段路径为 requestHistory.mode / requestHistory.successSamplingRate。
 */
export const RequestHistoryPolicyFields: React.FC = () => {
  const { control } = useFormContext();
  const mode = useWatch({ control, name: 'requestHistory.mode' }) ?? 'inherit';

  return (
    <>
      <p className="text-sm text-muted-foreground">
        继承使用系统默认策略；仅错误保留错误请求；全量保留已完成请求，包含普通 404 和客户端取消的可观测结果。全局关闭时，本站点仍可选择仅错误或全量。
      </p>
      <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
        <FormField
          control={control}
          name="requestHistory.mode"
          defaultValue="inherit"
          render={({ field }) => (
            <FormItem>
              <FormLabel>跟踪模式</FormLabel>
              <Select value={field.value ?? 'inherit'} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                </FormControl>
                <SelectContent>
                  {MODE_OPTIONS.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />
        {mode === 'all' ? (
          <FormField
            control={control}
            name="requestHistory.successSamplingRate"
            rules={{
              validate: (value) => value === null || value === undefined || value === '' || (typeof value === 'number' && value >= 0 && value <= 1) || '请输入 0 到 1 之间的数值',
            }}
            render={({ field }) => (
              <FormItem>
                <FormLabel>成功请求采样率</FormLabel>
                <FormControl>
                  <InputNumber
                    min={0}
                    max={1}
                    step={0.05}
                    placeholder="1"
                    value={field.value ?? null}
                    onValueChange={(value) => field.onChange(value)}
                    onBlur={field.onBlur}
                  />
                </FormControl>
                <FormDescription>留空表示 100%；错误请求始终完整记录。</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        ) : null}
      </div>
      <p className="mt-4 text-sm text-muted-foreground">
        成功请求仅保存元数据和 Header。请求可能因策略过滤、成功采样、全局排除路径、队列丢弃、数据库写入失败或保留期清理而无法查询。站点策略保存后自动同步，历史数据不回填。
      </p>
    </>
  );
};

export const TabRequestHistory = {
  label: '请求跟踪',
  key: 'tabRequestHistory',
  forceRender: true,
  children: <RequestHistoryPolicyFields />,
};
