import { ipBlacklistLookup, ipBlacklistUpsert } from "@/api/accessControl";
import { requestHistoryGet, requestHistorySearch, type IRequestHistoryDetail, type IRequestHistoryListItem } from "@/api/requestHistory";
import { LocalTime } from "@/componets/LocalTime";
import ServerDataTable from "@/ui/ServerDataTable";
import { ClearableSelect } from "@/ui/reqFilters";
import { notifySuccess } from "@/ui/notify";
import { DataToolbar, SectionCard, WorkbenchPage } from "@/ui/workbench";
import IPProfileDrawer, { type IPBlacklistScope } from "../IPProfileDrawer";
import {
  canDecompressResponseBody,
  decompressResponseBody,
  getResponseContentEncodings
} from "./responseBodyDecoder";
import { requestHistoryDetailViewState } from "./requestHistoryDetailState";
import { RequestStatusPopover } from "./RequestStatusPopover";
import { errorCategoryLabel, statusSourceLabel } from "./requestStatusExplanation";
import {
  Badge,
  Button,
  CopyableText,
  DateTimeRangePicker,
  Descriptions,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Empty,
  Input,
  InputNumber,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  Spin,
  TruncatedText,
  type BadgeProps,
  type DateTimeRange,
  type DescriptionItem
} from "@modra/react";
import type { ColumnDef } from "@tanstack/react-table";
import { Check, Copy, Download, MoreHorizontal, Search } from "lucide-react";
import React, { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { formatBytes, formatNumber } from "@/utils/formatters";

type BadgeVariant = BadgeProps["variant"];

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

const statusVariant = (statusCode: number): BadgeVariant => {
  if (statusCode >= 600) {
    return "default";
  }
  if (statusCode >= 500 || statusCode <= 0) {
    return "destructive";
  }
  if (statusCode >= 400) {
    return "warning";
  }
  if (statusCode >= 300) {
    return "info";
  }
  return "success";
};

const sourceVariant = (statusSource?: string): BadgeVariant => {
  switch (statusSource) {
    case "Upstream":
      return "info";
    case "ClientCanceled":
      return "warning";
    case "YaginxProxy":
    case "YaginxPipeline":
      return "default";
    default:
      return "secondary";
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

/** 复制按钮：写入剪贴板后短暂显示已复制。 */
const CopyButton: React.FC<{ text: string; className?: string }> = ({ text, className }) => {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      className={className}
      aria-label={copied ? "已复制" : "复制"}
      onClick={() => {
        void navigator.clipboard?.writeText(text).then(() => setCopied(true));
      }}
    >
      {copied ? <Check className="size-4 text-success" aria-hidden="true" /> : <Copy className="size-4" aria-hidden="true" />}
    </Button>
  );
};

const CODE_BLOCK_CLASS = "max-h-[260px] overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-muted p-3 pe-10 font-mono text-xs";

/** 大段文本块：右上角带复制按钮。 */
const CodeBlock: React.FC<{ text: string; tone?: "default" | "danger"; className?: string }> = ({ text, tone = "default", className }) => (
  <div className={`relative ${className ?? ""}`}>
    <pre className={`${CODE_BLOCK_CLASS} ${tone === "danger" ? "border-destructive/40 bg-destructive/10" : ""}`}>{text}</pre>
    <CopyButton text={text} className="absolute end-1 top-1 size-7" />
  </div>
);

const RequestHeaderValue: React.FC<{ name: string; values?: string[] }> = ({ name, values }) => {
  const [expanded, setExpanded] = useState(false);
  const rawValue = (values ?? []).join(", ");
  useEffect(() => setExpanded(false), [rawValue]);

  if (name.toLowerCase() === "content-length") {
    return <CopyableText value={rawValue}>{(values ?? []).map(formatContentLengthValue).join(", ")}</CopyableText>;
  }

  const formattedJson = formatLongJsonHeader(rawValue);
  if (formattedJson === null) {
    return <CopyableText value={rawValue}>{rawValue}</CopyableText>;
  }

  const preview = `${rawValue.slice(0, 120)}${rawValue.length > 120 ? "…" : ""}`;
  return (
    <div className="min-w-0">
      <div className="flex items-start gap-1">
        {expanded ? (
          <pre className="max-h-60 min-w-0 flex-1 overflow-auto whitespace-pre-wrap break-words rounded-sm border border-border bg-muted p-2 font-mono">{formattedJson}</pre>
        ) : (
          <span className="block max-h-[3.2em] min-w-0 flex-1 overflow-hidden whitespace-pre-wrap [overflow-wrap:anywhere]">{preview}</span>
        )}
        <CopyButton text={rawValue} className="size-6 shrink-0" />
      </div>
      <Button type="button" variant="link" size="sm" className="h-auto p-0" onClick={() => setExpanded((current) => !current)}>
        {expanded ? "收起" : "展开 JSON"}
      </Button>
    </div>
  );
};

const renderHeaders = (headers: Record<string, string[]> | null | undefined) => {
  const entries = Object.entries(headers ?? {});
  if (entries.length === 0) {
    return <span className="text-muted-foreground">无</span>;
  }

  return (
    <div className="grid gap-1">
      {entries.map(([key, values]) => (
        <div key={key} className="grid grid-cols-[150px_minmax(0,1fr)] items-start gap-2">
          <span className="min-w-0 font-semibold [overflow-wrap:anywhere]">{key}</span>
          <div className="min-w-0 [overflow-wrap:anywhere]">
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
      <div className="mt-3">
        <span className="font-semibold">{title}</span>
        <span className="mt-1 block text-muted-foreground">{emptyText}</span>
      </div>
    );
  }

  return (
    <div className="mt-3">
      <div className="mb-1 flex items-center gap-2">
        <span className="font-semibold">{title}</span>
        {truncated ? <Badge variant="warning">已截断</Badge> : null}
      </div>
      <CodeBlock text={decodeBody(value)} />
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
      <div className="mt-3">
        <span className="font-semibold">Response Body</span>
        <span className="mt-1 block text-muted-foreground">
          {statusCode !== undefined && statusCode >= 200 && statusCode < 300
            ? "成功响应默认不采集 Response Body，仅保留元数据和 Header，避免保存敏感或过大的内容。"
            : "该响应未采集 Response Body。"}
        </span>
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
    <div className="mt-3">
      <div className="mb-1 flex flex-wrap items-center gap-2">
        <span className="font-semibold">Response Body</span>
        {truncated ? <Badge variant="warning">已截断</Badge> : null}
        {canDecompress ? <Badge variant="info">压缩: {encodings.join(", ")}</Badge> : null}
        {canDecompress ? (
          <Button size="sm" variant="outline" loading={decoding} onClick={view === "decoded" ? () => setView("raw") : decompress}>
            {view === "decoded" ? "查看原始" : "解压查看"}
          </Button>
        ) : null}
      </div>
      {decodeError ? <span className="mb-1 block text-destructive">{decodeError}</span> : null}
      <CodeBlock text={displayedBody} />
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
  const normalized = ip?.trim();
  if (!normalized) {
    return <>-</>;
  }
  return (
    <span className="group inline-flex items-center gap-0.5">
      <span>{normalized}</span>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={`${normalized} 操作`}
            className="size-6 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100"
          >
            <MoreHorizontal className="size-4" aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem onSelect={() => onProfile(normalized)}>IP 详情</DropdownMenuItem>
          <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={() => onBlacklist(normalized)}>添加到黑名单</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </span>
  );
};

const compactText = (value: unknown, maxWidth: number) =>
  typeof value === "string" && value ? <TruncatedText className="inline-block align-bottom" style={{ maxWidth }}>{value}</TruncatedText> : "-";

export interface RequestHistoryListProps {
  fixedHost?: string;
  embedded?: boolean;
}

const STATUS_PATTERN = /^[0-9x]{3}$/i;

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
  const [timeRange, setTimeRange] = useState<DateTimeRange | undefined>(() => {
    const from = initialNumber("from"); const to = initialNumber("to");
    return from && to ? { from: new Date(from * 1000), to: new Date(to * 1000) } : undefined;
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
    setStatusCodePatternInvalid(!STATUS_PATTERN.test(value.trim()));
  };

  const applyStatusCodePattern = (value: string) => {
    const pattern = value.trim();
    if (!pattern) {
      setStatusCodePatternInvalid(false);
      setStatusCodePattern("");
      return;
    }
    if (!STATUS_PATTERN.test(pattern)) {
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
    if (timeRange?.from && timeRange?.to) {
      filters.from = Math.floor(timeRange.from.getTime() / 1000);
      filters.to = Math.floor(timeRange.to.getTime() / 1000);
    }
    return filters;
  }, [clientIp, errorCategory, errorCode, fixedHost, host, statusCodePattern, statusSource, timeRange, upstreamStatusCode, userAgent, websiteId]);

  const openDetail = async (record: IRequestHistoryListItem) => {
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
      notifySuccess(`已添加${label}到黑名单`, `${response.data.cidr} 将在所有节点热更新。`);
    } catch {
      // FetchJson already presents the service error.
    }
  };

  const exportCurrentPage = () => {
    const hostPart = fixedHost?.trim() || host.trim();
    downloadJson(`request-history-${safeFilenamePart(hostPart)}.json`, currentPageRecords);
  };

  const columns = useMemo<ColumnDef<IRequestHistoryListItem, unknown>[]>(() => [
    {
      id: "operation",
      header: "操作",
      cell: ({ row }) => <Button size="sm" variant="outline" onClick={() => void openDetail(row.original)}>详情</Button>
    },
    {
      id: "requestAt",
      header: "时间",
      meta: { minWidth: 168 },
      cell: ({ row }) => <span className="whitespace-nowrap"><LocalTime timestamp={row.original.requestAt} /></span>
    },
    {
      id: "clientIp",
      header: "客户端IP",
      meta: { minWidth: 150 },
      cell: ({ row }) => <RequestIPCell ip={row.original.clientIp} onProfile={openIPProfile} onBlacklist={addToBlacklist} />
    },
    { id: "method", header: "方法", cell: ({ row }) => row.original.method },
    {
      id: "host",
      header: "Host",
      meta: { minWidth: 176 },
      cell: ({ row }) => {
        const value = row.original.host;
        return value ? (
          <Button type="button" variant="link" className="h-auto p-0" onClick={() => navigate(`/resource/hosts/view/${encodeURIComponent(value)}`)}>
            {compactText(value, 160)}
          </Button>
        ) : "-";
      }
    },
    { id: "path", header: "Path", meta: { minWidth: 206 }, cell: ({ row }) => compactText(row.original.path, 190) },
    {
      id: "statusCode",
      header: "最终状态",
      cell: ({ row }) => <RequestStatusPopover field="final" value={row.original.statusCode} record={row.original} variant={statusVariant(row.original.statusCode)} />
    },
    {
      id: "upstreamStatusCode",
      header: "上游状态",
      cell: ({ row }) => {
        const value = row.original.upstreamStatusCode;
        return <RequestStatusPopover field="upstream" value={value} record={row.original} variant={statusVariant(value ?? 0)} />;
      }
    },
    { id: "durationMs", header: "耗时", cell: ({ row }) => `${row.original.durationMs}ms` },
    { id: "userAgent", header: "User-Agent", meta: { minWidth: 220 }, cell: ({ row }) => compactText(row.original.userAgent, 204) },
    {
      id: "statusSource",
      header: "来源",
      cell: ({ row }) => row.original.statusSource ? <Badge variant={sourceVariant(row.original.statusSource)}>{formatSource(row.original.statusSource)}</Badge> : "-"
    },
    {
      id: "errorCategory",
      header: "错误分类",
      meta: { minWidth: 138 },
      cell: ({ row }) => row.original.errorCategory ? <Badge variant="warning">{formatErrorCategory(row.original.errorCategory)}</Badge> : "-"
    },
    { id: "errorCode", header: "错误代码", meta: { minWidth: 156 }, cell: ({ row }) => compactText(row.original.errorCode, 140) },
    { id: "proxyStage", header: "代理阶段", cell: ({ row }) => formatStage(row.original.proxyStage) },
    {
      id: "exceptionStage",
      header: "异常阶段",
      cell: ({ row }) => row.original.exceptionStage ? <Badge variant="destructive">{formatStage(row.original.exceptionStage, exceptionStageText)}</Badge> : "-"
    },
    { id: "forwarderError", header: "Forwarder Error", meta: { minWidth: 220 }, cell: ({ row }) => compactText(row.original.forwarderError, 204) }
    // 回调只依赖稳定的 setState / navigate / API，列定义无需随其重建
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], []);

  const detailItems: DescriptionItem[] = detail ? [
    { key: "requestAt", label: "时间", children: <LocalTime timestamp={detail.requestAt} /> },
    { key: "duration", label: "耗时", children: `${detail.durationMs}ms` },
    { key: "clientIp", label: "客户端 IP", children: detail.clientIp || "-" },
    { key: "statusCode", label: "最终状态码", children: <Badge variant={statusVariant(detail.statusCode)}>{detail.statusCode}</Badge> },
    { key: "upstream", label: "上游状态码", children: detail.upstreamStatusCode ? <Badge variant={statusVariant(detail.upstreamStatusCode)}>{detail.upstreamStatusCode}</Badge> : "-" },
    { key: "source", label: "状态来源", children: <Badge variant={sourceVariant(detail.statusSource)}>{formatSource(detail.statusSource)}</Badge> },
    { key: "method", label: "方法", children: detail.method },
    { key: "protocol", label: "协议", children: detail.protocol },
    { key: "host", label: "Host", children: detail.host },
    { key: "path", label: "Path", children: detail.path },
    { key: "query", label: "Query", span: 2, children: detail.queryString || "-" },
    { key: "url", label: "URL", span: 2, children: <span className="[overflow-wrap:anywhere]">{detail.url}</span> },
    { key: "errorCategory", label: "错误分类", children: formatErrorCategory(detail.errorCategory) },
    { key: "errorCode", label: "错误代码", children: detail.errorCode || "-" },
    { key: "traceId", label: "Trace ID", span: 2, children: detail.traceId || "-" },
    { key: "proxyStage", label: "代理阶段", children: formatStage(detail.proxyStage) },
    { key: "exceptionStage", label: "异常阶段", children: formatStage(detail.exceptionStage, exceptionStageText) },
    { key: "forwarderError", label: "Forwarder Error", span: 2, children: detail.forwarderError || "-" }
  ] : [];

  const content = <>
      <DataToolbar>
          <DateTimeRangePicker
            className="w-[308px]"
            aria-label="时间范围"
            placeholder="时间范围"
            value={timeRange}
            onValueChange={setTimeRange}
          />
          {!fixedHost ? <Input className="w-[180px]" placeholder="域名" aria-label="域名" value={host} onChange={(event) => setHost(event.target.value)} /> : null}
          <form
            className="flex items-center gap-1"
            onSubmit={(event) => { event.preventDefault(); applyStatusCodePattern(statusCodePatternInput); }}
          >
            <Input
              className={`w-[190px] ${statusCodePatternInvalid ? "border-destructive" : ""}`}
              placeholder="最终状态码（如 4xx、4x3）"
              aria-label="最终状态码"
              aria-invalid={statusCodePatternInvalid || undefined}
              value={statusCodePatternInput}
              onChange={(event) => updateStatusCodePatternInput(event.target.value)}
            />
            <Button type="submit" variant="outline" size="icon" aria-label="按状态码查询">
              <Search className="size-4" aria-hidden="true" />
            </Button>
          </form>
          <InputNumber
            className="w-[120px]"
            min={100}
            max={999}
            step={1}
            placeholder="上游状态码"
            aria-label="上游状态码"
            value={upstreamStatusCode}
            onValueChange={(value) => setUpstreamStatusCode(typeof value === "number" ? Math.round(value) : null)}
          />
          <ClearableSelect className="w-[140px]" placeholder="状态来源" value={statusSource} onChange={setStatusSource} options={statusSourceOptions} />
          <ClearableSelect className="w-[180px]" placeholder="错误分类" value={errorCategory} onChange={setErrorCategory} options={errorCategoryOptions} />
          <Input className="w-[150px]" placeholder="客户端 IP" aria-label="客户端 IP" value={clientIp} onChange={(event) => setClientIp(event.target.value)} />
          <Input className="w-[160px]" placeholder="错误代码" aria-label="错误代码" value={errorCode} onChange={(event) => setErrorCode(event.target.value)} />
          <form
            className="flex items-center gap-1"
            onSubmit={(event) => { event.preventDefault(); forceRefreshTable((current) => current + 1); }}
          >
            <Input className="w-[220px]" placeholder="User-Agent" aria-label="User-Agent" value={userAgent} onChange={(event) => setUserAgent(event.target.value)} />
            <Button type="submit" variant="outline" size="icon" aria-label="按 User-Agent 查询">
              <Search className="size-4" aria-hidden="true" />
            </Button>
          </form>
          <Button variant="outline" onClick={() => forceRefreshTable((current) => current + 1)}>刷新</Button>
          <Button variant="outline" disabled={currentPageRecords.length === 0} onClick={exportCurrentPage}>
            <Download className="size-4" aria-hidden="true" />
            导出当前页
          </Button>
      </DataToolbar>
      <ServerDataTable<IRequestHistoryListItem>
        columns={columns}
        getRowId={(row) => String(row.id)}
        searchAction={requestHistorySearch}
        reloadKey={refreshTable}
        filter={externalFilterCondition}
        onPageDataChange={setCurrentPageRecords}
      />

      <Sheet open={detailOpen} onOpenChange={setDetailOpen}>
        <SheetContent className="w-full overflow-y-auto text-sm sm:max-w-[760px]">
          <SheetHeader className="pe-8">
            <SheetTitle>请求诊断详情</SheetTitle>
            <SheetDescription className="sr-only">单条请求的状态、Header、异常与 Body 诊断信息。</SheetDescription>
          </SheetHeader>
          <div className="mt-4">
            {detailViewState === "loading" ? <div className="flex justify-center"><Spin /></div> : null}
            {detailViewState === "missing" ? <Empty description="该请求记录已不存在或已被清理" /> : null}
            {detailViewState === "detail" && detail ? (
              <>
                <Descriptions bordered column={2} items={detailItems} />

                <div className="mt-3">
                  <span className="font-semibold">请求 Header</span>
                  <div className="mt-1">{renderHeaders(detail.requestHeaders)}</div>
                </div>

                <div className="mt-3">
                  <span className="font-semibold">响应 Header</span>
                  <div className="mt-1">{renderHeaders(detail.responseHeaders)}</div>
                </div>

                {detail.exceptionType || detail.exceptionMessage ? (
                  <div className="mt-3">
                    <span className="font-semibold">异常信息</span>
                    <CodeBlock
                      tone="danger"
                      className="mt-1"
                      text={[detail.exceptionType, detail.exceptionMessage, detail.exceptionStackTrace].filter(Boolean).join("\n\n")}
                    />
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
          </div>
        </SheetContent>
      </Sheet>
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
    return <SectionCard title="请求追踪" description={`完整追踪记录已限定为 ${fixedHost}。`}><div className="flex flex-col gap-4">{content}</div></SectionCard>;
  }
  return <WorkbenchPage eyebrow="ANALYTICS / REQUEST TRACE" title="请求追踪" description="定位上游返回、代理异常和请求上下文；选择任意记录查看完整诊断详情。">{content}</WorkbenchPage>;
};

export default RequestHistoryList;
