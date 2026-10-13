import { TableOperationMenuItem } from "@/componets/TableOperationMenus"
import { WorkbenchPage, SectionCard } from "@/componets/Workbench"
import { Space, Button, UploadProps, message, Upload, Form, Input, Modal, Select, Tag, Typography } from "antd"
import React, { useState } from "react"
import { useNavigate } from "react-router-dom"
import ServerPagedTable from "@/componets/ServerPagedTable"
import { DownloadOutlined, UploadOutlined } from "@ant-design/icons"
import { requireInt64String } from "@/api/identifier"
import { websiteBatchDelete, websiteDelete, websiteSearch, type IWebsiteSearchRequest } from "@/api/website"
import { buildWebsiteSearchRequest, websiteBatchDeleteResultText, websiteListOperationPath } from "./websiteListState"

const List: React.FC = (props) => {
  const pkFieldName = "id";

  const navigate = useNavigate();
  const [refreshTable, forceRefreshTable] = useState<number>(0);
  const [filterForm] = Form.useForm<IWebsiteSearchRequest>();
  const [appliedFilters, setAppliedFilters] = useState<IWebsiteSearchRequest>({});
  const [filterResetKey, setFilterResetKey] = useState(0);
  const [selectedRowKeys, setSelectedRowKeys] = useState<string[]>([]);

  const renderTableOperationMenu: (record: any) => TableOperationMenuItem[] = (record: any) => {
    const tempItems: TableOperationMenuItem[] = [];
    tempItems.push({ commandKey: "analytics", commandLabel: "查看分析" });
    tempItems.push({ commandKey: "edit", commandLabel: "配置" });
    tempItems.push({ commandKey: "childRules", commandLabel: "查看规则" });
    tempItems.push({ commandKey: "delete", commandLabel: "删除" });
    return tempItems;
  }
  const onTableMenuClick = async (commandKey: string, record: any) => {
    const pkValue = requireInt64String(record[pkFieldName], "网站 ID");
    switch (commandKey) {
      case "analytics":
        navigate(websiteListOperationPath("analytics", pkValue));
        break;
      case "edit":
        navigate(websiteListOperationPath("edit", pkValue));
        break;
      case "delete":
        await websiteDelete({ id: pkValue });
        message.success("站点已删除");
        break;
      case "childRules":
        navigate(websiteListOperationPath("childRules", pkValue));
        break;
      default:
        break;
    }
  }

  const applyFilters = (values: IWebsiteSearchRequest) => {
    setAppliedFilters(buildWebsiteSearchRequest(values));
    setSelectedRowKeys([]);
    setFilterResetKey((value) => value + 1);
  };

  const resetFilters = () => {
    filterForm.resetFields();
    setAppliedFilters({});
    setSelectedRowKeys([]);
    setFilterResetKey((value) => value + 1);
  };

  const deleteSelected = () => {
    const ids = selectedRowKeys;
    Modal.confirm({
      title: `确认删除已选择的 ${ids.length} 个站点？`,
      content: "删除后将无法恢复。",
      okText: "批量删除",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: async () => {
        const response = await websiteBatchDelete({ ids });
        message.success(websiteBatchDeleteResultText(response.data));
        setSelectedRowKeys([]);
        forceRefreshTable(Math.random() + 1);
      },
    });
  };

  const initTableColumn = (columnsArray: any[]) => {
    columnsArray.push({ title: '站点名称', key: 'name', width: 180, dataIndex: "name" });
    columnsArray.push({ title: '主站 Host', key: 'primaryHost', width: 180, render: (_value: unknown, record: any) => record.hostBindings?.find((host: any) => host.role === 'primary')?.hostname ?? record.defaultHost ?? '未配置' });
    columnsArray.push({ title: '转发规则', key: 'proxyRules', width: 104, render: (_value: unknown, record: any) => `${record.proxyRules?.length ?? 0} 条` });
    columnsArray.push({ title: '访问状态', key: 'isMaintenanceMode', width: 104, dataIndex: "isMaintenanceMode", render: (value: boolean) => value ? <Tag color="orange">维护中</Tag> : <Tag color="green">正常</Tag> })
    // columnsArray.push({
    //   title: 'domain', key: 'domain', width: 150,
    //   render: (text: any, record: any) => record.hosts.map((element: any) => <>{element?.domain}</>)
    // })
    // columnsArray.push({
    //   title: 'rule', key: 'rule', width: 150,
    //   render: (text: any, record: any) => record.proxyRules.map((element: any) => <>{element?.pathPattern}</>)
    // })
  }
  const downloadConfig = async () => {
    const newTab = window.open('/api/website/config/backup', '_blank');
    newTab?.focus();
    message.success('已打开站点配置备份文件');
  };
  const uploadProps: UploadProps = {
    name: 'file',
    action: '/api/website/config/restore',
    // headers: {
    //   authorization: 'authorization-text',
    // },
    showUploadList: false,
    onChange(info) {
      if (info.file.status !== 'uploading') {
        console.log(info.file, info.fileList);
      }
      if (info.file.status === 'done') {
        forceRefreshTable(Math.random() + 1)
        message.success(`${info.file.name} 已恢复`);
      } else if (info.file.status === 'error') {
        message.error(`${info.file.name} 恢复失败`);
      }
    },
  };
  return (
    <WorkbenchPage eyebrow="CONFIGURATION / WEBSITES" title="网站" description="管理站点入口、默认上游和 TLS 覆盖范围。" extra={<Space wrap><Button onClick={() => forceRefreshTable(Math.random() + 1)}>刷新</Button><Button type="primary" onClick={() => navigate("../create")}>创建站点</Button></Space>}>
      <SectionCard title="站点资源" description="选择站点进入工作区，集中维护主机与转发规则。" extra={<Space><Button icon={<DownloadOutlined />} onClick={downloadConfig}>备份</Button><Upload {...uploadProps}><Button icon={<UploadOutlined />}>恢复</Button></Upload></Space>}>
        <Form form={filterForm} layout="inline" onFinish={applyFilters}>
          <Form.Item label="域名" name="domain"><Input allowClear placeholder="默认或附加域名" /></Form.Item>
          <Form.Item label="上游地址" name="upstream"><Input allowClear placeholder="默认上游地址" /></Form.Item>
          <Form.Item label="证书" name="hasCertificate"><Select allowClear placeholder="全部" style={{ width: 112 }} options={[{ value: true, label: "有证书" }, { value: false, label: "无证书" }]} /></Form.Item>
          <Form.Item><Space><Button type="primary" htmlType="submit">查询</Button><Button onClick={resetFilters}>重置</Button></Space></Form.Item>
        </Form>
        <Space style={{ margin: "var(--space-4) 0" }}>
          <Typography.Text type="secondary">已选择 {selectedRowKeys.length} 个站点</Typography.Text>
          <Button danger disabled={selectedRowKeys.length === 0} onClick={deleteSelected}>批量删除</Button>
        </Space>
        <ServerPagedTable pkFieldName={pkFieldName} size={"small"} searchAction={websiteSearch} realodTableData={refreshTable} initTableColumn={initTableColumn} renderTableOperationMenu={renderTableOperationMenu} onTableMenuClick={onTableMenuClick} externalFilterCondition={appliedFilters} filterResetKey={filterResetKey} selectedRowKeys={selectedRowKeys} onSelectedRowKeysChange={setSelectedRowKeys} />
      </SectionCard>
    </WorkbenchPage>
  )
}
export default List;
