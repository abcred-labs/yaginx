import { getDomainAnalytics, type IAnalyticsSubjectView } from "@/api/analytics";
import { TimeWindowControl, useTimeWindow } from "./useTimeWindow";
import { LocalTime } from "@/components/LocalTime";
import { SectionCard, SignalTag, WorkbenchPage, WorkbenchTabs } from "@/ui/workbench";
import { MonAlert, MonDescriptions } from "@/ui/monParts";
import { Button, Spin } from "@modra/react";
import React, { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { RequestHistoryList } from "@/pages/Monitors/RequestHistory/List";
import { MetricCards, ResourceTrend } from "./ResourceMetrics";
import { websiteViewPath } from "./websiteViewRoute";

const DomainDetail: React.FC = () => {
  const { host = "" } = useParams(); const navigate = useNavigate(); const decodedHost = decodeURIComponent(host);
  const timeWindow = useTimeWindow(); const window = timeWindow.window; const [data, setData] = useState<IAnalyticsSubjectView | null>(); const [loading, setLoading] = useState(true); const [error, setError] = useState("");
  const resolvedHost = decodedHost;
  const load = async () => { if (!resolvedHost) return; setLoading(true); setError(""); try { const response = await getDomainAnalytics({ host: resolvedHost, ...window }); setData(response.data ?? null); } catch (reason) { setError(reason instanceof Error ? reason.message : "无法加载该 Host 的资源视图，请稍后重试。"); } finally { setLoading(false); } };
  useEffect(() => { if (resolvedHost) void load(); }, [resolvedHost, window.from, window.to]);
  const item = data?.hosts[0];
  return <WorkbenchPage eyebrow="OBSERVABILITY / HOST" title={decodedHost} backTo="/resource/hosts" description={item?.managementStatus === "managed" ? "已纳入站点配置管理。" : "未托管 Host：仅来自观测数据，可用于排查异常访问。"} extra={<>
    <TimeWindowControl state={timeWindow} />
    <Button variant="outline" onClick={load} disabled={!resolvedHost}>刷新</Button>
  </>}>
    {!resolvedHost || (loading && !data) ? <Spin /> : <>{error ? <MonAlert type="error" message={error} className="mb-3" /> : null}<WorkbenchTabs items={[
      {
        key: "overview",
        label: "概览",
        children: <>
          <SectionCard title="资源归属" description="配置、证书与关联网站。"><MonDescriptions items={[
            { key: "status", label: "管理状态", children: <SignalTag tone={item?.managementStatus === "managed" ? "success" : "warning"}>{item?.managementStatus === "managed" ? "已托管" : "未托管"}</SignalTag> },
            { key: "websites", label: "关联网站", children: item?.websites.length ? <span className="flex flex-wrap">{item.websites.map((website) => <Button key={website.id} variant="link" size="sm" onClick={() => navigate(websiteViewPath(website.id))}>{website.name || website.id}</Button>)}</span> : "—" },
            { key: "certificate", label: "证书", children: item?.certificate ? <>有效至 <LocalTime timestamp={item.certificate.notAfter} /></> : "未发现有效证书" }
          ]} /></SectionCard>
          <div className="mt-3"><MetricCards overview={data?.overview} /><ResourceTrend overview={data?.overview} timeseries={data?.timeseries} loading={loading} /></div>
        </>
      },
      { key: "history", label: "请求追踪", children: <RequestHistoryList fixedHost={resolvedHost} embedded /> }
    ]} /></>}
  </WorkbenchPage>;
};
export default DomainDetail;
