import { Select, SelectClearButton, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@modra/react';
import React from 'react';

export interface ClearableSelectOption {
  value: string;
  label: string;
}

/** 可清空的单选下拉：空值显示 placeholder，选中后右侧出现清除按钮（对应旧 antd Select allowClear）。 */
export const ClearableSelect: React.FC<{
  value?: string;
  onChange: (value: string | undefined) => void;
  options: ClearableSelectOption[];
  placeholder: string;
  className?: string;
  'aria-label'?: string;
}> = ({ value, onChange, options, placeholder, className, ...rest }) => (
  <Select value={value ?? ''} onValueChange={(next) => onChange(next || undefined)}>
    <SelectTrigger className={className} aria-label={rest['aria-label'] ?? placeholder}>
      <SelectValue placeholder={placeholder} />
      {value ? <SelectClearButton aria-label="清除" onClear={() => onChange(undefined)} /> : null}
    </SelectTrigger>
    <SelectContent>
      {options.map((option) => (
        <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
      ))}
    </SelectContent>
  </Select>
);
