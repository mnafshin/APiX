# Plugin Configuration (How-To)

This guide describes the current plugin configuration model in APiX v2.0.0.

## Current state

APiX defines plugin config structs in code (for example
`internal/pluginrt/builtins/rate_limiter.go`), but there is no general runtime
`plugins.*` YAML loader that hydrates all plugin configs dynamically.

In practice:

- Plugin behavior is configured by constructor arguments / defaults in code.
- Built-ins registered in `cmd/apix-engine/main.go` are active at runtime.
- Additional plugin implementations may exist in source but be unregistered.

## Recommended pattern for wiring config

1. Add explicit fields to `internal/config/config.go`.
2. Populate defaults in `LoadConfig`.
3. Validate values in `Validate`.
4. Pass `cfg` values into the plugin constructor in `cmd/apix-engine/main.go`.

Example:

```go
// config.go
type Config struct {
	MyPluginEnabled bool `yaml:"my_plugin_enabled"`
	MyPluginLimit   int  `yaml:"my_plugin_limit"`
}
```

```go
// main.go
if cfg.MyPluginEnabled {
	p := builtins.NewMyPlugin(builtins.MyPluginConfig{
		Limit: cfg.MyPluginLimit,
	})
	_ = pluginRT.Register(p)
}
```

## Per-plugin schema references

Implemented config structs are documented in:

- [`../CONFIG_VALIDATION.md`](../CONFIG_VALIDATION.md)
- [`../REFERENCE/plugin-sdk.md`](../REFERENCE/plugin-sdk.md)
