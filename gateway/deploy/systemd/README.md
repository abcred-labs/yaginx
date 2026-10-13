# Memory Profiles

The default `yaginx.service` is the 1 GiB production profile:

- `GOMEMLIMIT=700MiB` and `GOGC=75` keep Go heap growth near the target.
- `MemoryHigh=768M` applies cgroup reclaim pressure before the hard limit.
- `MemoryMax=1G` is a hard boundary and can terminate the service.

The repository configuration examples explicitly select the same baseline:
`maxConcurrentRequests=256`, `maxRequestBodyBytes=0` (unlimited),
`maxRequestHeaderBytes=64KiB`, database pool `8/1`, and
`requestHistory.recordMode=ErrorsOnly` with `successSamplingRate=0`.

During the 24-hour and 72-hour observation windows, inspect:

- `yaginx_process_resident_memory_bytes`
- `yaginx_cgroup_memory_current_bytes`
- `yaginx_cgroup_memory_peak_bytes`
- `yaginx_cgroup_memory_metrics_available`
- `yaginx_cgroup_oom_events_total`
- `yaginx_cgroup_oom_kills_total`
- `yaginx_proxy_concurrency_rejected_total`
- `yaginx_tls_handshake_rejected_total`
- `yaginx_go_heap_alloc_bytes` and `yaginx_go_heap_inuse_bytes`

If RSS exceeds 850 MiB or remains near `MemoryHigh`, reduce
`proxy.maxConcurrentRequests` from 256 to 192, then 128 if necessary. If RSS
stays below 650 MiB and 503 rejection is materially high, increase it only by
64 after each 24-hour observation window. Do not increase queue limits to hide
database flush backlogs.

For a dedicated low-traffic instance, apply the optional 512 MiB drop-in:

```sh
install -D -m 0644 yaginx-low-memory.conf /etc/systemd/system/yaginx.service.d/memory.conf
systemctl daemon-reload
systemctl restart yaginx
```

The low-memory profile requires the matching runtime configuration: at most
64-128 concurrent requests, 4 MiB request bodies, 32 KiB headers, 64 idle
connections, and request history disabled. It is not the default because TLS,
HTTP/2, upstream buffers, and non-Go allocations are outside `GOMEMLIMIT`.
