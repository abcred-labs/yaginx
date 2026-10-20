---
sidebar_position: 7
title: Monitoring
---

# Monitoring

Monitor the management listener and the business listeners separately. The
management listener exposes process liveness, dependency readiness and
Prometheus metrics:

| Endpoint | Use |
| --- | --- |
| `/health/live` | Process liveness and restart detection |
| `/health/ready` | PostgreSQL, node approval and configuration readiness |
| `/metrics` | Request, queue, certificate and runtime metrics |

Collect the service journal and preserve the `Act-Trace-Id` from management
responses. Configure OpenTelemetry only with a protected collector endpoint;
Yaginx redacts credentials and sensitive request data from exported spans.

Alert on readiness failures, sustained 5xx or 601–610 proxy classifications,
queue growth, certificate renewal failures, stale nodes and database pool
exhaustion. During a PostgreSQL outage the loaded last-known-good proxy
snapshot can continue serving traffic while management writes fail; readiness
and recovery latency remain operator checks.
