# Plugin Development (How-To)

This guide shows how to add a new built-in plugin to APiX.

## 1. Implement the plugin

Create a file under `internal/pluginrt/builtins/` and implement
`pkg/plugins.Plugin`.

```go
type MyPlugin struct{}

func (p *MyPlugin) Name() string        { return "my-plugin" }
func (p *MyPlugin) Version() string     { return "1.0.0" }
func (p *MyPlugin) Description() string { return "Example plugin" }

func (p *MyPlugin) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
    return nil, nil
}

func (p *MyPlugin) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
    return nil, nil
}
```

## 2. Register the plugin at startup

Register it in `cmd/apix-engine/main.go` with the other built-ins.

```go
if err := pluginRT.Register(&builtins.MyPlugin{}); err != nil {
    logging.Warnf(ctx, "register my-plugin: %v", err)
}
```

## 3. Add tests

Add tests in `internal/pluginrt/builtins/`:

- Request path behavior (`OnRequest`)
- Response path behavior (`OnResponse`)
- Error paths and edge cases

## 4. Validate locally

```bash
make lint
make test
```

## Notes

- Plugins run in registration order.
- A plugin error aborts the request/response pipeline.
- Panics are recovered by the runtime and returned as errors.

For interface and lifecycle details, see
[`../REFERENCE/plugin-sdk.md`](../REFERENCE/plugin-sdk.md).
