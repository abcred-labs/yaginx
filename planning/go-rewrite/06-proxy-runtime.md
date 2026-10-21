# 代理运行时

本章固定代理组件边界和运行策略；代理参数以 `contracts/runtime-config.schema.json` 为准。

## Listener

| 端口 | 固定协议 |
| --- | --- |
| 8080/TCP | HTTP/1.1 |
| 9080/TCP | trusted PROXY v1/v2 + HTTP/1.1 |
| 8443/TCP | TLS + HTTP/1.1/2 |
| 8443/UDP | QUIC + HTTP/3 |
| 9443/TCP | trusted PROXY v1/v2 + TLS + HTTP/1.1/2 |

环境变量保持四个现有 port 名。9080/9443 必须在 trusted peer 上收到 v1/v2 header；缺失、解析失败、超时或 non-trusted peer 一律在传输层关闭连接，绝不将首段字节交给 HTTP server。PROXY trusted CIDR 默认全来源以兼容部署，`doctor` production warning 要求收窄。9443 不启动 UDP。

## 参考实现与复用限制

Edge Gateway 是本章数据面主参考，基线为 `9c9bdc013a7d22bb5801e74901ba326a99c4d675`：

| Yaginx 区域 | 参考文件（相对 Edge Gateway root） | 本机绝对路径 | 迁入内容 | 不迁入内容 |
| --- | --- | --- | --- | --- |
| PROXY listener | `go/internal/proxyprotocol/listener.go` | `external source path omitted | `pires/go-proxyproto`、CIDR pre-check、v1/v2、header timeout、TLS TLV context 和测试向量 | AutoBan、Edge 的默认 loopback-only 配置 |
| HTTP transport | `go/internal/proxy/proxy.go` | `external source path omitted | activity timeout、WebSocket、trailer、TRACE、Next scoped `%40` 兼容测试 | filesystem domain registry、地理定位、黑名单、静态路由 |
| HTTP/3 | `go/cmd/edge-gateway/main.go` | `external source path omitted | TCP/UDP 同端口编排、`http3.Server`、Alt-Svc 和真实 HTTP/3 测试 | Edge 的 listener 默认值和控制面启动流程 |
| TLS/SNI | `go/internal/tls/manager.go` | `external source path omitted | `crypto/tls.GetCertificate`、热替换和证书故障测试向量 | filesystem LRU、fallback certificate、按需磁盘读取 |

Yaginx 的 runtime snapshot 在构建时把已验证证书解密到内存，因此 SNI 选择只读取 snapshot；无匹配证书必须失败。PROXY wrapper 必须先按 trusted CIDR 拒绝非可信 peer，再解析 header；仅在兼容默认全来源配置时允许全来源。

## Snapshot 与路由

- Host 统一 trim、IDNA ASCII、小写、去尾点；port 不参与 website match。
- 每个有效 website 一个 default route；enabled child rule 按 path segment 最长匹配优先。
- `pathPattern` 去首尾 slash 后按 segment prefix；`isRegexMatchPattern` 首版仍按 prefix，不启用 regex。
- duplicate Host、rule 或 ambiguous priority 使整个 candidate 失败，继续 last-known-good。
- `destination` 必须 absolute HTTP(S)；`destination` 空且 `destinationHost` 是 absolute URL 时执行 legacy fallback。
- simple response > redirect > child > default；ACME 和管理保留路径优先级更高。

## Transform

- default route 应用站点 `proxy_transforms`；child route 只应用自己的 Host、RemovePrefix、WebProxy 和 TLS 配置。
- 支持的 path/query/request header/forwarded/response header/trailer grammar在矩阵固定；unknown key/组合拒绝 candidate。
- default `isWithOriginalHostHeader`、`defaultDestinationHost` 和 child `destinationHost` 按矩阵顺序应用。
- header 使用 UTF-8 compatibility；hop-by-hop header 始终由 proxy 过滤，配置不能注入 CR/LF。
- `/_next/static` scoped package 的 `@` 编码为 `%40`，保留 query 原始 encoding。

## Transport

基于 `httputil.ReverseProxy` 和长期复用 `http.Transport`。transport key 包含 destination scheme/authority、forward proxy、TLS skip verify、timeout 和 header encoding；reload 新 key 新建 transport，旧 key有 active reference 时保留，归零后 `CloseIdleConnections`。

固定默认：activity/response header 15 分钟、request body unlimited、idle connection 90 秒、Expect Continue timeout 1 秒。WebSocket、SSE、chunked、大文件、trailer 和 client cancel 全程流式，不为日志/统计整包缓冲。

`UpstreamExpectContinue` 只对大于 1 MiB 的 POST/PUT/PATCH、命中 configured host/path、非 Upgrade 添加；完整条件见矩阵。

运行时必须直接应用 `runtime-config.schema.json` 中的代理参数：`activityTimeout` 到 TCP listener idle timeout，`responseHeaderTimeout`、idle connection/连接池和 `expectContinueTimeout` 到每个 upstream transport，`maxRequestHeaderBytes` 和非零 `maxRequestBodyBytes` 到业务 listener，HTTP/3 `maxIdleTimeout` 到 QUIC。任何无效 CIDR 或不安全限制都在 listener 启动前 fail-closed。`UpstreamExpectContinue` 只添加合法的上游 header，不删除客户端已有 header；`ipBlacklistCIDRs` 保留 .NET 的首个可解析 `X-Forwarded-For` 候选优先级，并由运行日志给出安全 warning。

## CORS、Blacklist、Redirect

- 为兼容当前实际代理中间件，所有 proxy route 对 Origin 执行 loose CORS，OPTIONS 返回 200 不访问上游；`isAllowAnySourceCors` 首版不改变结果。
- simple response 不自动加 proxy CORS。
- blacklist 使用 effective connection client IP；当前首个 X-Forwarded-For 候选行为保留并产生安全 warning，部署必须清除客户端自带 header。
- HTTP redirect 返回 302 到 normalized defaultHost，保留 encoded path/query；simple response 优先。
- 普通 proxy 路由删除入站 `X-Forwarded-For` 后交由 `httputil.ReverseProxy` 写入唯一 effective client IP，避免客户端链被重复追加；RFC7239-only transform 显式抑制 `X-Forwarded-*`。`Access-Control-Request-Headers` 和 `Access-Control-Request-Method` 无论实际方法均按原值回显，只有 `OPTIONS` 短路上游并以 `CorsPreflight` 分类。

## TLS/SNI

- 8443 TCP/UDP 和 9443 使用同一 certificate snapshot。
- exact SAN 优先 single-label wildcard；多证书按 active、已生效、notAfter、createdAt、ID 排序。
- 无匹配证书 handshake 失败，无 fallback/random certificate。
- private key 只在 process memory；snapshot swap 不关闭现有 TLS/HTTP/QUIC connection。

## 代理错误

601-610、status source、error category、response started 规则完全按矩阵。线上响应未开始时可写 gateway reserved status；已开始时不改写，只记录 request_history。上游真实 601-610 保存在 upstreamStatusCode，并以 source/category 区分。

## 包边界

- `internal/listeners`：TCP/UDP、PROXY、TLS/QUIC accept。
- `internal/proxy/config`：website JSON -> validated immutable routes/transports。
- `internal/proxy/runtime`：matching、transform、ReverseProxy、stream lifecycle。
- `internal/certificates/runtime`：SNI index 和 certificate selection。
- `internal/telemetry`：只接收完成事件，不允许在 request path 同步写 DB。

测试和性能门槛见第 10、14 章。
