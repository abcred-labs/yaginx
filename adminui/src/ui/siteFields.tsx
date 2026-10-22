import {
  FormControl, FormField, FormItem, FormLabel, FormMessage,
  Input, InputNumber, RadioGroup, RadioGroupItem,
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
  Switch, Textarea,
} from '@modra/react';
import React, { createContext, useContext } from 'react';
import type { Control, ControllerProps, ControllerRenderProps } from 'react-hook-form';

/**
 * 站点工作区使用的字段封装：把 FormField + FormItem + FormLabel + FormControl + FormMessage
 * 压缩成一行，字段名使用点路径字符串（动态列表里是 `hostBindings.0.hostname`）。
 * control 通过 context 注入，避免每个字段重复传参。
 */

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyControl = Control<any>;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type FieldRules = ControllerProps<any, any>['rules'];

const ControlContext = createContext<AnyControl | null>(null);
/** control 的字段类型由调用方的表单决定，这里按字符串路径使用，所以入参放宽为 unknown。 */
export const SiteFieldsProvider: React.FC<{ control: unknown; children: React.ReactNode }> = ({ control, children }) => (
  <ControlContext.Provider value={control as AnyControl}>{children}</ControlContext.Provider>
);

const useControl = (): AnyControl => {
  const control = useContext(ControlContext);
  if (!control) throw new Error('Site fields must be rendered inside <SiteFieldsProvider>.');
  return control;
};

export interface SiteFieldBaseProps {
  name: string;
  label?: React.ReactNode;
  extra?: React.ReactNode;
  rules?: FieldRules;
  /** 对应 antd 的 initialValue：表单值为 undefined 时在字段挂载时写入。 */
  defaultValue?: unknown;
  className?: string;
  required?: boolean;
}

interface ShellProps extends SiteFieldBaseProps {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  children: (field: ControllerRenderProps<any, any>) => React.ReactNode;
}

const FieldShell: React.FC<ShellProps> = ({ name, label, extra, rules, defaultValue, className, required, children }) => {
  const control = useControl();
  return (
    <FormField
      control={control}
      name={name}
      rules={rules}
      defaultValue={defaultValue}
      render={({ field }) => (
        <FormItem className={className} required={required} extra={extra} labelText={typeof label === 'string' ? label : undefined}>
          {label ? <FormLabel>{label}</FormLabel> : null}
          {children(field)}
          <FormMessage />
        </FormItem>
      )}
    />
  );
};

export const TextField: React.FC<SiteFieldBaseProps & { placeholder?: string; list?: string }> = ({ placeholder, list, ...base }) => (
  <FieldShell {...base}>
    {(field) => (
      <FormControl>
        <Input
          name={field.name}
          ref={field.ref}
          value={field.value ?? ''}
          onChange={field.onChange}
          onBlur={field.onBlur}
          placeholder={placeholder}
          list={list}
          autoComplete="off"
        />
      </FormControl>
    )}
  </FieldShell>
);

export const TextAreaField: React.FC<SiteFieldBaseProps & { placeholder?: string; rows?: number }> = ({ placeholder, rows = 4, ...base }) => (
  <FieldShell {...base}>
    {(field) => (
      <FormControl>
        <Textarea
          name={field.name}
          ref={field.ref}
          value={field.value ?? ''}
          onChange={field.onChange}
          onBlur={field.onBlur}
          placeholder={placeholder}
          rows={rows}
        />
      </FormControl>
    )}
  </FieldShell>
);

/** 每行一个条目的列表文本框：表单里存 string[]，与旧 antd 版本的 getValueProps/getValueFromEvent 一致。 */
export const LinesField: React.FC<SiteFieldBaseProps & { placeholder?: string; rows?: number }> = ({ placeholder, rows = 8, ...base }) => (
  <FieldShell {...base}>
    {(field) => (
      <FormControl>
        <Textarea
          name={field.name}
          ref={field.ref}
          value={Array.isArray(field.value) ? field.value.join('\n') : ''}
          onChange={(event) => field.onChange(event.target.value.split(/\r?\n/))}
          onBlur={field.onBlur}
          placeholder={placeholder}
          rows={rows}
          className="font-mono"
        />
      </FormControl>
    )}
  </FieldShell>
);

export const NumberField: React.FC<SiteFieldBaseProps & { min?: number; max?: number; step?: number; placeholder?: string }> = ({ min, max, step, placeholder, ...base }) => (
  <FieldShell {...base}>
    {(field) => (
      <FormControl>
        <InputNumber
          name={field.name}
          ref={field.ref}
          value={typeof field.value === 'number' ? field.value : null}
          onValueChange={field.onChange}
          onBlur={field.onBlur}
          min={min}
          max={max}
          step={step}
          placeholder={placeholder}
        />
      </FormControl>
    )}
  </FieldShell>
);

export interface SwitchFieldProps extends SiteFieldBaseProps {
  onText?: string;
  offText?: string;
  /** 表单值与开关布尔值的映射（例如 certificateMode 用 'request' / 'manual'）。 */
  valueMap?: { on: unknown; off: unknown };
}

export const SwitchField: React.FC<SwitchFieldProps> = ({ onText, offText, valueMap, ...base }) => (
  <FieldShell {...base}>
    {(field) => {
      const checked = valueMap ? field.value === valueMap.on : Boolean(field.value);
      return (
        <div className="flex h-9 items-center gap-2">
          <FormControl>
            <Switch
              ref={field.ref}
              checked={checked}
              onCheckedChange={(next) => field.onChange(valueMap ? (next ? valueMap.on : valueMap.off) : next)}
              onBlur={field.onBlur}
            />
          </FormControl>
          {onText || offText ? <span className="text-sm text-muted-foreground">{checked ? onText : offText}</span> : null}
        </div>
      );
    }}
  </FieldShell>
);

export interface ChoiceOption<V extends string | number | boolean = string> {
  value: V;
  label: string;
}

/** 单选按钮组，选项值可以是数字（如跳转状态码 308 / 301）。 */
export const RadioField = <V extends string | number>({ options, ...base }: SiteFieldBaseProps & { options: ChoiceOption<V>[] }) => (
  <FieldShell {...base}>
    {(field) => (
      <FormControl>
        <RadioGroup
          orientation="horizontal"
          className="min-h-9 gap-x-4 gap-y-2"
          value={field.value === undefined || field.value === null ? '' : String(field.value)}
          onValueChange={(next) => field.onChange(options.find((option) => String(option.value) === next)?.value)}
        >
          {options.map((option) => (
            <label key={String(option.value)} className="flex cursor-pointer items-center gap-2 text-sm">
              <RadioGroupItem value={String(option.value)} />
              {option.label}
            </label>
          ))}
        </RadioGroup>
      </FormControl>
    )}
  </FieldShell>
);

const NONE_VALUE = '__none__';

export const SelectField: React.FC<SiteFieldBaseProps & {
  options: ChoiceOption[];
  placeholder?: string;
  /** 提供后在列表首位增加一个“清空”选项，对应 antd 的 allowClear。 */
  clearLabel?: string;
}> = ({ options, placeholder, clearLabel, ...base }) => (
  <FieldShell {...base}>
    {(field) => (
      <Select
        value={field.value === undefined || field.value === null || field.value === '' ? undefined : String(field.value)}
        onValueChange={(next) => field.onChange(next === NONE_VALUE ? undefined : next)}
      >
        <FormControl>
          <SelectTrigger ref={field.ref} onBlur={field.onBlur}>
            <SelectValue placeholder={placeholder} />
          </SelectTrigger>
        </FormControl>
        <SelectContent>
          {clearLabel ? <SelectItem value={NONE_VALUE}>{clearLabel}</SelectItem> : null}
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
          ))}
        </SelectContent>
      </Select>
    )}
  </FieldShell>
);
