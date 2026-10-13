import { getDomainAnalytics, type IAnalyticsSubjectView } from "@/api/analytics";
import { defaultResourceViewWindowSeconds } from "./resourceViewDefaults";
import { LocalTime } from "@/componets/LocalTime";
import { SectionCard, SignalTag, WorkbenchPage } from "@/componets/Workbench";
import { Alert, Button, DatePicker, Descriptions, Segmented, Space, Spin } from "antd";
import { Dayjs } from "dayjs";
import React, { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { RequestHistoryList } from "@/pages/Monitors/RequestHistory/List";
import { MetricCards, ResourceTrend } from "./ResourceMetrics";
import { websiteViewPath } from "./websiteViewRoute";

const presets = [{ label: "1 小时", value: 3600 }, { label: "24 小时", value: 86400 }, { label: "7 天", value: 604800 }];
const DomainDetail: React.FC = () => {
  const { host = "" } = useParams(); const navigate = useNavigate(); const decodedHost = decodeURIComponent(host);
  const [range, setRange] = useState(defaultResourceViewWindowSeconds); const [customRange, setCustomRange] = useState<[Dayjs | null, Dayjs | null] | null>(null); const [data, setData] = useState<IAnalyticsSubjectView | null>(); const [loading, setLoading] = useState(true); const [error, setError] = useState("");
  const window = useMemo(() => { const to = Math.floor(Date.now() / 1000); if (customRange?.[0] && customRange?.[1]) return { from: Math.floor(customRange[0].valueOf() / 1000), to: Math.floor(customRange[1].valueOf() / 1000) }; return { from: to - range, to }; }, [range, customRange]);
  const resolvedHost = decodedHost;
  const load = async () => { if (!resolvedHost) return; setLoading(true); setError(""); try { const response = await getDomainAnalytics({ host: resolvedHost, ...window }); setData(response.data ?? null); } catch (reason) { setError(reason instanceof Error ? reason.message : "无法加载该 Host 的资源视图，请稍后重试。"); } finally { setLoading(false); } };
  useEffect(() => { if (resolvedHost) void load(); }, [resolvedHost, window.from, window.to]);
  const item = data?.hosts[0];
  return <WorkbenchPage eyebrow="OBSERVABILITY / HOST" title={decodedHost} backTo="/resource/hosts" description={item?.managementStatus === "managed" ? "已纳入站点配置管理。" : "未托管 Host：仅来自观测数据，可用于排查异常访问。"} extra={<Space><Segmented options={presets} value={range} onChange={(value) => { setCustomRange(null); setRange(Number(value)); }} /><DatePicker.RangePicker showTime value={customRange} onChange={(value) => setCustomRange(value as [Dayjs | null, Dayjs | null] | null)} /><Button onClick={load} disabled={!resolvedHost}>刷新</Button></Space>}>
    {!resolvedHost || (loading && !data) ? <Spin /> : <>{error ? <Alert type="error" showIcon message={error} style={{ marginBottom: "var(--space-3)" }} /> : null}<SectionCard title="资源归属" description="配置、证书与关联网站。"><Descriptions size="small" column={{ xs: 1, md: 3 }}><Descriptions.Item label="管理状态"><SignalTag tone={item?.managementStatus === "managed" ? "success" : "warning"}>{item?.managementStatus === "managed" ? "已托管" : "未托管"}</SignalTag></Descriptions.Item><Descriptions.Item label="关联网站">{item?.websites.length ? item.websites.map((website) => <Button key={website.id} type="link" size="small" onClick={() => navigate(websiteViewPath(website.id))}>{website.name || website.id}</Button>) : "—"}</Descriptions.Item><Descriptions.Item label="证书">{item?.certificate ? <>有效至 <LocalTime timestamp={item.certificate.notAfter} /></> : "未发现有效证书"}</Descriptions.Item></Descriptions></SectionCard>
    <div style={{ marginTop: "var(--space-3)" }}><MetricCards overview={data?.overview} /><ResourceTrend overview={data?.overview} timeseries={data?.timeseries} loading={loading} /></div>
    <div style={{ marginTop: "var(--space-3)" }}><RequestHistoryList fixedHost={resolvedHost} embedded /></div></>}
  </WorkbenchPage>;
};
export default DomainDetail;
