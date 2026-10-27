export type RequestStatusField = "final" | "upstream";

export interface RequestStatusContext {
  statusCode?: number | null;
  upstreamStatusCode?: number | null;
  statusSource?: string;
  errorCategory?: string;
  errorCode?: string;
}

export interface RequestStatusExplanation {
  fieldLabel: string;
  status: number | null;
  title: string;
  conclusion: string;
  meaning: string;
  impact: string;
  actions: string[];
  sourceLabel: string;
  sourceMeaning: string;
  categoryLabel?: string;
  categoryMeaning?: string;
  errorCode?: string;
  notice?: string;
}

type StatusDetail = {
  label: string;
  meaning: string;
  actions: string[];
};

const httpStatusDetails: Record<number, StatusDetail> = {
  100: { label: "继续", meaning: "服务器已收到请求头，客户端可以继续发送请求体。", actions: ["通常无需处理；若请求长期停留在此阶段，检查客户端上传和代理超时设置。"] },
  101: { label: "切换协议", meaning: "连接将从 HTTP 切换到协商的协议，例如 WebSocket。", actions: ["确认 Upgrade 和 Connection 请求头，以及客户端是否支持目标协议。"] },
  102: { label: "处理中", meaning: "服务器已接收请求，仍在处理，暂未生成最终响应。", actions: ["检查长任务进度和上游处理耗时。"] },
  103: { label: "早期提示", meaning: "服务器在最终响应前提供可预加载资源的提示。", actions: ["通常无需处理；确认客户端不会把它当作最终结果。"] },
  200: { label: "成功", meaning: "请求已成功完成，响应内容由处理该请求的服务返回。", actions: ["若业务结果仍不符合预期，请通过详情核对响应头、响应体和 Trace ID。"] },
  201: { label: "已创建", meaning: "请求成功，并已创建新的资源。", actions: ["确认创建结果、资源标识和幂等策略是否符合预期。"] },
  202: { label: "已接受", meaning: "请求已被接收，但处理可能仍在异步进行。", actions: ["查询异步任务或轮询接口确认最终处理结果。"] },
  203: { label: "非权威信息", meaning: "响应内容由中间层转换或补充，并非上游原始表示。", actions: ["检查中间代理、缓存或内容转换规则。"] },
  204: { label: "无内容", meaning: "请求成功完成，但服务器不返回响应正文。", actions: ["确认调用方不依赖响应体，并检查前端对空响应的处理。"] },
  205: { label: "重置内容", meaning: "请求成功，客户端应重置当前展示或输入状态。", actions: ["确认客户端实现了预期的界面重置行为。"] },
  206: { label: "部分内容", meaning: "服务器仅返回请求的内容范围，常用于断点续传和媒体请求。", actions: ["检查 Range 请求头、Content-Range 响应头和缓存配置。"] },
  207: { label: "多状态", meaning: "一个请求包含多个子操作，每个子操作可能有不同结果。", actions: ["查看响应体中各子操作的独立状态。"] },
  208: { label: "已报告", meaning: "多状态响应中的成员已在前文报告，避免重复传输。", actions: ["通常无需处理；结合完整多状态响应阅读。"] },
  226: { label: "已使用增量", meaning: "服务器返回了基于已有表示计算出的增量结果。", actions: ["确认客户端和缓存层支持增量编码。"] },
  300: { label: "多种选择", meaning: "目标资源有多个可选表示或位置。", actions: ["检查客户端选择逻辑和响应中的候选地址。"] },
  301: { label: "永久重定向", meaning: "资源已永久迁移到新的地址，客户端和缓存可能会记住该跳转。", actions: ["检查 Location 地址、域名迁移和缓存刷新策略。"] },
  302: { label: "临时重定向", meaning: "资源暂时位于其他地址，客户端通常会继续使用原地址。", actions: ["检查 Location 地址以及重定向是否符合登录、灰度或 HTTPS 跳转预期。"] },
  303: { label: "查看其他位置", meaning: "客户端应通过 GET 请求访问响应指定的位置。", actions: ["确认调用方会按约定切换到指定地址和方法。"] },
  304: { label: "未修改", meaning: "客户端缓存仍然有效，服务器没有重新返回响应正文。", actions: ["检查 ETag、Last-Modified 和缓存失效策略。"] },
  305: { label: "使用代理", meaning: "旧的、已废弃的代理指令状态。", actions: ["检查上游或中间设备配置，避免依赖此状态。"] },
  307: { label: "临时重定向", meaning: "资源暂时迁移，客户端必须保持原请求方法和正文。", actions: ["检查 Location 地址及上传、表单等非 GET 请求能否安全重放。"] },
  308: { label: "永久重定向", meaning: "资源永久迁移，客户端必须保持原请求方法和正文。", actions: ["检查 Location 地址、客户端兼容性和非幂等请求的重试风险。"] },
  400: { label: "请求错误", meaning: "服务器无法理解或接受请求，通常是参数、格式或协议不符合要求。", actions: ["核对请求 URL、查询参数、请求体格式和必填请求头。"] },
  401: { label: "未认证", meaning: "访问该资源需要有效认证凭据，或当前凭据无效。", actions: ["检查 Authorization、令牌有效期、签名和认证服务配置。"] },
  402: { label: "需要付款", meaning: "该状态没有统一的通用语义，常被业务系统用于配额或计费限制。", actions: ["查看上游 API 文档和响应体中的业务错误说明。"] },
  403: { label: "禁止访问", meaning: "服务器理解请求，但当前身份或来源没有访问权限。", actions: ["检查账号权限、来源 IP、网关/站点黑名单、CORS 和资源访问策略。"] },
  404: { label: "未找到", meaning: "请求的路由、资源或匹配的网站不存在。", actions: ["核对 Host、Path、路由规则和上游服务是否已部署。"] },
  405: { label: "方法不允许", meaning: "资源存在，但不接受当前 HTTP 方法。", actions: ["检查 GET、POST 等请求方法，以及响应 Allow 请求头。"] },
  406: { label: "不可接受", meaning: "服务器无法根据 Accept 等内容协商请求头返回可接受的表示。", actions: ["检查 Accept、Accept-Language 和内容协商配置。"] },
  407: { label: "需要代理认证", meaning: "客户端需要向代理提供认证信息。", actions: ["检查正向代理凭据和 Proxy-Authorization 请求头。"] },
  408: { label: "请求超时", meaning: "服务器在等待客户端请求时超时。", actions: ["检查客户端网络、上传速度、请求体大小和服务端读取超时。"] },
  409: { label: "冲突", meaning: "请求与资源当前状态冲突，例如版本冲突或重复创建。", actions: ["检查资源版本、幂等键和并发更新策略。"] },
  410: { label: "已删除", meaning: "资源已永久不可用，不应继续按原地址请求。", actions: ["更新调用地址或移除过期资源引用。"] },
  411: { label: "需要 Content-Length", meaning: "服务器要求请求明确提供 Content-Length。", actions: ["检查客户端是否使用服务端支持的传输方式。"] },
  412: { label: "前置条件失败", meaning: "If-Match、If-Unmodified-Since 等条件不满足。", actions: ["刷新资源版本后重试，并检查条件请求头。"] },
  413: { label: "内容过大", meaning: "请求正文超过服务器或代理允许的大小。", actions: ["减小请求体，或检查上传、代理和上游的大小限制。"] },
  414: { label: "URI 过长", meaning: "请求 URL 或查询字符串超过服务器允许的长度。", actions: ["将过大的参数移入请求体，或检查网关和上游限制。"] },
  415: { label: "不支持的媒体类型", meaning: "请求 Content-Type 不被当前接口支持。", actions: ["检查 Content-Type、字符集和请求体编码。"] },
  416: { label: "范围不满足", meaning: "请求的字节范围不在资源可用范围内。", actions: ["检查 Range 请求头和资源实际大小。"] },
  417: { label: "期望失败", meaning: "服务器无法满足请求中的 Expect 条件。", actions: ["检查 Expect 请求头，必要时由客户端移除该头。"] },
  418: { label: "我是茶壶", meaning: "非标准的玩笑状态，有时被业务系统用于自定义拒绝。", actions: ["查看上游 API 文档和响应体，确认其业务含义。"] },
  421: { label: "错误定向请求", meaning: "当前连接对应的服务器无法处理该请求的目标域名。", actions: ["检查 TLS SNI、Host、HTTP/2 连接复用和反向代理路由。"] },
  422: { label: "无法处理的内容", meaning: "请求格式正确，但业务语义或字段校验未通过。", actions: ["查看响应体中的字段校验错误并修正输入。"] },
  423: { label: "已锁定", meaning: "目标资源正被锁定，暂时不能修改。", actions: ["等待锁释放或检查协作、文件和资源锁策略。"] },
  424: { label: "依赖失败", meaning: "当前操作依赖的前置操作失败。", actions: ["先定位并修复失败的依赖操作。"] },
  425: { label: "过早请求", meaning: "服务器拒绝处理可能被重放的早期数据请求。", actions: ["检查 TLS 早期数据和请求幂等性策略。"] },
  426: { label: "需要升级", meaning: "服务器要求客户端使用其他协议版本。", actions: ["查看 Upgrade 响应头并更新客户端协议支持。"] },
  428: { label: "需要前置条件", meaning: "服务器要求请求携带并发控制等条件。", actions: ["使用 ETag、If-Match 等条件请求头后重试。"] },
  429: { label: "请求过多", meaning: "请求触发了限流或配额保护。", actions: ["降低并发和重试频率，遵循 Retry-After，并检查调用配额。"] },
  431: { label: "请求头过大", meaning: "请求头字段或总大小超过限制。", actions: ["精简 Cookie、自定义头和追踪信息，检查代理头部限制。"] },
  451: { label: "因法律原因不可用", meaning: "资源因法律、合规或地区限制而不可访问。", actions: ["查看响应说明并联系资源所有者确认限制原因。"] },
  500: { label: "内部服务器错误", meaning: "服务在处理请求时发生未处理的内部错误。", actions: ["查看 Trace ID、服务日志和异常信息，定位上游应用故障。"] },
  501: { label: "未实现", meaning: "服务器不支持当前请求所需的功能或方法。", actions: ["检查调用的 HTTP 方法、协议能力和上游版本。"] },
  502: { label: "网关错误", meaning: "网关从上游收到无效响应，或无法完成有效的上游交互。", actions: ["检查上游健康、DNS、网络连通性、TLS 和代理配置。"] },
  503: { label: "服务不可用", meaning: "服务暂时无法处理请求，常见于过载、维护或无可用实例。", actions: ["检查服务健康、实例容量、部署状态和 Retry-After。"] },
  504: { label: "网关超时", meaning: "网关等待上游响应超时。", actions: ["检查上游延迟、超时阈值、数据库依赖和网络状况。"] },
  505: { label: "HTTP 版本不支持", meaning: "服务器不支持请求使用的 HTTP 协议版本。", actions: ["检查客户端协议协商和代理协议配置。"] },
  506: { label: "协商变体错误", meaning: "服务器的内容协商配置形成了循环或无法选择表示。", actions: ["检查上游内容协商和变体配置。"] },
  507: { label: "存储空间不足", meaning: "服务器无法完成请求，因为可用存储资源不足。", actions: ["检查上游磁盘、对象存储和配额。"] },
  508: { label: "检测到循环", meaning: "服务器在处理请求时检测到无限循环或依赖环。", actions: ["检查代理跳转、重写规则和上游依赖链。"] },
  510: { label: "未扩展", meaning: "服务器要求请求提供额外扩展条件。", actions: ["查看上游 API 文档和响应体说明。"] },
  511: { label: "需要网络认证", meaning: "客户端需要先完成网络层认证，例如认证门户。", actions: ["检查网络准入、认证门户和出口网络策略。"] }
};

const reservedStatusDetails: Record<number, StatusDetail> = {
  601: { label: "客户端已取消", meaning: "客户端在代理完成前取消了请求，可能是浏览器离开页面、调用方超时、连接中断或升级连接取消。", actions: ["若偶发出现，通常无需修复上游。", "若比例升高，检查客户端超时、用户取消行为和上游响应速度。"] },
  602: { label: "上游超时", meaning: "Yaginx 等待上游请求、响应或升级活动超过了允许时间。", actions: ["检查上游实例健康、响应耗时和依赖服务。", "核对代理与上游的超时设置是否与业务 SLA 匹配。"] },
  603: { label: "无可用上游", meaning: "Yaginx 没有可用于转发当前请求的有效目标地址。", actions: ["检查网站或子规则的目标地址配置。", "确认目标 URL 格式正确，且当前路由确实匹配该配置。"] },
  604: { label: "上游请求失败", meaning: "Yaginx 在创建或发送上游请求时失败，例如 DNS、连接、TLS 或连接被拒绝。", actions: ["检查上游域名解析、端口连通性、TLS 证书和防火墙。", "查看详情中的错误代码、Forwarder Error 和 Trace ID。"] },
  605: { label: "上游响应头失败", meaning: "Yaginx 在读取上游响应头之前发生失败，未能获得完整的上游响应。", actions: ["检查上游进程是否提前断开、协议是否正确以及网络稳定性。", "结合上游日志和 Forwarder Error 排查连接关闭原因。"] },
  606: { label: "上游响应体失败", meaning: "上游已经开始响应，但在读取响应体或升级响应过程中发生失败。", actions: ["检查上游流式响应、连接稳定性和响应体传输。", "注意客户端可能已收到部分内容，应避免直接重试非幂等请求。"] },
  607: { label: "上游请求体失败", meaning: "读取客户端请求体或向上游发送请求体时发生失败，常见于上传中断或连接异常。", actions: ["检查客户端上传连接、请求体大小和上游接收能力。", "确认上传类请求的重试策略不会重复写入。"] },
  608: { label: "向客户端写入失败", meaning: "响应已经开始发送后，Yaginx 无法继续向客户端写入数据，通常是客户端断开或网络中断。", actions: ["检查客户端网络、长连接和下载中断情况。", "上游通常不是首要排查对象；结合客户端日志判断是否需要重试。"] },
  609: { label: "代理管线异常", meaning: "Yaginx 本地代理管线出现明确的配置、状态或运行时异常。", actions: ["检查网站、转发代理和路由配置。", "通过详情中的异常信息、Trace ID 和服务日志定位本地异常。"] },
  610: { label: "未分类代理异常", meaning: "Yaginx 记录了无法归入其他类别的代理错误，或请求未能产生有效状态。", actions: ["优先查看详情中的错误代码、异常信息和 Trace ID。", "结合网关与上游日志确认网络、协议或运行时异常。"] }
};

const sourceDetails: Record<string, { label: string; meaning: string }> = {
  Upstream: { label: "上游返回", meaning: "状态由上游服务生成，Yaginx 已获得该上游响应。" },
  YaginxProxy: { label: "Yaginx 代理", meaning: "状态是 Yaginx 在转发上游请求或传输响应时记录的诊断结果。" },
  YaginxPipeline: { label: "Yaginx 本地", meaning: "状态由 Yaginx 的本地请求处理流程生成，例如路由、重定向、简单响应或本地异常。" },
  ClientCanceled: { label: "客户端取消", meaning: "请求在客户端一侧被取消或连接中断，Yaginx 因此记录了该诊断结果。" },
  CorsPreflight: { label: "CORS 预检", meaning: "这是浏览器发送的跨域预检请求，由 Yaginx 在本地处理。" },
  Unknown: { label: "未知", meaning: "系统无法可靠确定状态来源，需要结合详情和 Trace ID 进一步判断。" }
};

const categoryDetails: Record<string, { label: string; meaning: string }> = {
  UpstreamReturnedError: { label: "上游原始错误", meaning: "上游服务明确返回了错误状态，Yaginx 仅记录并转发该结果。" },
  ClientCanceled: { label: "客户端取消", meaning: "调用方主动取消、超时或连接中断导致请求未完成。" },
  UpstreamTimeout: { label: "上游超时", meaning: "等待上游处理或响应超过了允许时间。" },
  NoAvailableDestination: { label: "无可用上游", meaning: "当前请求没有匹配到可用的上游目标。" },
  UpstreamRequestFailure: { label: "上游请求失败", meaning: "创建或发送上游请求时失败。" },
  UpstreamResponseHeaderFailure: { label: "上游响应头失败", meaning: "在取得完整上游响应头前发生失败。" },
  UpstreamResponseBodyFailure: { label: "上游响应体失败", meaning: "上游响应开始后，读取正文或升级响应时发生失败。" },
  UpstreamRequestBodyFailure: { label: "上游请求体失败", meaning: "读取客户端请求体或向上游发送请求体时发生失败。" },
  ClientWriteFailure: { label: "向客户端写失败", meaning: "向客户端传输响应时连接中断或写入失败。" },
  ProxyPipelineException: { label: "Yaginx 管线异常", meaning: "Yaginx 本地处理流程遇到配置或运行时异常。" },
  UnknownProxyError: { label: "未知代理异常", meaning: "代理错误无法归类到更具体的稳定类别。" },
  Blacklist: { label: "网关黑名单", meaning: "请求命中了 Yaginx 配置的网关级 IP 黑名单并被本地拒绝。" },
  SiteIPBlacklist: { label: "站点黑名单", meaning: "请求命中了当前站点配置的 IP 黑名单并被本地拒绝。" },
  SiteIPWhitelist: { label: "站点白名单", meaning: "请求未通过当前站点配置的 IP 白名单。" },
  SiteMaintenance: { label: "站点维护中", meaning: "站点已由管理员切换至维护模式，Yaginx 在访问上游前固定返回 503。" }
};

export const statusSourceLabel = (source?: string) => sourceDetails[source ?? ""]?.label ?? source ?? "未记录";
export const errorCategoryLabel = (category?: string) => categoryDetails[category ?? ""]?.label ?? category ?? "未记录";

const statusFamilyDetail = (status: number): StatusDetail => {
  if (status >= 100 && status < 200) {
    return { label: "信息响应", meaning: "这是请求处理过程中的临时信息，通常不会作为最终业务结果。", actions: ["确认客户端继续等待最终响应，不要把此状态当作请求完成。"] };
  }
  if (status >= 200 && status < 300) {
    return { label: "成功", meaning: "请求被处理为成功结果。", actions: ["若业务内容异常，请通过详情核对响应内容和追踪信息。"] };
  }
  if (status >= 300 && status < 400) {
    return { label: "重定向", meaning: "客户端需要根据响应中的跳转或缓存语义采取后续动作。", actions: ["检查 Location、缓存策略和客户端重定向行为。"] };
  }
  if (status >= 400 && status < 500) {
    return { label: "客户端请求问题", meaning: "请求、身份、权限或目标资源不符合服务要求。", actions: ["检查请求参数、认证、权限和目标地址。"] };
  }
  if (status >= 500 && status < 600) {
    return { label: "服务端错误", meaning: "处理请求的服务或网关未能成功完成请求。", actions: ["检查服务健康、错误日志、依赖服务和 Trace ID。"] };
  }
  return { label: "非标准状态", meaning: "该数值不在标准 HTTP 状态码范围内，系统不会猜测其具体业务含义。", actions: ["查看本行详情、上游文档和 Trace ID，确认该自定义状态的定义。"] };
};

const detailForStatus = (status: number) => httpStatusDetails[status] ?? statusFamilyDetail(status);

const sourceImpact = (status: number, source?: string) => {
  if (source === "Upstream") {
    if (status >= 500) {
      return "请求已到达上游，但上游未能成功处理。影响通常由上游服务、其依赖或容量问题造成。";
    }
    if (status >= 400) {
      return "请求已到达上游。该结果不一定表示网关故障，优先确认调用参数、身份和上游业务规则。";
    }
    return "Yaginx 已获得上游响应；如业务结果不符合预期，应进一步检查响应内容而非仅看状态码。";
  }
  if (source === "YaginxPipeline") {
    return "结果由 Yaginx 本地流程产生，上游可能没有参与本次请求；应优先检查路由、重定向、简单响应或本地策略。";
  }
  if (source === "ClientCanceled") {
    return "请求未必代表上游故障。客户端可能已提前放弃等待，用户实际体验取决于客户端超时和重试策略。";
  }
  if (source === "CorsPreflight") {
    return "该结果影响浏览器跨域调用是否会继续发送实际业务请求。";
  }
  return "仅凭状态码无法确定完整影响范围；请结合状态来源、错误分类和请求详情判断。";
};

const finalConclusion = (status: number, source?: string) => {
  const detail = detailForStatus(status);
  switch (source) {
    case "Upstream":
      return `本次请求最终记录为 ${status}（${detail.label}），该结果由上游服务返回。`;
    case "YaginxPipeline":
      return `本次请求最终记录为 ${status}（${detail.label}），该结果由 Yaginx 本地流程生成。`;
    case "CorsPreflight":
      return `这是浏览器跨域预检的 ${status}（${detail.label}）结果，由 Yaginx 本地处理。`;
    case "ClientCanceled":
      return `请求结束时记录为 ${status}（${detail.label}），但关键原因是客户端已取消或断开。`;
    case "YaginxProxy":
      return `本次请求最终记录为 ${status}（${detail.label}），Yaginx 在代理转发过程中记录了该结果。`;
    default:
      return `本次请求最终记录为 ${status}（${detail.label}）。`;
  }
};

const missingUpstreamExplanation = (context: RequestStatusContext): RequestStatusExplanation => {
  const source = context.statusSource;
  const sourceDetail = sourceDetails[source ?? ""];
  let conclusion = "本次请求没有记录到可用的上游状态码。";
  let impact = "这并不等同于上游返回成功或失败，只表示历史记录中没有可确认的上游响应状态。";
  let actions = ["结合最终状态、状态来源和错误分类判断请求在哪个阶段结束。", "如需精确定位，请打开本行详情查看 Trace ID、异常信息和 Forwarder Error。"];

  if (source === "YaginxProxy") {
    conclusion = "Yaginx 在取得上游响应前结束了代理流程，因此没有可显示的上游状态码。";
    impact = "通常表示请求在连接、发送、等待响应头或传输过程中失败。";
    actions = ["优先检查最终状态对应的代理错误说明。", "检查上游连通性、超时、TLS、DNS 和 Forwarder Error。"];
  } else if (source === "YaginxPipeline" || source === "CorsPreflight") {
    conclusion = "该请求由 Yaginx 本地流程处理，未必会访问上游，因此没有上游状态码。";
    impact = "上游服务通常不是首要排查对象。";
    actions = ["检查路由、简单响应、重定向、CORS 或本地安全策略。", "若状态为 403，进一步检查网关/站点黑名单和访问控制配置。"];
  } else if (source === "ClientCanceled") {
    conclusion = "客户端已取消或断开，请求结束前未记录到可用的上游状态码。";
    impact = "这通常反映客户端等待或网络中断，不一定表示上游不可用。";
    actions = ["检查客户端超时和网络情况。", "若大量出现，再排查上游响应延迟。"];
  }

  return {
    fieldLabel: "上游状态",
    status: null,
    title: "上游状态未获取",
    conclusion,
    meaning: "上游状态仅在 Yaginx 获得可确认的上游响应后才会记录。",
    impact,
    actions,
    sourceLabel: statusSourceLabel(source),
    sourceMeaning: sourceDetail?.meaning ?? "未记录状态来源。",
    categoryLabel: context.errorCategory ? errorCategoryLabel(context.errorCategory) : undefined,
    categoryMeaning: categoryDetails[context.errorCategory ?? ""]?.meaning,
    errorCode: context.errorCode || undefined
  };
};

export const explainRequestStatus = (
  context: RequestStatusContext,
  field: RequestStatusField
): RequestStatusExplanation => {
  const status = field === "final" ? context.statusCode ?? null : context.upstreamStatusCode ?? null;
  if (field === "upstream" && status === null) {
    return missingUpstreamExplanation(context);
  }

  const safeStatus = status ?? 0;
  const source = context.statusSource;
  const sourceDetail = sourceDetails[source ?? ""];
  const category = context.errorCategory;
  const categoryDetail = categoryDetails[category ?? ""];
  const finalReservedStatus = field === "final" ? reservedStatusDetails[safeStatus] : undefined;

  if (field === "upstream" && reservedStatusDetails[safeStatus]) {
    return {
      fieldLabel: "上游状态",
      status: safeStatus,
      title: `上游状态 ${safeStatus}：上游自定义状态`,
      conclusion: `上游服务返回了 ${safeStatus}。虽然该数值位于 Yaginx 的保留诊断区间，但它出现在“上游状态”列，不能按 Yaginx 代理错误解释。`,
      meaning: "标准 HTTP 未定义 601–610。这里保留的是上游服务实际返回的自定义数值，需以该上游服务的接口约定为准。",
      impact: "客户端最终收到的结果可能与此值相同，也可能在 Yaginx 后续处理或传输中发生变化；请同时查看最终状态。",
      actions: ["查阅上游服务对该自定义状态的定义。", "对照最终状态、错误分类和详情中的响应内容判断是否存在后续代理错误。"],
      sourceLabel: statusSourceLabel(source),
      sourceMeaning: sourceDetail?.meaning ?? "未记录状态来源。",
      categoryLabel: category ? errorCategoryLabel(category) : undefined,
      categoryMeaning: categoryDetail?.meaning,
      errorCode: context.errorCode || undefined,
      notice: "601–610 仅在“最终状态”列中才表示 Yaginx 的内部诊断状态。"
    };
  }

  const maintenanceDetail: StatusDetail = {
    label: "站点维护中",
    meaning: "站点处于维护模式，Yaginx 主动暂停业务访问并返回 503。",
    actions: ["确认维护窗口和恢复计划。", "维护完成后关闭站点维护模式，恢复正常转发。"]
  };
  const detail = finalReservedStatus ?? (field === "final" && category === "SiteMaintenance" ? maintenanceDetail : detailForStatus(safeStatus));
  const isReserved = Boolean(finalReservedStatus);
  const conclusion = field === "final"
    ? (isReserved ? `Yaginx 记录了 ${safeStatus}（${detail.label}）诊断状态。` : category === "SiteMaintenance" ? "站点已进入维护模式，Yaginx 在访问上游前主动返回 503。" : finalConclusion(safeStatus, source))
    : `上游服务返回了 ${safeStatus}（${detail.label}），Yaginx 已记录该上游响应状态。`;
  const impact = isReserved
    ? sourceImpact(safeStatus, source)
    : field === "upstream"
      ? "这是上游服务的响应结果。若最终状态不同，说明 Yaginx 在后续处理或向客户端传输时还记录了额外诊断信息。"
      : category === "SiteMaintenance"
        ? "请求未进入静态响应、路由规则或上游服务；这是预期的维护行为，不应按上游故障处理。"
      : sourceImpact(safeStatus, source);

  return {
    fieldLabel: field === "final" ? "最终状态" : "上游状态",
    status: safeStatus,
    title: `${field === "final" ? "最终状态" : "上游状态"} ${safeStatus}：${detail.label}`,
    conclusion,
    meaning: detail.meaning,
    impact,
    actions: detail.actions,
    sourceLabel: statusSourceLabel(source),
    sourceMeaning: sourceDetail?.meaning ?? "未记录状态来源。",
    categoryLabel: category ? errorCategoryLabel(category) : undefined,
    categoryMeaning: categoryDetail?.meaning,
    errorCode: context.errorCode || undefined,
    notice: isReserved
      ? "这是 Yaginx 用于请求历史诊断的保留状态，不是标准 HTTP 状态码。若响应已开始，客户端实际收到的 HTTP 状态可能不同。"
      : undefined
  };
};
