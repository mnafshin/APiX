# Configuration Validation

This document describes APiX's configuration validation behaviour and the
`--config-check` CLI flag.

## --config-check

Running `apix-engine --config-check` loads the configuration from the
standard search locations (APIX_CONFIG, ~/.apix/config.yaml, /etc/apix/config.yaml,
./config.yaml) and runs the internal validation logic. If any invariant fails,
`apix-engine` exits with non-zero status and logs the validation error. On
success, it prints `config: validation passed` and exits 0.

This helps automation and packaging systems detect broken configurations before
starting the service.

## Validation rules

- http_port and grpc_port must be numeric strings (e.g. "8080")
- db_path must be set
- max_idle_conns_per_host must be > 0
- max_body_size_mb must be >= 0
- grpc_bind_address must be a valid bind host (IP address or localhost)
- If grpc_bind_address is remote (for example `0.0.0.0`), both `tls_enabled`
  and `auth_token` are required
- If TLS is enabled, `grpc_cert_path` and `grpc_key_path` must be set, exist,
  and load as a valid TLS key pair

If you need stricter validation (e.g., file existence checks), call into the
`config` package and run `cfg.Validate()` from your orchestration scripts.
