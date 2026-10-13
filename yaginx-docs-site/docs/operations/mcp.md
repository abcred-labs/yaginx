---
sidebar_position: 8
title: MCP 自动导入
---

# MCP 自动导入

Yaginx 的 MCP 服务复用现有管理端口，不新增监听端口。

## 自动导入地址

将下面地址提供给支持 URL 导入的 MCP 客户端：

```text
http(s)://<管理地址>:<管理端口>/.well-known/mcp.json
```

返回内容类似：

```json
{
  "mcpServers": {
    "yaginx": {
      "url": "http://127.0.0.1:9090/mcp"
    }
  }
}
```

配置发现地址不包含认证 token。客户端导入后，需要为该服务器配置：

```text
Authorization: Bearer <在管理 UI“API 凭据”页面创建的 token>
```

请在管理 UI 的“系统 → API 凭据”页面创建用途为 `mcp` 的凭据。Token 只在创建或重置后显示一次；MCP endpoint 是：

```text
http(s)://<管理地址>:<管理端口>/mcp
```

## 请求诊断工具

MCP 提供 `diagnose_request` 工具，用于查询请求历史和关联的系统错误。例如：

```json
{
  "requestId": "123456789"
}
```

也可以按 `from`、`to`、`host`、`path`、`statusCode` 和 `errorCode` 查询候选请求。

管理 UI 和 API 现在分别位于 `/` 和 `/api/...`。旧的 `/yaginx_console/` 路径已移除。
