# Configuration Validation & Schema

This document describes APiX configuration loading, validation behaviour, and
the currently supported config keys.

## `--config-check`

Running `apix-engine --config-check` loads configuration from the standard
search locations (`APIX_CONFIG`, `~/.apix/config.yaml`, `/etc/apix/config.yaml`,
`./config.yaml`) and executes internal validation.

- On failure, `apix-engine` exits non-zero and logs the validation error.
- On success, it prints `config: validation passed` and exits 0.

This is intended for CI/deployment guards before starting the engine.

## Core validation rules

- `http_port` and `grpc_port` must be numeric strings (for example `"8080"`).
- `db_path` must be set.
- `max_idle_conns_per_host` must be `> 0`.
- `max_body_size_mb` must be `>= 0`.
- `grpc_bind_address` must be a valid bind host (`localhost` or IP).
- If `grpc_bind_address` is remote (for example `0.0.0.0`), both `tls_enabled`
  and `auth_token` are required.
- If `tls_enabled=true`, `grpc_cert_path` and `grpc_key_path` must be set and
  load as a valid TLS keypair.

## Top-level config keys (`config.yaml`)

APiX currently uses a **flat top-level YAML schema** (not nested `storage:`,
`tls:`, or `breakpoints:` sections).

```yaml
http_port: "8080"
grpc_port: "9090"
grpc_bind_address: "127.0.0.1"
db_path: "~/.apix/apix.db"
ca_cert_path: "~/.apix/ca.pem"
ca_key_path: "~/.apix/ca-key.pem"
tls_enabled: false
grpc_cert_path: ""
grpc_key_path: ""
auth_token: ""
max_idle_conns_per_host: 10
idle_conn_timeout_sec: 90
dial_timeout_sec: 10
upstream_tls_handshake_timeout_sec: 10
upstream_response_header_timeout_sec: 30
upstream_expect_continue_timeout_sec: 1
http_read_header_timeout_sec: 10
http_read_timeout_sec: 30
http_write_timeout_sec: 120
http_idle_timeout_sec: 120
max_body_size_mb: 32
replay_skip_tls_verify: false
breakpoint_pause_timeout_sec: 120
metrics_enabled: false
metrics_port: "9091"
health_port: "9092"
vacuum_interval_hours: 24
slowlog_threshold_ms: 1000
history_max_age_days: 0
history_max_rows: 0
grpc_rate_limit_per_sec: 0
mcp_enabled: false
mcp_port: "9093"
mcp_bind_address: "127.0.0.1"
mcp_allow_replay: false
mcp_allow_compose: false
plugin_paths: []
url_patterns: []
map_local_rules: []
```

## Breakpoint-related config

- `breakpoint_pause_timeout_sec`: max seconds to hold a paused request before
  forwarding unchanged. `0` means no pause timeout.

Breakpoint match conditions (`methods`, `header_name`, `header_value`,
`body_pattern`, `status_codes`) are configured per breakpoint rule through the
gRPC/CLI API, not as top-level static config keys.

## Rate-limit config

- `grpc_rate_limit_per_sec`: per-peer gRPC call limit (`0` disables limiter).

There is currently no separate top-level HTTP proxy rate-limit key. Some rate
control behavior exists in plugin implementations but those plugins are not all
active in the default runtime wiring.

## Plugin configuration schemas (implemented in code)

Built-in plugin configuration structs exist in `internal/pluginrt/builtins/*`.
Current runtime wiring registers `header-editor`, `mock-response`, and
`env-subst` by default; other built-ins are present in source.

| Plugin | Config struct (code) | Key fields |
|---|---|---|
| `rate-limiter` | `RateLimiterConfig` | `rules[]` |
| `traffic-shaping` | `TrafficShapingConfig` | `max_concurrent`, `bandwidth_bps`, `match_path`, `reject_with` |
| `jwt-auth` | `JWTAuthConfig` | `secret`, `algorithm`, `header_name`, `claims_to_headers`, `optional` |
| `retry-policy` | `RetryPolicyConfig` | `rules[]` |
| `caching` | `CachingConfig` | `capacity`, `ttl`, `cache_methods` |
| `latency-modifier` | `LatencyModifierConfig` | `rules[]` |
| `policy-engine` | `PolicyEngineConfig` | `rules[]`, `default_action` |
| `otel-tracing` | `OTelTracingConfig` | `endpoint`, `service_name`, `insecure`, `sample_rate` |
| `load-generator` | `LoadGeneratorConfig` | `match_path`, `concurrency`, `total_reqs`, `rate_per_sec`, `passthrough` |

See also:

- [`how-to/plugin-configuration.md`](how-to/plugin-configuration.md)
- [`REFERENCE/plugin-sdk.md`](REFERENCE/plugin-sdk.md)

## Plugin isolation & security model

Plugin hooks run **in-process** with engine privileges. APiX currently provides:

- panic recovery in runtime hook execution (prevents whole-engine crash),
- deterministic ordering by registration sequence.

APiX does **not** currently provide per-plugin sandboxing, memory quotas, or
process isolation in the default runtime path.
