import { ipBlacklistLookup, ipBlacklistUpsert } from "@/api/accessControl";
import { requestHistoryGet, requestHistorySearch, type IRequestHistoryDetail, type IRequestHistoryListItem } from "@/api/requestHistory";
import { LocalTime } from "@/componets/LocalTime";
import PagedTableList from "@/componets/PagedTableList";
import { renderCompactTextCell } from "@/componets/tableCompact";
import IPProfileDrawer, { type IPBlacklistScope } from "../IPProfileDrawer";
import {
  canDecompressResponseBody,
  decompressResponseBody,
  getResponseContentEncodings
} from "./responseBodyDecoder";
import { requestHistoryDetailViewState } from "./requestHistoryDetailState";
import { RequestStatusPopover } from "./RequestStatusPopover";
import { errorCategoryLabel, statusSourceLabel } from "./requestStatusExplanation";
import { DownloadOutlined, MoreOutlined } from "@ant-design/icons";
import { DataToolbar, SectionCard, WorkbenchPage } from "@/componets/Workbench";
import { Button, DatePicker, Descriptions, Drawer, Dropdown, Empty, Input, InputNumber, notification, Select, Space, Spin, Tag, Typography } from "antd";
import dayjs, { Dayjs } from "dayjs";
import React, { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { SPACE } from "@/utils/spacing";
import { formatBytes, formatNumber } from "@/utils/formatters";
import "./requestHistory.less";

const { Paragraph, Text } = Typography;

const exceptionStageText: Record<string, string> = {
  BeforeYarpRoute: "YARP 前",
  YarpProxyPipeline: "YARP 管线",
  YarpForwarder: "YARP 转发"
};

const proxyStageText: Record<string, string> = {
  BeforeYarpRoute: "YARP 前",
  YarpProxyPipeline: "YARP 管线",
  Completed: "完成"
};

const statusSourceText: Record<string, string> = {
  Upstream: "上游返回",
  YaginxProxy: "Yaginx 代理",
  YaginxPipeline: "Yaginx 本地",
  ClientCanceled: "客户端取消",
  CorsPreflight: "CORS 预检",
  Unknown: "未知"
};

const errorCategoryText: Record<string, string> = {
  UpstreamReturnedError: "上游原始错误",
  ClientCanceled: "客户端取消",
  UpstreamTimeout: "上游超时",
  NoAvailableDestination: "无可用上游",
  UpstreamRequestFailure: "上游请求失败",
  UpstreamResponseHeaderFailure: "上游响应头失败",
  UpstreamResponseBodyFailure: "上游响应体失败",
  UpstreamRequestBodyFailure: "上游请求体失败",
  ClientWriteFailure: "向客户端写失败",
  ProxyPipelineException: "Yaginx 管线异常",
  UnknownProxyError: "未知代理异常",
  Blacklist: "IP 黑名单",
  SiteMaintenance: "站点维护中"
};

const statusSourceOptions = Object.entries(statusSourceText).map(([value, label]) => ({ value, label }));
const errorCategoryOptions = Object.entries(errorCategoryText).map(([value, label]) => ({ value, label }));

const formatStage = (value?: string, labels: Record<string, string> = proxyStageText) => {
  if (!value) {
    return "-";
  }
  return labels[value] ?? value;
};

const statusColor = (statusCode: number) => {
  if (statusCode >= 600) {
    return "magenta";
  }
  if (statusCode >= 500 || statusCode <= 0) {
    return "red";
  }
  if (statusCode >= 400) {
    return "orange";
  }
  if (statusCode >= 300) {
    return "blue";
  }
  return "green";
};

const sourceColor = (statusSource?: string) => {
  switch (statusSource) {
    case "Upstream":
      return "blue";
    case "ClientCanceled":
      return "orange";
    case "YaginxProxy":
    case "YaginxPipeline":
      return "magenta";
    default:
      return "default";
  }
};

const decodeBody = (value?: string) => {
  if (!value) {
    return "";
  }
  try {
    const binary = atob(value);
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  } catch {
    return value;
  }
};

const formatContentLengthValue = (value: string) => {
  const normalized = value.trim();
  if (!/^\d+$/.test(normalized)) {
    return value;
  }

  const bytes = Number(normalized);
  if (!Number.isSafeInteger(bytes)) {
    return value;
  }

  const readable = formatBytes(bytes);
  return bytes < 1024 ? readable : `${readable} (${formatNumber(bytes)} B)`;
};

const formatLongJsonHeader = (value: string) => {
  if (value.length <= 200) {
    return null;
  }

  try {
    const parsed: unknown = JSON.parse(value);
    if (parsed === null || typeof parsed !== "object") {
      return null;
    }
    return JSON.stringify(parsed, null, 2);
  } catch {
    return null;
  }
};

const RequestHeaderValue: React.FC<{ name: string; values?: string[] }> = ({ name, values }) => {
  const [expanded, setExpanded] = useState(false);
  const rawValue = (values ?? []).join(", ");
  useEffect(() => setExpanded(false), [rawValue]);

  if (name.toLowerCase() === "content-length") {
    return <Text copyable={{ text: rawValue }}>{(values ?? []).map(formatContentLengthValue).join(", ")}</Text>;
  }

  const formattedJson = formatLongJsonHeader(rawValue);
  if (formattedJson === null) {
    return <Text copyable={{ text: rawValue }}>{rawValue}</Text>;
  }

  const preview = `${rawValue.slice(0, 120)}${rawValue.length > 120 ? "…" : ""}`;
  return (
    <div className="request-history-header-json">
      <Text
        copyable={{ text: rawValue }}
        className={`request-history-header-json__value${expanded ? " request-history-header-json__value--expanded" : " request-history-header-json__value--preview"}`}
      >
        {expanded ? formattedJson : preview}
      </Text>
      <Button type="link" size="small" onClick={() => setExpanded((current) => !current)}>
        {expanded ? "收起" : "展开 JSON"}
      </Button>
    </div>
  );
};

const renderHeaders = (headers: Record<string, string[]> | null | undefined) => {
  const entries = Object.entries(headers ?? {});
  if (entries.length === 0) {
    return <Text type="secondary">无</Text>;
  }

  return (
    <div className="request-history-header-list">
      {entries.map(([key, values]) => (
        <div key={key} className="request-history-header-row">
          <Text strong className="request-history-header-name">{key}</Text>
          <div className="request-history-header-value">
            <RequestHeaderValue name={key} values={values} />
          </div>
        </div>
      ))}
    </div>
  );
};

const formatSource = (value?: string) => {
  if (!value) {
    return "-";
  }

  return statusSourceLabel(value);
};

const formatErrorCategory = (value?: string) => {
  if (!value) {
    return "-";
  }

  return errorCategoryLabel(value);
};

const downloadJson = (filename: string, value: unknown) => {
  const blob = new Blob([JSON.stringify(value, null, 2)], { type: "application/json;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
};

const safeFilenamePart = (value: string) => value.trim().replace(/[^a-zA-Z0-9._-]+/g, "_") || "all";

const BodyBlock = ({ title, value, truncated, emptyText }: { title: string; value?: string; truncated?: boolean; emptyText: string }) => {
  if (!value) {
    return (
      <div style={{ marginTop: "var(--space-3)" }}>
        <Text strong>{title}</Text>
        <Text type="secondary" style={{ display: "block", marginTop: "var(--space-1)" }}>{emptyText}</Text>
      </div>
    );
  }

  return (
    <div style={{ marginTop: "var(--space-3)" }}>
      <Space size={SPACE.space2} style={{ marginBottom: "var(--space-1)" }}>
        <Text strong>{title}</Text>
        {truncated ? <Tag color="orange">已截断</Tag> : null}
      </Space>
      <Paragraph
        copyable
        style={{
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          maxHeight: 260,
          overflow: "auto",
          padding: "var(--space-3)",
          background: "#f7f8fa",
          border: "1px solid #e5e7eb",
          borderRadius: 6
        }}
      >
        {decodeBody(value)}
      </Paragraph>
    </div>
  );
};

const ResponseBodyBlock = ({
  value,
  headers,
  truncated,
  statusCode
}: {
  value?: string;
  headers: Record<string, string[]> | null | undefined;
  truncated?: boolean;
  statusCode?: number;
}) => {
  const encodings = getResponseContentEncodings(headers);
  const canDecompress = canDecompressResponseBody(encodings);
  const [view, setView] = useState<"raw" | "decoded">("raw");
  const [decodedBody, setDecodedBody] = useState("");
  const [decodeError, setDecodeError] = useState("");
  const [decoding, setDecoding] = useState(false);

  useEffect(() => {
    setView("raw");
    setDecodedBody("");
    setDecodeError("");
    setDecoding(false);
  }, [value, encodings.join(",")]);

  if (!value) {
    return (
      <div style={{ marginTop: "var(--space-3)" }}>
        <Text strong>Response Body</Text>
        <Text type="secondary" style={{ display: "block", marginTop: "var(--space-1)" }}>
          {statusCode !== undefined && statusCode >= 200 && statusCode < 300
            ? "成功响应默认不采集 Response Body，仅保留元数据和 Header，避免保存敏感或过大的内容。"
            : "该响应未采集 Response Body。"}
        </Text>
      </div>
    );
  }

  const decompress = async () => {
    setDecoding(true);
    setDecodeError("");
    const result = await decompressResponseBody(value, encodings);
    setDecoding(false);
    if (result.ok) {
      setDecodedBody(result.text);
      setView("decoded");
      return;
    }
    setDecodeError(result.error);
  };

  const displayedBody = canDecompress && view === "raw" ? value : (canDecompress ? decodedBody : decodeBody(value));

  return (
    <div style={{ marginTop: "var(--space-3)" }}>
      <Space size={SPACE.space2} style={{ marginBottom: "var(--space-1)" }} wrap>
        <Text strong>Response Body</Text>
        {truncated ? <Tag color="orange">已截断</Tag> : null}
        {canDecompress ? <Tag color="blue">压缩: {encodings.join(", ")}</Tag> : null}
        {canDecompress ? (
          <Button size="small" loading={decoding} onClick={view === "decoded" ? () => setView("raw") : decompress}>
            {view === "decoded" ? "查看原始" : "解压查看"}
          </Button>
        ) : null}
      </Space>
      {decodeError ? <Text type="danger" style={{ display: "block", marginBottom: "var(--space-1)" }}>{decodeError}</Text> : null}
      <Paragraph
        copyable
        style={{
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          maxHeight: 260,
          overflow: "auto",
          padding: "var(--space-3)",
          background: "#f7f8fa",
          border: "1px solid #e5e7eb",
          borderRadius: 6
        }}
      >
        {displayedBody}
      </Paragraph>
    </div>
  );
};

const RequestIPCell = ({
  ip,
  onProfile,
  onBlacklist
}: {
  ip?: string;
  onProfile: (ip: string) => void;
  onBlacklist: (ip: string) => void;
}) => {
  const [hovered, setHovered] = useState(false);
  const normalized = ip?.trim();
  if (!normalized) {
    return <>-</>;
  }
  return (
    <Space size={SPACE.space0} onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}>
      <Typography.Text>{normalized}</Typography.Text>
      <Dropdown
        trigger={["click"]}
        menu={{
          items: [
            { key: "profile", label: "IP 详情" },
            { key: "blacklist", label: "添加到黑名单", danger: true }
          ],
          onClick: ({ key }) => key === "profile" ? onProfile(normalized) : onBlacklist(normalized)
        }}
      >
        <Button
          aria-label={`${normalized} 操作`}
          type="text"
          size="small"
          icon={<MoreOutlined />}
          style={{ opacity: hovered ? 1 : 0, paddingInline: 3, transition: "opacity 120ms ease" }}
        />
      </Dropdown>
    </Space>
  );
};

export interface RequestHistoryListProps {
  fixedHost?: string;
  embedded?: boolean;
}

export const RequestHistoryList: React.FC<RequestHistoryListProps> = ({ fixedHost, embedded = false }) => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const initialNumber = (key: string) => { const value = Number(searchParams.get(key)); return Number.isFinite(value) && value > 0 ? value : null; };
  const [refreshTable, forceRefreshTable] = useState<number>(0);
  const [host, setHost] = useState<string>(() => searchParams.get("host") || "");
  const websiteId = searchParams.get("websiteId") || undefined;
  const initialStatusCodePattern = () => {
    const pattern = searchParams.get("statusCodePattern");
    if (pattern !== null) return pattern;
    const family = searchParams.get("statusFamily");
    if (family && /^[1-6]xx$/i.test(family)) return family;
    const exactStatusCode = initialNumber("statusCode");
    return exactStatusCode === null ? "" : String(exactStatusCode);
  };
  const [statusCodePattern, setStatusCodePattern] = useState<string>(initialStatusCodePattern);
  const [statusCodePatternInput, setStatusCodePatternInput] = useState<string>(initialStatusCodePattern);
  const [statusCodePatternInvalid, setStatusCodePatternInvalid] = useState(false);
  const [upstreamStatusCode, setUpstreamStatusCode] = useState<number | null>(() => initialNumber("upstreamStatusCode"));
  const [statusSource, setStatusSource] = useState<string | undefined>(undefined);
  const [errorCategory, setErrorCategory] = useState<string | undefined>(() => searchParams.get("errorCategory") || undefined);
  const [errorCode, setErrorCode] = useState<string>(() => searchParams.get("errorCode") || "");
  const [clientIp, setClientIp] = useState<string>("");
  const [userAgent, setUserAgent] = useState<string>("");
  const [timeRange, setTimeRange] = useState<[Dayjs | null, Dayjs | null] | null>(() => {
    const from = initialNumber("from"); const to = initialNumber("to");
    return from && to ? [dayjs.unix(from), dayjs.unix(to)] : null;
  });
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detail, setDetail] = useState<IRequestHistoryDetail | null>(null);
  const [detailMissing, setDetailMissing] = useState(false);
  const [profileOpen, setProfileOpen] = useState(false);
  const [profileLoading, setProfileLoading] = useState(false);
  const [profile, setProfile] = useState<any>(null);
  const [profileError, setProfileError] = useState("");
  const [currentPageRecords, setCurrentPageRecords] = useState<IRequestHistoryListItem[]>([]);

  const updateStatusCodePatternInput = (value: string) => {
    setStatusCodePatternInput(value);
    if (!value.trim()) {
      setStatusCodePatternInvalid(false);
      setStatusCodePattern("");
      return;
    }
    setStatusCodePatternInvalid(!/^[0-9x]{3}$/i.test(value.trim()));
  };

  const applyStatusCodePattern = (value: string) => {
    const pattern = value.trim();
    if (!pattern) {
      setStatusCodePatternInvalid(false);
      setStatusCodePattern("");
      return;
    }
    if (!/^[0-9x]{3}$/i.test(pattern)) {
      setStatusCodePatternInvalid(true);
      return;
    }
    setStatusCodePatternInvalid(false);
    setStatusCodePattern(pattern.toLowerCase());
  };

  const externalFilterCondition = useMemo(() => {
    const filters: any = {};
    if (websiteId) filters.websiteId = websiteId;
    if (fixedHost?.trim()) {
      filters.host = fixedHost.trim();
    } else if (host.trim()) {
      filters.host = host.trim();
    }
    if (statusCodePattern) filters.statusCodePattern = statusCodePattern;
    if (upstreamStatusCode !== null) {
      filters.upstreamStatusCode = upstreamStatusCode;
    }
    if (statusSource) {
      filters.statusSource = statusSource;
    }
    if (errorCategory) {
      filters.errorCategory = errorCategory;
    }
    if (errorCode.trim()) {
      filters.errorCode = errorCode.trim();
    }
    if (clientIp.trim()) {
      filters.clientIp = clientIp.trim();
    }
    if (userAgent.trim()) {
      filters.userAgent = userAgent.trim();
    }
    if (timeRange && timeRange[0] && timeRange[1]) {
      filters.from = Math.floor(timeRange[0].valueOf() / 1000);
      filters.to = Math.floor(timeRange[1].valueOf() / 1000);
    }
    return filters;
  }, [clientIp, errorCategory, errorCode, fixedHost, host, statusCodePattern, statusSource, timeRange, upstreamStatusCode, userAgent, websiteId]);

  const openDetail = async (record: any) => {
    setDetailOpen(true);
    setDetailLoading(true);
    setDetail(null);
    setDetailMissing(false);
    try {
      const response = await requestHistoryGet({ id: record.id });
      setDetail(response.data);
      setDetailMissing(response.data === null);
    } finally {
      setDetailLoading(false);
    }
  };

  const detailViewState = requestHistoryDetailViewState(detailLoading, detailMissing, Boolean(detail));

  const openIPProfile = async (ip: string) => {
    setProfileOpen(true);
    setProfileLoading(true);
    setProfile(null);
    setProfileError("");
    try {
      const response = await ipBlacklistLookup({ ip });
      setProfile(response.data);
    } catch {
      setProfileError("无法从工具服务获取该 IP 的最新资料。");
    } finally {
      setProfileLoading(false);
    }
  };

  const addToBlacklist = async (cidr: string, scope: IPBlacklistScope = "ip") => {
    try {
      const response = await ipBlacklistUpsert({ cidr });
      const label = scope === "network" ? "网络段" : "IP";
      notification.success({ message: `已添加${label}到黑名单`, description: `${response.data.cidr} 将在所有节点热更新。` });
    } catch {
      // FetchJson already presents the service error.
    }
  };

  const exportCurrentPage = () => {
    const hostPart = fixedHost?.trim() || host.trim();
    downloadJson(`request-history-${safeFilenamePart(hostPart)}.json`, currentPageRecords);
  };

  const initTableColumn = (columnsArray: any[]) => {
    columnsArray.push({
      title: "操作",
      key: "operation",
      width: 76,
      render: (_: any, record: any) => <Button size="small" onClick={() => openDetail(record)}>详情</Button>
    });
    columnsArray.push({
      title: "时间",
      dataIndex: "requestAt",
      key: "requestAt",
      width: 184,
      render: (_: any, record: any) => <span style={{ whiteSpace: "nowrap" }}><LocalTime timestamp={record.requestAt} /></span>
    });
    columnsArray.push({
      title: "客户端IP",
      dataIndex: "clientIp",
      key: "clientIp",
      width: 150,
      render: (value: string) => <RequestIPCell ip={value} onProfile={openIPProfile} onBlacklist={addToBlacklist} />
    });
    columnsArray.push({ title: "方法", dataIndex: "method", key: "method", width: 76 });
    columnsArray.push({ title: "Host", dataIndex: "host", key: "host", width: 176, render: (value: string) => value ? <Button type="link" style={{ padding: 0 }} onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(value)}`)}>{renderCompactTextCell(value, { emptyText: "-", maxWidth: 160 })}</Button> : "-" });
    columnsArray.push({ title: "Path", dataIndex: "path", key: "path", width: 206, render: (value: string) => renderCompactTextCell(value, { emptyText: "-", maxWidth: 190 }) });
    columnsArray.push({
      title: "最终状态",
      dataIndex: "statusCode",
      key: "statusCode",
      width: 96,
      render: (value: number, record: any) => <RequestStatusPopover field="final" value={value} record={record} color={statusColor(value)} />
    });
    columnsArray.push({
      title: "上游状态",
      dataIndex: "upstreamStatusCode",
      key: "upstreamStatusCode",
      width: 96,
      render: (value: number | null | undefined, record: any) => <RequestStatusPopover field="upstream" value={value} record={record} color={statusColor(value ?? 0)} />
    });
    columnsArray.push({ title: "耗时", dataIndex: "durationMs", key: "durationMs", width: 92, render: (value: number) => `${value}ms` });
    columnsArray.push({ title: "User-Agent", dataIndex: "userAgent", key: "userAgent", width: 220, render: (value: string) => renderCompactTextCell(value, { emptyText: "-", maxWidth: 204 }) });
    columnsArray.push({
      title: "来源",
      dataIndex: "statusSource",
      key: "statusSource",
      width: 108,
      render: (value: string) => value ? <Tag color={sourceColor(value)}>{formatSource(value)}</Tag> : "-"
    });
    columnsArray.push({
      title: "错误分类",
      dataIndex: "errorCategory",
      key: "errorCategory",
      width: 138,
      render: (value: string) => value ? <Tag color="volcano">{formatErrorCategory(value)}</Tag> : "-"
    });
    columnsArray.push({
      title: "错误代码",
      dataIndex: "errorCode",
      key: "errorCode",
      width: 156,
      render: (value: string) => renderCompactTextCell(value, { emptyText: "-", maxWidth: 140 })
    });
    columnsArray.push({ title: "代理阶段", dataIndex: "proxyStage", key: "proxyStage", width: 110, render: (value: string) => formatStage(value) });
    columnsArray.push({
      title: "异常阶段",
      dataIndex: "exceptionStage",
      key: "exceptionStage",
      width: 118,
      render: (value: string) => value ? <Tag color="red">{formatStage(value, exceptionStageText)}</Tag> : "-"
    });
    columnsArray.push({
      title: "Forwarder Error",
      dataIndex: "forwarderError",
      key: "forwarderError",
      width: 220,
      render: (value: string) => renderCompactTextCell(value, { emptyText: "-", maxWidth: 204 })
    });
  };

  const content = <>
      <DataToolbar>
          <DatePicker.RangePicker
            showTime
            value={timeRange}
            onChange={(values) => setTimeRange(values as [Dayjs | null, Dayjs | null] | null)}
            style={{ width: 308 }}
          />
          {!fixedHost ? <Input placeholder="域名" value={host} onChange={(event) => setHost(event.target.value)} style={{ width: 180 }} /> : null}
          <Input.Search
            placeholder="最终状态码（如 4xx、4x3）"
            value={statusCodePatternInput}
            status={statusCodePatternInvalid ? "error" : undefined}
            onChange={(event) => updateStatusCodePatternInput(event.target.value)}
            onSearch={applyStatusCodePattern}
            allowClear
            style={{ width: 190 }}
          />
          <InputNumber
            min={100}
            max={999}
            precision={0}
            placeholder="上游状态码"
            value={upstreamStatusCode}
            onChange={(value) => setUpstreamStatusCode(typeof value === "number" ? value : null)}
            style={{ width: 120 }}
          />
          <Select
            allowClear
            placeholder="状态来源"
            value={statusSource}
            onChange={(value) => setStatusSource(value)}
            options={statusSourceOptions}
            style={{ width: 140 }}
          />
          <Select
            allowClear
            showSearch
            placeholder="错误分类"
            value={errorCategory}
            onChange={(value) => setErrorCategory(value)}
            options={errorCategoryOptions}
            style={{ width: 180 }}
          />
          <Input placeholder="客户端 IP" value={clientIp} onChange={(event) => setClientIp(event.target.value)} style={{ width: 150 }} />
          <Input placeholder="错误代码" value={errorCode} onChange={(event) => setErrorCode(event.target.value)} style={{ width: 160 }} />
          <Input.Search placeholder="User-Agent" value={userAgent} onChange={(event) => setUserAgent(event.target.value)} onSearch={() => forceRefreshTable(Math.random() + 1)} style={{ width: 220 }} />
          <Button onClick={() => forceRefreshTable(Math.random() + 1)}>刷新</Button>
          <Button icon={<DownloadOutlined />} disabled={currentPageRecords.length === 0} onClick={exportCurrentPage}>导出当前页</Button>
      </DataToolbar>
      <PagedTableList
        pkFieldName="id"
        searchAction={requestHistorySearch}
        realodTableData={refreshTable}
        initTableColumn={initTableColumn}
        externalFilterCondition={externalFilterCondition}
        onPageDataChange={setCurrentPageRecords}
      />

      <Drawer
        rootClassName="request-history-detail-drawer"
        title="请求诊断详情"
        width={760}
        open={detailOpen}
        onClose={() => setDetailOpen(false)}
        styles={{
          header: { padding: "var(--space-3) var(--space-4)" },
          body: { padding: "var(--space-4)" }
        }}
      >
        {detailViewState === "loading" ? <Spin /> : null}
        {detailViewState === "missing" ? <Empty description="该请求记录已不存在或已被清理" /> : null}
        {detailViewState === "detail" && detail ? (
          <>
            <Descriptions bordered size="small" column={2}>
              <Descriptions.Item label="时间"><LocalTime timestamp={detail.requestAt} /></Descriptions.Item>
              <Descriptions.Item label="耗时">{detail.durationMs}ms</Descriptions.Item>
              <Descriptions.Item label="客户端 IP">{detail.clientIp || "-"}</Descriptions.Item>
              <Descriptions.Item label="最终状态码"><Tag color={statusColor(detail.statusCode)}>{detail.statusCode}</Tag></Descriptions.Item>
              <Descriptions.Item label="上游状态码">{detail.upstreamStatusCode ? <Tag color={statusColor(detail.upstreamStatusCode)}>{detail.upstreamStatusCode}</Tag> : "-"}</Descriptions.Item>
              <Descriptions.Item label="状态来源"><Tag color={sourceColor(detail.statusSource)}>{formatSource(detail.statusSource)}</Tag></Descriptions.Item>
              <Descriptions.Item label="方法">{detail.method}</Descriptions.Item>
              <Descriptions.Item label="协议">{detail.protocol}</Descriptions.Item>
              <Descriptions.Item label="Host">{detail.host}</Descriptions.Item>
              <Descriptions.Item label="Path">{detail.path}</Descriptions.Item>
              <Descriptions.Item label="Query" span={2}>{detail.queryString || "-"}</Descriptions.Item>
              <Descriptions.Item label="URL" span={2}>{detail.url}</Descriptions.Item>
              <Descriptions.Item label="错误分类">{formatErrorCategory(detail.errorCategory)}</Descriptions.Item>
              <Descriptions.Item label="错误代码">{detail.errorCode || "-"}</Descriptions.Item>
			  <Descriptions.Item label="Trace ID" span={2}>{detail.traceId || "-"}</Descriptions.Item>
              <Descriptions.Item label="代理阶段">{formatStage(detail.proxyStage)}</Descriptions.Item>
              <Descriptions.Item label="异常阶段">{formatStage(detail.exceptionStage, exceptionStageText)}</Descriptions.Item>
              <Descriptions.Item label="Forwarder Error" span={2}>{detail.forwarderError || "-"}</Descriptions.Item>
            </Descriptions>

            <div style={{ marginTop: "var(--space-3)" }}>
              <Text strong>请求 Header</Text>
              <div style={{ marginTop: "var(--space-1)" }}>{renderHeaders(detail.requestHeaders)}</div>
            </div>

            <div style={{ marginTop: "var(--space-3)" }}>
              <Text strong>响应 Header</Text>
              <div style={{ marginTop: "var(--space-1)" }}>{renderHeaders(detail.responseHeaders)}</div>
            </div>

            {detail.exceptionType || detail.exceptionMessage ? (
              <div style={{ marginTop: "var(--space-3)" }}>
                <Text strong>异常信息</Text>
                <Paragraph
                  copyable
                  style={{
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-word",
                    maxHeight: 260,
                    overflow: "auto",
                    padding: "var(--space-3)",
                    background: "#fff7f7",
                    border: "1px solid #fecaca",
                    borderRadius: 6,
                    marginTop: "var(--space-1)"
                  }}
                >
                  {[detail.exceptionType, detail.exceptionMessage, detail.exceptionStackTrace].filter(Boolean).join("\n\n")}
                </Paragraph>
              </div>
            ) : null}

            <BodyBlock
              title="Request Body"
              value={detail.requestBodyBase64}
              truncated={detail.requestBodyTruncated}
              emptyText="该请求未采集 Request Body，可能没有正文或内容类型不在采集范围内。"
            />
            <ResponseBodyBlock
              value={detail.responseBodyBase64}
              headers={detail.responseHeaders}
              truncated={detail.responseBodyTruncated}
              statusCode={detail.statusCode}
            />
          </>
        ) : null}
      </Drawer>
      <IPProfileDrawer
        open={profileOpen}
        loading={profileLoading}
        profile={profile}
        error={profileError}
        onClose={() => setProfileOpen(false)}
        onBlacklist={addToBlacklist}
      />
    </>;
  if (embedded) {
    return <SectionCard title="请求追踪" description={`完整追踪记录已限定为 ${fixedHost}。`}>{content}</SectionCard>;
  }
  return <WorkbenchPage eyebrow="ANALYTICS / REQUEST TRACE" title="请求追踪" description="定位上游返回、代理异常和请求上下文；选择任意记录查看完整诊断详情。">{content}</WorkbenchPage>;
};

export default RequestHistoryList;
