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
- If TLS is enabled, an auth_token must be configured to avoid accidental
  exposure when binding to 0.0.0.0

If you need stricter validation (e.g., file existence checks), call into the
`config` package and run `cfg.Validate()` from your orchestration scripts.
