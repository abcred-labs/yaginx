# 管理 API 兼容

> **Archived v1 design record.** This document is historical evidence only;
> v2 deployment and operations must follow the current OpenAPI and database
> lifecycle documentation.

## 权威合同

[management-api.openapi.yaml](../../contracts/management-api.openapi.yaml) 是完整协议：全部 operation、request/response schema、enum、默认值、认证、示例和 planned Go handler。本文只记录跨 operation 规则。

Base URL 固定 `/yaginx_console`，API 固定 `/yaginx_console/api/...`，但只由默认 `0.0.0.0:9090`、受 `observability.allowedCIDRs` 限制的 management listener 服务；四个业务 listener 对该 base path 返回 404。生产合同包含 account、website、web_domain、host_traffic、request_history、resource/report、system_setting 和 gateway_node；`dev/test`、`account/log_test` 不注册。

## 信封与序列化

成功：HTTP 200，`code=200`、`msg=null`、`errMsg=null`、`tid`、`data`。data 的 null/empty object/bool/string 由 operation fixture 决定：register 与 website/web_domain delete 为 `{}`，SMTP write/test 与 profile update 为 `true`，不得按 Go 的 void 语义统一改写。

错误：

| 场景 | HTTP/code | msg | data |
| --- | --- | --- | --- |
| Bearer 缺失/无效/过期 | 401 | `授权认证失败` | `{requestId}` |
| 授权策略拒绝 | 403 | `无对应授权` | `{requestId}` |
| 参数转换、JSON、业务、missing row、repository/controller exception | 500 | `调用异常` | `{requestId}` |
| Go 节点数据库不可写的写 operation | 503 | `服务暂时不可用` | `{requestId}` |

现有参数和业务错误通常是 HTTP 500，Go 不改为 400/404。当前 .NET golden 的 account、recovery、website、domain、SMTP 和查询 controller/repository exception 均为 `msg="调用异常"`、`errMsg=""`；Go 保持该 wire 行为，详细中文业务原因只写入脱敏 production log。若后续捕获到某个 operation 的非空 `errMsg`，必须以对应 fixture 和 OpenAPI operation 逐项加入，禁止全局猜测或替换。response content type 为 `application/json`。

JSON 使用 camelCase。C# `long/long?` response 按现有 converter 输出 decimal string；request/query 同时接受 decimal string 和 JSON integer。DateTime/DateTimeOffset 按 golden fixture 的 epoch converter 比较。空 list 输出 `[]`，已初始化 string 输出 `""`，nullable reference 可输出 null。

Legacy `[FromBody]` operation 要求 JSON body 存在，但 DTO 没有 validation attribute：省略属性时 string/nullable 绑定为 null，bool/int/enum/long 绑定为 false/0，属性级错误不得被 Go 自动改成 HTTP 400。OpenAPI 因此只把 body 本身标为 required；legacy request schema 的属性均可省略并标明 CLR default，handler/repository 按当前路径返回 500。`gateway_node` 是新合同，其 command DTO 继续使用明确的 required 字段和验证。

## 认证

- 匿名：login、password recovery 三步、`account/user_info`。
- 其他现有 operation 和 gateway_node 扩展要求 `Authorization: Bearer <token>`；cookie 不作为认证输入。
- 密码：`uppercase_hex(SHA256(UTF8(password + salt)))`；salt 为 16 random bytes Base64。
- JWT：HS256，issuer `Yaginx Proxy Server`，audience 必须是拼写 `Yaingx Clients`。
- claims：`iat`、`uid`、`uname`、`email`，非空才有 `ssid`。
- `nbf=issued-30s`、`exp=issued+10m+30s`、`expiresIn=600`。
- cookie `yaginx-token`、Path `/`、Secure、HttpOnly、expires=issued+10m。
- signing key 文件、跨切换验证和轮换见第 12 章；不得使用代码内默认 GUID key。

## 现有 operation 特例

- `account/user_info` 实际不要求认证。work-context 固定输出 `type/id/name/isAuthenticated`；HTTP identity 固定输出 `authenticationType/isAuthenticated/actor/bootstrapContext/claims/label/name/nameClaimType/roleClaimType`，claim 固定输出 `issuer/originalIssuer/properties/type/value/valueType`。匿名时 HTTP identity 可为 null。
- website/web_domain search 同时注册 GET 和 POST，POST body 被忽略。
- website/web_domain get missing row 允许 HTTP 200 + `data:null`；request_history get missing row抛 500。
- website rule refresh 30 秒节流：首次 `data="OK"`，窗口内返回上次 ISO-8601 时间字符串。
- host traffic 和 request history 默认 pageIndex=1/pageSize=50，repository pageSize 上限 200。
- report cycle enum 固定 5/10/20/30；period 上限 200；type 只允许 statusCode/spider/browser/os/duration。
- SMTP GET 为兼容当前 UI 继续返回 password；管理网络、日志和 trace 严禁记录该字段。
- password recovery 状态、10 分钟、5 次和中文错误见第 12 章。

## Gateway node 扩展

- `search`：分页、status、keyword，heartbeat desc + node ID 排序。
- `get?id=<uuid>`：missing 返回 200 null。
- `approve`：pending 分配最小可用 node number；disabled/draining 保留 number 并回 approved。
- `disable`：online approved 先进入 draining，完成后 disabled；offline approved 可直接 disabled。
- `reload`：递增 generation，节点异步完整对账，不传输配置/证书 payload。

扩展继续使用同一 Bearer、信封和 500 行为。状态转换和并发条件见第 12 章。

## Golden 对比

同一 PostgreSQL fixture 分别启动 .NET 和 Go，按 OpenAPI operationId 比较 status、content type、header、信封和 data。只归一化 tid、JWT、cookie expiry、随机 ID 和 current time；错误文本、null、顺序、Int64 string、status source 不归一化。每个 operation 至少覆盖 success、auth、parameter/business error 和 DB failure；missing row 按 operation 实际行为覆盖。
