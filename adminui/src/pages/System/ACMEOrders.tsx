import React, {useEffect, useState} from "react";
import {Alert, Button, Descriptions, Drawer, Empty, Input, Pagination, Space, Table, Tag, Timeline, Typography, notification} from "antd";
import {SectionCard, WorkbenchPage} from "@/componets/Workbench";
import {ACMEOrder, ACMEOrderLog, getACMELogs, getACMEOrderLogs, getACMEOrders, cancelACMEOrder, retryACMEOrder} from "@/api/acme";

const labels: Record<string, string> = {pending:"等待处理", locked:"申请中", authorizing:"验证中", finalizing:"签发中", retry_wait:"等待重试", valid:"已完成", failed:"失败", canceled:"已取消"};
const operationLabels: Record<string, string> = {create:"创建订单", lock:"获取锁", lock_conflict:"锁冲突", authorizing:"开始验证", finalizing:"开始签发", retry:"安排重试", cancel:"取消订单", failed:"处理失败", invalid_domain:"域名无效", complete:"完成签发"};
const formatTime = (value?: string | number) => value ? new Date(typeof value === "number" ? value * 1000 : value).toLocaleString() : "—";

const ACMEOrders: React.FC = () => {
  const [rows, setRows] = useState<ACMEOrder[]>([]);
  const [loading, setLoading] = useState(false);
  const [domain, setDomain] = useState(() => new URLSearchParams(window.location.search).get("domain") ?? "");
  const [systemLogs, setSystemLogs] = useState<Array<Record<string, any>>>([]);
  const [systemLogsLoading, setSystemLogsLoading] = useState(false);
  const [selected, setSelected] = useState<ACMEOrder | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [orderLogs, setOrderLogs] = useState<ACMEOrderLog[]>([]);
  const [orderLogsTotal, setOrderLogsTotal] = useState(0);
  const [orderLogsPage, setOrderLogsPage] = useState(1);
  const [orderLogsLoading, setOrderLogsLoading] = useState(false);
  const [orderLogsError, setOrderLogsError] = useState<string | null>(null);

  const load = async () => { setLoading(true); try { const r = await getACMEOrders({pageSize:200, domain:domain.trim() || undefined}); if (r.code === 200) setRows(r.data.records); } finally { setLoading(false); } };
  const loadSystemLogs = async () => { setSystemLogsLoading(true); try { const r = await getACMELogs({pageIndex:1,pageSize:100}); if (r.code === 200) setSystemLogs(r.data.records || []); } finally { setSystemLogsLoading(false); } };
  const loadOrderLogs = async (order: ACMEOrder, page = 1) => { setOrderLogsLoading(true); setOrderLogsError(null); try { const r = await getACMEOrderLogs(order.id, {pageIndex:page,pageSize:10}); if (r.code !== 200) throw new Error("日志加载失败"); setOrderLogs(r.data.records || []); setOrderLogsTotal(r.data.paging?.total || 0); setOrderLogsPage(page); } catch (error) { setOrderLogsError(error instanceof Error ? error.message : "日志加载失败"); } finally { setOrderLogsLoading(false); } };
  useEffect(() => { void load(); }, [domain]);
  useEffect(() => { void loadSystemLogs(); }, []);

  const openLogs = (order: ACMEOrder) => { setSelected(order); setDrawerOpen(true); void loadOrderLogs(order); };
  const action = async (fn: () => Promise<any>, msg: string) => { await fn(); notification.success({message:msg}); await load(); await loadSystemLogs(); if (selected) await loadOrderLogs(selected, orderLogsPage); };

  return <WorkbenchPage eyebrow="SYSTEM / ACME" title="ACME 订单" description="查看证书申请状态、错误和处理记录。">
    <SectionCard title="订单列表" description="订单状态来自共享 PostgreSQL。">
      <Space style={{marginBottom:16}}><Input placeholder="按域名筛选" value={domain} onChange={e=>setDomain(e.target.value)} onPressEnter={()=>void load()} /><Button onClick={()=>void load()}>刷新</Button></Space>
      <Table rowKey="id" loading={loading} dataSource={rows} scroll={{x:1000}} columns={[
        {title:"域名",dataIndex:"domain"},
        {title:"状态",dataIndex:"status",render:(v:string)=><Tag color={v === "failed" ? "red" : v === "valid" ? "green" : "gold"}>{labels[v] ?? v}</Tag>},
        {title:"尝试次数",dataIndex:"attempt"}, {title:"错误",dataIndex:"errorMessage"}, {title:"更新时间",dataIndex:"updatedAt",render:(v:string)=>formatTime(v)},
        {title:"操作",fixed:"right" as const,render:(_:unknown,r:ACMEOrder)=><Space>
          <Button size="small" onClick={()=>openLogs(r)}>日志</Button>
          {["failed","canceled","retry_wait"].includes(r.status) ? <Button size="small" onClick={()=>void action(()=>retryACMEOrder(r.id),"已安排重试")}>重试</Button> : null}
          {["pending","locked","authorizing","finalizing","retry_wait"].includes(r.status) ? <Button danger size="small" onClick={()=>void action(()=>cancelACMEOrder(r.id),"已取消订单")}>取消</Button> : null}
        </Space>}
      ]} />
    </SectionCard>
    <SectionCard title="ACME 系统错误" description="这里记录全局 ACME 组件的系统级故障；单条订单的完整处理流水请点击订单行的“日志”。">
      <Table rowKey="id" loading={systemLogsLoading} dataSource={systemLogs} locale={{emptyText:<Empty description="暂无 ACME 系统级错误" />}} pagination={{pageSize:10}} columns={[
        {title:"时间",dataIndex:"lastSeenAt",render:(v:number)=>formatTime(v)}, {title:"操作",dataIndex:"operation",render:(v:string)=>operationLabels[v] ?? v}, {title:"错误码",dataIndex:"errorCode"}, {title:"消息",dataIndex:"message"}, {title:"次数",dataIndex:"occurrenceCount"}
      ]} />
    </SectionCard>
    <Drawer title={selected ? `订单日志 · ${selected.domain}` : "订单日志"} width={620} open={drawerOpen} onClose={()=>setDrawerOpen(false)}>
      {selected ? <>
        <Descriptions size="small" column={2} bordered style={{marginBottom:20}} items={[{key:"status",label:"当前状态",children:<Tag color={selected.status === "failed" ? "red" : selected.status === "valid" ? "green" : "gold"}>{labels[selected.status] ?? selected.status}</Tag>},{key:"attempt",label:"尝试次数",children:selected.attempt},{key:"updated",label:"更新时间",children:formatTime(selected.updatedAt)}]} />
        {orderLogsError ? <Alert type="error" showIcon message={orderLogsError} action={<Button size="small" onClick={()=>void loadOrderLogs(selected, orderLogsPage)}>重试</Button>} style={{marginBottom:16}} /> : null}
        {!orderLogsError && !orderLogsLoading && orderLogs.length === 0 ? <Empty description="该订单暂无处理日志" /> : <Timeline pending={orderLogsLoading ? "加载中…" : undefined} items={orderLogs.map(log => ({key:log.id,color:log.result === "failure" ? "red" : log.result === "success" ? "green" : "blue", children:<div><Space wrap><Typography.Text strong>{operationLabels[log.operation] ?? log.operation}</Typography.Text><Tag>{labels[log.status] ?? log.status}</Tag>{log.result ? <Tag color={log.result === "failure" ? "red" : "blue"}>{log.result}</Tag> : null}<Typography.Text type="secondary">第 {log.attempt} 次</Typography.Text></Space><Typography.Paragraph style={{margin:"6px 0 0",whiteSpace:"pre-wrap",wordBreak:"break-word"}}>{log.message || "—"}{log.errorCode ? <Typography.Text type="danger"> · {log.errorCode}</Typography.Text> : null}</Typography.Paragraph><Typography.Text type="secondary">{formatTime(log.occurredAt)}{log.nodeId ? ` · 节点 ${log.nodeId}` : ""}</Typography.Text></div>}))} />}
        {orderLogsTotal > 10 ? <Pagination size="small" current={orderLogsPage} pageSize={10} total={orderLogsTotal} onChange={page=>void loadOrderLogs(selected, page)} style={{marginTop:20,textAlign:"right"}} /> : null}
      </> : null}
    </Drawer>
  </WorkbenchPage>;
};

export default ACMEOrders;
