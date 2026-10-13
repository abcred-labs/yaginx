import React, { useEffect, useState } from 'react';
import { Button, Dropdown, Modal } from 'antd';
import { useNavigate } from 'react-router-dom';

export interface TableOperationMenuItem {
  commandKey: string;
  commandLabel: string;
}
export interface TableOperationProps {
  record: any;
  menuOptions: TableOperationMenuItem[];
  onMenuClick: (commandKey: string, record: any) => void | Promise<void>;
  onComplete: (record: any) => void;
}
export const TableOperations: React.FC<TableOperationProps> = (props: TableOperationProps) => {
  const { record, menuOptions, onMenuClick, onComplete } = props;
  const [items, setItems] = useState<Array<any>>([]);
  const [mainAction, setMainAction] = useState<any>({});
  const onMenuClickInternal = (e: any) => {
    const execute = async () => {
      if (onMenuClick) await onMenuClick(e.key, record);
      if (onComplete) onComplete(record);
    };
    if (e.key === 'delete') {
      Modal.confirm({
        title: '确认删除此资源？',
        content: '删除后将无法恢复。',
        okText: '删除',
        cancelText: '取消',
        okButtonProps: { danger: true },
        onOk: execute,
      });
      return;
    }
    execute();
  };

  useEffect(() => {
    let tempItems: any[] = [];
    menuOptions.forEach(item => {
      tempItems.push({ key: item.commandKey, label: item.commandLabel });
    });
    setMainAction({ ...tempItems[0] });
    setItems(tempItems.splice(1, tempItems.length - 1));
  }, [record])
  const invokeMain = () => onMenuClickInternal({ key: mainAction.key });
  const mainButton = <Button onClick={invokeMain}>{mainAction.label}</Button>;
  if (items.length > 0) return <Dropdown.Button menu={{ items, onClick: onMenuClickInternal }} onClick={invokeMain}>{mainAction.label}</Dropdown.Button>;
  return mainButton;
}
