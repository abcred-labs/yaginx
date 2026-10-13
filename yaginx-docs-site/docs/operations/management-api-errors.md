---
sidebar_position: 9
title: 管理 API 错误排查
---

# 管理 API 错误排查

管理 API 的 JSON 请求会忽略 DTO 未声明的字段，因此前端表单附带的
`autoLogin` 等字段不会阻止反序列化。非法 JSON、字段类型错误、空请求体
和同一请求中的多个 JSON 值仍会被拒绝。

错误响应保持统一信封：`code`、`msg`、`errMsg`、`tid` 和 `data`。`errMsg`
包含服务端返回的具体解析、业务或存储错误，`tid` 与响应头 `Act-Trace-Id`
一致。服务端日志使用同一个追踪号，例如：

```bash
journalctl -u yaginx --since "10 minutes ago" --no-pager \
  | grep -F '<tid>'
```

登录失败会保留对应的 HTTP 状态码，并在 `errMsg` 中说明是账号不存在还是
密码校验失败。数据库错误会同时记录在服务日志中，排查时应提供完整的
`tid`、请求时间、接口路径和响应内容。
