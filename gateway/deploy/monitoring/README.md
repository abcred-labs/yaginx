# Prometheus 规则

`yaginx-alerts.yaml` 是生产环境的 Prometheus recording/alert rule 示例，基于
gateway 已公开的低基数 `/metrics`。它不包含 scrape target、Alertmanager receiver、
生产地址或凭据。

部署前必须将 `job="yaginx-management"` 改为实际管理 listener 的 scrape job，并在
staging 校验所有表达式。延迟和错误率规则仅发现绝对异常；最终判定仍须与上一稳定版本的基线比较。
