# 代理兼容矩阵

本文是 Go 数据面实现和合同测试的直接输入。行为来源为当前 `DynamicProxyRuleProvider`、`SimpleProcessorRuntimeLookup`、反向代理中间件、`UpstreamExpectContinuePolicy` 和请求历史状态解析器。除“首版固定差异”表列出的项目外，Go 输出必须与当前 .NET/YARP 2.3 基线一致。

## 监听器合同

| 端口 | 传输 | 入站协议 | PROXY Protocol | TLS | 首版合同 |
| --- | --- | --- | --- | --- | --- |
| 8080 | TCP | HTTP/1.1 | 否 | 否 | `ports.http`；兼容 `YAGINX_HTTP_PORT` |
| 9080 | TCP | HTTP/1.1 | v1/v2，必须有 header | 否 | `ports.proxyHttp`；兼容 `YAGINX_PROXY_HTTP_PORT` |
| 8443 | TCP + UDP | TCP 为 HTTP/1.1/2，UDP 为 HTTP/3 | 否 | 动态 SNI | `ports.https`；HTTP/3 自动共用地址与端口，兼容 `YAGINX_HTTPS_PORT` |
| 9443 | TCP | HTTP/1.1/2 | v1/v2，必须有 header | 动态 SNI | `ports.proxyHttps`；兼容 `YAGINX_PROXY_HTTPS_PORT`，不在 UDP 上监听 |
| 9090 | TCP | HTTP/1.1 | 否 | 否 | `ports.management`；兼容 `YAGINX_MANAGEMENT_PORT` |

五个 TCP listener 共享本节点 `ports.bindAddress`，默认 `0.0.0.0`；管理路径受本节点 `management.allowedCIDRs` 限制，默认双栈全开放。管理访问策略属于节点拓扑配置，不进入 PostgreSQL shared runtime。`YAGINX_MANAGEMENT_ADDRESS` 已移除，不再覆盖绑定地址。

PROXY Protocol 默认可信 CIDR 为 `0.0.0.0/0` 和 `::/0`，用于兼容当前全来源行为。`doctor` 在非开发环境发现全来源配置时返回 warning。解析失败、超时或不可信来源发送 header 时关闭连接，不把 header 字节交给 HTTP 解析器。TLS offload TLV 只用于记录原始连接信息，不跳过 9443 的本地 TLS 握手。

## 请求处理优先级

| 优先级 | 匹配 | 动作 |
| --- | --- | --- |
| 1 | 独立管理 listener 上的 AdminUI、`/yaginx_console/api/`、`/health/live`、`/health/ready`、`/metrics` | 由管理 listener 处理，不进入业务路由；业务 listener 的 `/yaginx_console` 返回 404 |
| 2 | 管理 listener 上的 `/yaginx_console/api/` | 管理 API；任何 API 路径都不得进入 SPA fallback |
| 3 | 管理 listener 上的 `/yaginx_console/` | React 静态资源或 `index.html` fallback |
| 4 | `/.well-known/acme-challenge/{token}` | 查询 `configurable.example.invalid`；有值直接响应，无值继续以下业务匹配 |
| 5 | Host + Path 命中且站点未开启维护模式的 `simple_responses` | 直接响应，不调用上游 |
| 6 | HTTP 请求命中站点 `isAutoRedirectHttp2Https` | 302 跳转到站点主域名 HTTPS |
| 7 | Host + 子规则 Path 匹配 | 使用子规则 destination 和 transport |
| 8 | Host 命中站点默认路由 | 使用默认 destination 和 transforms |
| 9 | 无匹配 | 404，空响应体 |

同一优先级发生冲突时，候选快照构建失败并继续使用 last-known-good，不依赖数据库行顺序。Host 冲突包括 IDNA ASCII、小写并去尾点后的相同域名。站点内部重复子规则以规范化后的 `pathPattern` 为键；重复键同样拒绝快照。

## Host 与路径

| 配置/输入 | 当前语义 | Go 固定实现 | 合同 fixture |
| --- | --- | --- | --- |
| `defaultHost` | 站点主 Host，加入默认和子规则路由 | IDNA ASCII、小写、去尾点；端口不参与站点匹配 | ASCII、中文域名、大小写、尾点、带端口 |
| `hosts[]` | 默认 Host 的别名集合 | 与主 Host 同样规范化、去重；空字符串忽略 | 重复别名、空值 |
| `proxyRules[].pathPattern` | 去首尾 `/` 后生成 `/{pattern}/{**remainder}` | 分段前缀匹配，`/api` 命中 `/api`、`/api/`、`/api/x`，不命中 `/apix`；不区分大小写 | 根、尾斜杠、相似前缀 |
| `proxyRules[].isRegexMatchPattern` | 当前 provider 未读取该字段 | 首版继续忽略，不启用正则；值为 true 也按 path 分段前缀处理 | true/false 对照 |
| simple response `url` | 补前导 `/` 后按 Host + Path 精确匹配，不区分大小写 | 保持；不包含 query string，不做前缀或模板匹配 | 大小写、query、尾斜杠 |
| query string | 默认原样转发 | 保留原始键值顺序、重复键和 percent encoding | 重复参数、`+`、`%2F` |
| `/_next/static/.../@scope/...` | 目标 URI 中 scoped package 的 `@` 改为 `%40`，禁用二次 canonicalization | 保持当前正则限定和原始 query | 已编码与未编码 `@` |

## 站点默认路由

| 字段 | 输入规则 | 上游行为 | 无效配置 |
| --- | --- | --- | --- |
| `defaultDestination` | 必须是绝对 `http` 或 `https` URL | 作为上游前缀，保留入站 path/query | 拒绝候选快照 |
| `defaultDestinationHost` | 空或合法 Host/authority | 非空时覆盖上游 `Host` | 控制字符或非法 authority 拒绝快照 |
| `isWithOriginalHostHeader` | boolean | true 保留客户端 Host；false 使用 destination URL host，之后仍允许 `defaultDestinationHost` 覆盖 | 无 |
| `webProxy` | 空或绝对 `http` URL | 非空时该站点使用显式 forward proxy | 非法 URL 拒绝快照 |
| `isAllowUnsafeSslCertificate` | boolean | true 跳过该站点上游 TLS 证书链和主机名验证 | 仅影响上游，不影响客户端 TLS |
| `authorizationPolicy` | 空或字符串 | 空值和 `Anonymous` 均匿名；其他历史值只从 golden fixture 接受 | 未出现在冻结 fixture 的非空值拒绝快照 |
| `isAllowAnySourceCors` | 当前代理中间件未按字段开关，所有代理路由均执行宽松 CORS | 首版保留实际行为，字段不改变结果 | true/false 对照结果相同 |

默认 transport：连接池按 destination scheme/authority、`webProxy`、TLS 跳过验证和 header encoding 组合隔离；请求和响应 header 使用 UTF-8；活动超时 15 分钟；不设置 `Server` 响应头。

## 子规则

| 字段 | 固定行为 |
| --- | --- |
| `ruleId` | 新增或非正数时由 Snowflake 生成；只用于稳定身份，不决定优先级 |
| `isEnabled` | false 时不生成路由 |
| `destination` | 非空时必须是绝对 `http`/`https` URL |
| `destinationHost` | `destination` 为空且该值是绝对 URL 时，按旧兼容逻辑把它当 destination，并且不覆盖 Host；否则作为上游 Host 覆盖值 |
| `removePrefix` | 在路由命中后按 path segment 前缀删除一次；不匹配时该路由视为配置错误并拒绝快照 |
| `webProxy` | 与默认路由相同，但仅作用于当前规则 transport |
| `isAllowUnsafeSslCertificate` | 与默认路由相同，但仅作用于当前规则 transport |
| 原始 Host | 子规则固定不保留原始 Host，除非 `destinationHost` 显式覆盖 |
| `proxyTransforms` | 子规则不应用站点级 `proxyTransforms`，保持当前 provider 行为 |

一个请求命中多个子规则时，选择 path segment 数最多者；segment 数相同选择规范化字符串长度最长者；仍相同时快照构建失败。默认路由只在没有子规则命中时使用。

## `proxy_transforms` 合同

数据库值是一个 JSON object，管理 API 表示为 `{key,value}` 数组；它描述一个 YARP transform 配置对象，不是 transform 数组。Go 首版实现以下固定语法，键名不区分大小写，重复键由 API 在入库前返回 HTTP 500，直接 SQL 产生的重复 JSON key 以 PostgreSQL JSONB 最终值为准。

| 主键 | 同对象参数 | 行为 |
| --- | --- | --- |
| `PathPrefix` | 无 | 在当前 path 前加指定 segment 前缀 |
| `PathRemovePrefix` | 无 | 删除匹配的 path segment 前缀 |
| `PathSet` | 无 | 用指定绝对 path 替换当前 path |
| `PathPattern` | 路由值参数 | 按 YARP 2.3 route-value pattern 生成 path；缺少 route value 拒绝快照 |
| `QueryValueParameter` | `Set`、`Append`、`Remove` 之一 | 设置、追加或删除固定名称的 query 参数 |
| `QueryRouteParameter` | `Set` 或 `Append` | 把路由值写入 query 参数 |
| `RequestHeader` | `Set`、`Append`、`Remove` 之一 | 修改上游请求头；Hop-by-hop header 禁止配置 |
| `RequestHeadersCopy` | `true` 或 `false` | 控制普通请求头复制，Hop-by-hop header 始终过滤 |
| `RequestHeaderOriginalHost` | `true` 或 `false` | 控制上游 Host；`defaultDestinationHost` 在其后覆盖 |
| `X-Forwarded` | `For`、`Host`、`Proto`、`Prefix` 的 `Set`/`Append`/`Remove` 参数 | 使用 YARP 2.3 值格式生成 forwarded headers |
| `Forwarded` | `For`、`By`、`Host`、`Proto` 参数 | 生成 RFC 7239 `Forwarded` |
| `ResponseHeader` | `Set`、`Append`、`Remove` 之一，允许 `When` | 在响应头提交前修改 |
| `ResponseHeadersCopy` | `true` 或 `false` | 控制普通上游响应头复制 |
| `ResponseTrailer` | `Set`、`Append`、`Remove` 之一，允许 `When` | 修改 HTTP trailer |
| `ResponseTrailersCopy` | `true` 或 `false` | 控制上游 trailer 复制 |

未列出的主键、参数组合、空 key、控制字符、CR/LF 注入、修改 `Connection`/`Upgrade`/`Keep-Alive`/`Proxy-Connection`/`Transfer-Encoding`/`TE` 或与自动 Host 规则冲突的配置，都使候选快照失败。转换执行顺序固定为 path、query、普通请求头、Host、forwarded、响应头、trailer。

## Forwarded headers

| 路由 | `X-Forwarded-For` | `X-Forwarded-Host` | `X-Forwarded-Proto` | `X-Forwarded-Prefix` |
| --- | --- | --- | --- | --- |
| 默认路由 | 追加有效客户端 IP | 设置为入站 Host | 设置为入站 scheme；可信 PROXY TLS TLV 不覆盖本地实际 scheme | 设置为 PathBase，通常为空 |
| 子规则 | 使用 YARP 2.3 默认 Set 行为 | Set | Set | Set |

只有受信 PROXY listener 可以从 PROXY header 改写有效客户端 IP。为兼容当前黑名单，业务请求上已有 `X-Forwarded-For` 时取第一个可解析 IP 作为黑名单候选；该行为必须记录安全 warning，并由部署文档要求在外层负载均衡清除客户端自带 header。

## CORS 与简单响应

| 场景 | 固定响应 |
| --- | --- |
| 代理请求带 `Origin` | 回显 `Access-Control-Allow-Origin`，并设置 `Access-Control-Allow-Credentials: true` |
| 带 `Access-Control-Request-Headers` | 原值回显到 `Access-Control-Allow-Headers` |
| 带 `Access-Control-Request-Method` | 原值回显到 `Access-Control-Allow-Methods` |
| 任意代理 `OPTIONS` | 返回 200，不访问上游；仍按以上规则设置 CORS |
| simple response | 使用配置的 status、content type、body 和 extra headers；不自动增加代理 CORS header |
| simple response body | UTF-8 写出 `responseContent`；.NET golden 使用 chunked 传输，不发送 `Content-Length` |
| simple response extra header 重复 | 后出现值覆盖前值；禁止 CR/LF 和 hop-by-hop header |

## HTTP 到 HTTPS 跳转

- 只有站点 `isAutoRedirectHttp2Https=true` 时生成跳转匹配。
- 入站 scheme 已是 HTTPS 且 Host 已是主域名时不跳转，继续代理。
- 其他命中别名或 HTTP 的请求返回 302，`Location=https://<defaultHost><encoded-path-and-query>`。
- `defaultHost` 使用规范化 ASCII，不附带入站端口；path/query 保持原始编码。
- simple response 优先于跳转，因此同一 Host + Path 的 simple response 在 HTTP 上直接返回。

## `Expect: 100-continue`

仅当以下条件全部满足时，Go 主动给上游请求设置 `Expect: 100-continue`：

1. 功能启用。
2. 方法为 POST、PUT 或 PATCH。
3. `Content-Length` 存在且严格大于 `minContentLengthBytes`；chunked 且未知长度不主动添加。
4. Host 与 `includedHosts` 任一项不区分大小写相等；配置可写绝对 URL，按其 authority 比较。
5. Path 与任一规范化前缀按 segment 匹配。
6. 请求不是 WebSocket 或其他 `Connection: upgrade` 请求。

客户端原有 `Expect` 按 Go `net/http` 合法性规则转发；策略未命中时不得自行新增。上游返回 100 后继续流式发送 body；上游在 body 前返回最终状态时停止读取客户端 body 并按连接可复用规则处理。

## 长连接与流式传输

| 能力 | 合同 |
| --- | --- |
| WebSocket | 保留 101、双向帧、子协议和关闭码；配置重载不终止现有连接 |
| SSE | 收到上游数据后立即 flush，不做整响应缓冲或压缩改写 |
| 大文件上传/下载 | 流式转发；请求体上限 0 表示无限；背压由 TCP/QUIC 和 `io.Copy` 链路传播 |
| chunked | HTTP/1.1 入站和上游均允许；不得把未知长度请求预读为内存 buffer |
| trailers | 只转发协议允许且已声明的 trailer；HTTP/2/3 与 HTTP/1.1 语义转换由标准库处理 |
| 客户端取消 | 取消上游请求；记录 601，响应未开始时可写 601，已开始时只记录最终观测状态 |
| 配置重载 | 每个请求和升级连接持有进入时的不可变快照与 transport 引用，transport 在引用归零后关闭 idle connections |

## TLS 与证书选择

- SNI 先执行 IDNA ASCII、小写和去尾点；精确 SAN 优先于单标签 wildcard。
- wildcard 只匹配一个标签，例如 `*.example.com` 匹配 `a.example.com`，不匹配 `a.b.example.com` 或 `example.com`。
- 多张有效证书匹配时依次选择：当前 active、`not_before` 已生效、`not_after` 最晚、创建时间最新、ID 最大。
- 没有匹配证书时 TLS handshake 失败并记录 `tls_certificate_not_found`；不使用任意站点证书兜底。
- 8443 的 TCP TLS 与 UDP QUIC 使用同一证书快照。9443 解析 PROXY header 后再进行 TLS handshake。

## 代理状态与错误分类

| 最终状态 | `statusSource` | `errorCategory` | 触发 |
| --- | --- | --- | --- |
| 上游原始状态 | `Upstream` | 4xx/5xx 为 `UpstreamReturnedError`，其他为空 | 已完成代理且无转发错误 |
| 601 | `ClientCanceled` | `ClientCanceled` | 请求取消、请求/响应 client side cancel、upgrade client cancel；分类保留，但按生产 history 策略抑制落库 |
| 602 | `YaginxProxy` | `UpstreamTimeout` | 上游请求或 upgrade activity 超时 |
| 603 | `YaginxProxy` | `NoAvailableDestination` | 无可用 destination |
| 604 | `YaginxProxy` | `UpstreamRequestFailure` | 创建或发送上游请求失败 |
| 605 | `YaginxProxy` | `UpstreamResponseHeaderFailure` | 读取上游响应头失败 |
| 606 | `YaginxProxy` | `UpstreamResponseBodyFailure` | 读取上游响应体或 upgrade response 失败 |
| 607 | `YaginxProxy` | `UpstreamRequestBodyFailure` | 读取客户端请求体或写上游失败 |
| 608 | `YaginxProxy` | `ClientWriteFailure` | 响应已开始后写客户端失败 |
| 609 | `YaginxPipeline` | `ProxyPipelineException` | 明确的代理管道配置/状态异常 |
| 610 | `YaginxProxy` 或 `Unknown` | `UnknownProxyError` | 无法分类的代理错误或无有效状态 |

`errorCode` 保存稳定的 Go 分类 token，不保存完整错误文本；映射 fixture 必须覆盖 DNS、connect refused、TLS、timeout、body read/write、客户端取消、WebSocket 和 QUIC 错误。若响应已开始，不改写线上状态，只在 `request_history` 保存可确定的上游状态和错误分类。

## 首版固定差异

| 项目 | .NET 基线 | Go 首版 |
| --- | --- | --- |
| 9443 HTTP/3 | Kestrel 配置声明 HTTP/3，但 PROXY Protocol 只适用于 TCP，部署行为不稳定 | 明确只支持 TCP HTTP/1.1/2 |
| Host 规范化 | 多处仅 `ToLower()`，尾点和 IDNA 行为不统一 | 统一 IDNA ASCII、小写、去尾点，冲突时拒绝快照 |
| 配置冲突 | YARP 可能依赖 route precedence 或覆盖字典项 | 构建阶段拒绝，不发布部分候选快照 |

这些差异是已批准的目标行为，必须有迁移前扫描报告；发现会改变线上匹配结果的 Host 或路由时，切换门槛为先修正数据库配置并重新生成 golden fixture。

## 站点维护模式

`isMaintenanceMode` 未配置时为 false。开启后，ACME challenge 和 IP 黑名单保持更高优先级；其余匹配该站点的请求在 CORS、simple response、HTTPS 跳转、子规则和默认路由之前结束。

| 配置 | 固定响应 | 请求历史 | 上游 |
| --- | --- | --- | --- |
| `isMaintenanceMode=true` | `503 Service Unavailable`、`Content-Type: text/plain; charset=utf-8`、正文“当前站点正在维护，暂时停止访问。” | `YaginxPipeline` / `SiteMaintenance` | 不访问 |
