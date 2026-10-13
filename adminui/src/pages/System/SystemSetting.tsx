import React, { useEffect, useState } from "react";
import { Alert, Button, Col, Form, Input, InputNumber, Modal, Row, Select, Space, Switch, Table, Tabs, Tag, notification } from "antd";
import { SectionCard, WorkbenchPage } from "@/componets/Workbench";
import {
  getRuntimeSetting,
  getRuntimeSettingStatus,
  getRuntimeAccessContext,
  getSmtpSetting,
  saveRuntimeSetting,
  saveSmtpSetting,
  testSendSmtpSetting,
  type RuntimeSetting,
  type RuntimeAccessContext,
} from "@/api/systemSetting";
import {
  disableGatewayNode,
  deleteGatewayNode,
  reloadGatewayNode,
  restartGatewayNode,
  searchGatewayNodes,
  type GatewayNode,
} from "@/api/gatewayNode";

const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value)) as T;

const merge = (target: Record<string, any>, source: Record<string, any>) => {
  for (const [key, value] of Object.entries(source)) {
    if (value && typeof value === "object" && !Array.isArray(value)) {
      target[key] = merge(target[key] && typeof target[key] === "object" ? target[key] : {}, value);
    } else {
      target[key] = value;
    }
  }
  return target;
};

const statusColor: Record<GatewayNode["status"], string> = {
  active: "green",
  draining: "orange",
  disabled: "red",
};

const validCIDR = (value: string): boolean => {
  const [address, prefix, extra] = value.trim().split("/");
  if (!address || !prefix || extra !== undefined || !/^\d+$/.test(prefix)) return false;
  const bits = Number(prefix);
  const octets = address.split(".");
  if (octets.length === 4 && octets.every(item => /^\d+$/.test(item) && Number(item) <= 255)) return bits <= 32;
  return address.includes(":") && bits <= 128;
};

const RuntimeSettings: React.FC = () => {
  const [form] = Form.useForm();
  const [runtime, setRuntime] = useState<RuntimeSetting | null>(null);
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState<any>(null);
  const [accessContext, setAccessContext] = useState<RuntimeAccessContext | null>(null);

  const load = async () => {
    const [setting, currentStatus, access] = await Promise.all([getRuntimeSetting(), getRuntimeSettingStatus(), getRuntimeAccessContext()]);
    if (setting.code === 200) {
      setRuntime(setting.data);
      if (setting.data?.config) form.setFieldsValue(setting.data.config);
    }
    if (currentStatus.code === 200) setStatus(currentStatus.data);
    if (access.code === 200) setAccessContext(access.data);
  };

  useEffect(() => { void load(); }, []);

  const save = async () => {
    const values = await form.validateFields();
    const cidrs = (values.management?.allowedCIDRs ?? []).map((cidr: string) => cidr.trim()).filter(Boolean);
    const trusted = (values.listeners?.proxyProtocol?.trustedCIDRs ?? []).map((cidr: string) => cidr.trim()).filter(Boolean);
    values.management = { ...values.management, allowedCIDRs: cidrs };
    values.listeners = { ...values.listeners, proxyProtocol: { ...values.listeners?.proxyProtocol, trustedCIDRs: trusted } };
    if (cidrs.length === 0 || trusted.length === 0 || cidrs.some((cidr: string) => !validCIDR(cidr)) || trusted.some((cidr: string) => !validCIDR(cidr))) {
      notification.error({ message: "请输入至少一个有效的管理 CIDR" });
      return;
    }
    Modal.confirm({
      title: "保存运行配置",
      content: "运行参数需要重启才能生效，保存后会按节点逐个 drain、重启并等待 ready。节点启动失败时会暂停后续节点。",
      okText: "保存并滚动重启",
      cancelText: "取消",
      onOk: async () => {
        setLoading(true);
        try {
          const config = merge(clone(runtime?.config ?? {}), values);
          const response = await saveRuntimeSetting({ config, expectedRevision: runtime?.revision ?? null, restart: true });
          if (response.code === 200) {
            notification.success({ message: "运行配置已保存，滚动重启已排队" });
            await load();
          }
          else if (response.msg) notification.error({ message: response.msg });
        } finally {
          setLoading(false);
        }
      },
    });
  };

  const addCurrentIP = () => {
    if (!accessContext?.clientIP) return;
    const suffix = accessContext.clientIP.includes(":") ? "/128" : "/32";
    const current = form.getFieldValue(["management", "allowedCIDRs"]) ?? [];
    if (!current.includes(accessContext.clientIP + suffix)) {
      form.setFieldValue(["management", "allowedCIDRs"], [...current, accessContext.clientIP + suffix]);
    }
  };

  const saveBar = (
    <Space>
      <Button type="primary" loading={loading} onClick={() => void save()}>保存并滚动重启</Button>
      <Tag color={status?.pendingRestart ? "orange" : "green"}>{status?.pendingRestart ? "存在待应用节点" : "节点配置已同步"}</Tag>
      {runtime ? <Tag>Revision {runtime.revision}</Tag> : null}
    </Space>
  );

  return (
    <SectionCard title="运行参数" description="这些参数在 PostgreSQL 中集中保存，所有已批准节点共享。保存后按节点滚动重启。">
      <Form form={form} layout="vertical">
        <Tabs
          items={[
            {
              key: "proxy",
              label: "代理连接",
              children: (
                <Row gutter={14}>
                  <Col xs={24} md={8}><Form.Item label="活动超时" name={["proxy", "activityTimeout"]}><Input placeholder="5m" /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="响应头超时" name={["proxy", "responseHeaderTimeout"]}><Input placeholder="30s" /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="最大并发请求" name={["proxy", "maxConcurrentRequests"]}><InputNumber min={1} max={65536} style={{ width: "100%" }} /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="最大请求头字节数" name={["proxy", "maxRequestHeaderBytes"]}><InputNumber min={8192} max={1048576} style={{ width: "100%" }} /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="最大请求体字节数" name={["proxy", "maxRequestBodyBytes"]}><InputNumber min={0} style={{ width: "100%" }} /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="每主机最大空闲连接" name={["proxy", "maxIdleConnectionsPerHost"]}><InputNumber min={1} max={4096} style={{ width: "100%" }} /></Form.Item></Col>
                </Row>
              ),
            },
            {
              key: "collection",
              label: "数据采集",
              children: (
                <Row gutter={14}>
                  <Col xs={24} md={8}><Form.Item label="启用流量聚合" name={["trafficAggregation", "enabled"]} valuePropName="checked"><Switch /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="聚合刷新间隔" name={["trafficAggregation", "flushInterval"]}><Input placeholder="15s" /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="流量保留天数" name={["trafficAggregation", "retentionDays"]}><InputNumber min={1} max={3650} style={{ width: "100%" }} /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="启用请求历史" name={["requestHistory", "enabled"]} valuePropName="checked"><Switch /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="记录模式" name={["requestHistory", "recordMode"]}><Select options={[{ value: "ErrorsOnly", label: "仅错误" }, { value: "AllMetadata", label: "全部元数据" }]} /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="历史保留小时" name={["requestHistory", "retentionHours"]}><InputNumber min={1} max={8760} style={{ width: "100%" }} /></Form.Item></Col>
                </Row>
              ),
            },
            {
              key: "observability",
              label: "证书与可观测性",
              children: (
                <Row gutter={14}>
                  <Col xs={24} md={8}><Form.Item label="ACME 账户邮箱" name={["acme", "email"]} rules={[{ type: "email", message: "请输入有效邮箱" }]}><Input placeholder="operator@example.com" /></Form.Item></Col>
                  <Col xs={24} md={16}><Form.Item label="远程 Challenge Endpoint（可选）" name={["acme", "keyValueEndpointURL"]} extra="留空使用本节点管理 endpoint；填写后必须指向同集群其他 Yaginx 节点"><Input placeholder="http://other-yaginx:9090" /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="提前续期" name={["acme", "renewBefore"]}><Input placeholder="168h" /></Form.Item></Col>
                  <Col xs={24} md={16}><Alert type="info" showIcon message="ACME 与本地 Challenge endpoint 始终运行" description="证书申请范围在站点 Host 配置中选择“自动申请 ACME 证书”。Directory URL 默认使用 Let’s Encrypt production。" /></Col>
                  <Col xs={24} md={8}><Form.Item label="日志级别" name={["observability", "logLevel"]}><Select options={["debug", "info", "warn", "error"].map(value => ({ value, label: value }))} /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item label="日志格式" name={["observability", "logFormat"]}><Select options={[{ value: "json", label: "JSON" }, { value: "text", label: "Text" }]} /></Form.Item></Col>
                  <Col xs={24} md={16}>
                    <Form.Item label="管理端允许访问网段（每行一个 CIDR）" name={["management", "allowedCIDRs"]} getValueProps={(value: string[] | undefined) => ({ value: value?.join("\n") ?? "" })} normalize={(value: string) => value.split(/\r?\n/)}>
                      <Input.TextArea rows={3} />
                    </Form.Item>
                    {accessContext ? <Space direction="vertical" size={4} style={{ width: "100%" }}>
                      <Alert type={accessContext.clientAllowed ? "success" : "warning"} showIcon message={`当前请求 IP：${accessContext.clientIP}（${accessContext.clientAllowed ? "当前允许" : "当前不在允许网段"}）`} />
                      <Button size="small" onClick={addCurrentIP}>添加当前 IP（{accessContext.clientIP.includes(":") ? "/128" : "/32"}）</Button>
                      <span style={{ color: "#888" }}>服务端按 TCP 对端地址识别客户端；反向代理场景下这里显示的是代理地址。</span>
                    </Space> : null}
                  </Col>
                  <Col xs={24} md={8}>
                    <Form.Item label="PROXY 信任网段（每行一个 CIDR）" name={["listeners", "proxyProtocol", "trustedCIDRs"]} getValueProps={(value: string[] | undefined) => ({ value: value?.join("\n") ?? "" })} normalize={(value: string) => value.split(/\r?\n/)}>
                      <Input.TextArea rows={5} />
                    </Form.Item>
                  </Col>
                </Row>
              ),
            },
          ]}
          tabBarExtraContent={saveBar}
        />
      </Form>
    </SectionCard>
  );
};

const SmtpSettings: React.FC = () => {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    void getSmtpSetting().then(rsp => {
      if (rsp.code === 200) {
        form.setFieldsValue({
          enabled: rsp.data?.enabled ?? false,
          host: rsp.data?.host ?? "",
          port: rsp.data?.port ?? 25,
          useSsl: rsp.data?.useSsl ?? true,
          userName: rsp.data?.userName ?? "",
          password: rsp.data?.password ?? "",
          fromEmail: rsp.data?.fromEmail ?? "",
          fromName: rsp.data?.fromName ?? "",
          testEmail: rsp.data?.fromEmail ?? "",
        });
      }
    });
  }, []);

  const save = async () => {
    const values = await form.validateFields(["enabled", "host", "port", "useSsl", "userName", "password", "fromEmail", "fromName"]);
    setLoading(true);
    try {
      const rsp = await saveSmtpSetting(values);
      if (rsp.code === 200) notification.success({ message: "通知设置已保存" });
    } finally {
      setLoading(false);
    }
  };

  const test = async () => {
    const values = await form.validateFields(["testEmail"]);
    setTesting(true);
    try {
      const rsp = await testSendSmtpSetting({ email: values.testEmail });
      if (rsp.code === 200) notification.success({ message: "测试邮件发送成功" });
    } finally {
      setTesting(false);
    }
  };

  return (
    <SectionCard title="SMTP 发信服务" description="用于密码恢复和系统通知。">
      <Form form={form} layout="vertical">
        <Form.Item label="启用 SMTP" name="enabled" valuePropName="checked"><Switch /></Form.Item>
        <Row gutter={14}><Col xs={24} md={16}><Form.Item label="SMTP Host" name="host"><Input /></Form.Item></Col><Col xs={24} md={8}><Form.Item label="SMTP Port" name="port"><InputNumber min={1} max={65535} style={{ width: "100%" }} /></Form.Item></Col></Row>
        <Form.Item label="使用 SSL" name="useSsl" valuePropName="checked"><Switch /></Form.Item>
        <Row gutter={14}><Col xs={24} md={12}><Form.Item label="用户名" name="userName"><Input /></Form.Item></Col><Col xs={24} md={12}><Form.Item label="密码" name="password"><Input.Password /></Form.Item></Col><Col xs={24} md={12}><Form.Item label="发件人邮箱" name="fromEmail"><Input /></Form.Item></Col><Col xs={24} md={12}><Form.Item label="发件人名称" name="fromName"><Input /></Form.Item></Col></Row>
        <Space><Button type="primary" loading={loading} onClick={() => void save()}>保存通知设置</Button><Input placeholder="测试收件邮箱" onChange={event => form.setFieldValue("testEmail", event.target.value)} /><Button loading={testing} onClick={() => void test()}>发送测试邮件</Button></Space>
      </Form>
    </SectionCard>
  );
};

const Nodes: React.FC = () => {
  const [rows, setRows] = useState<GatewayNode[]>([]);
  const [loading, setLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const response = await searchGatewayNodes({ pageIndex: 1, pageSize: 200 });
      if (response.code === 200) setRows(response.data.records);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const action = async (promise: Promise<unknown>, message: string) => {
    await promise;
    notification.success({ message });
    await load();
  };

  return (
    <SectionCard title="节点列表" description="节点启动后自动注册并承载业务流量。">
      <Table<GatewayNode>
        rowKey="nodeId"
        size="small"
        loading={loading}
        dataSource={rows}
        scroll={{ x: 1100 }}
        columns={[
          { title: "节点", dataIndex: "displayName", render: (value: string, node) => <Space direction="vertical" size={0}><strong>{value || "未命名节点"}</strong><small>{node.nodeId}</small></Space> },
          { title: "状态", dataIndex: "status", render: (value: GatewayNode["status"]) => <Tag color={statusColor[value]}>{value}</Tag> },
          { title: "编号", dataIndex: "nodeNumber", render: value => value ?? "-" },
          { title: "版本", dataIndex: "softwareVersion" },
          { title: "配置 Revision", dataIndex: "appliedRevision" },
          { title: "重启状态", render: (_, node) => node.restartGeneration === node.observedRestartGeneration ? "正常" : "待重启" },
          { title: "最近心跳", dataIndex: "lastHeartbeatAt", render: value => value ? new Date(value * 1000).toLocaleString() : "-" },
          { title: "操作", fixed: "right", render: (_, node) => <Space wrap>
            {node.status === "active" ? <Button size="small" onClick={() => void action(reloadGatewayNode(node.nodeId), "节点 reload 已请求")}>Reload</Button> : null}
            {node.status === "active" ? <Button size="small" onClick={() => void action(restartGatewayNode(node.nodeId), "节点重启已排队")}>重启</Button> : null}
            {node.status === "active" ? <Button danger size="small" onClick={() => void action(disableGatewayNode(node.nodeId, "operator request"), "节点已禁用")}>禁用</Button> : null}
            {(node.status === "disabled" || node.stale) ? <Button danger size="small" onClick={() => void action(deleteGatewayNode(node.nodeId), "节点已删除")}>删除</Button> : null}
          </Space> },
        ]}
      />
    </SectionCard>
  );
};

export type SystemSettingSection = "runtime" | "nodes" | "notifications";

export const SystemSetting: React.FC<{ section?: SystemSettingSection }> = ({ section }) => {
  const pageMeta = section === "runtime"
    ? { title: "运行配置", description: "管理所有已批准节点共享的运行参数。" }
    : section === "nodes"
      ? { title: "节点与发布", description: "执行节点 reload、重启、禁用和清理操作。" }
      : section === "notifications"
        ? { title: "通知", description: "配置密码恢复和系统通知使用的 SMTP 服务。" }
        : { title: "系统设置", description: "集中管理共享运行参数、通知服务和网关节点。" };
  return <WorkbenchPage eyebrow="SYSTEM / CONTROL PLANE" title={pageMeta.title} description={pageMeta.description}>
    {section === "runtime" ? <RuntimeSettings /> : section === "nodes" ? <Nodes /> : section === "notifications" ? <SmtpSettings /> : <Tabs items={[
      { key: "runtime", label: "运行参数", children: <RuntimeSettings /> },
      { key: "nodes", label: "节点列表", children: <Nodes /> },
      { key: "notifications", label: "通知设置", children: <SmtpSettings /> },
    ]} />}
  </WorkbenchPage>;
};

export default SystemSetting;
