# Plugin SDK & Lifecycle Reference

This page documents the plugin interface used by APiX runtime.

## Source of truth

- `pkg/plugins/sdk.go` — plugin interfaces and request/response types
- `internal/pluginrt/runtime.go` — registration, ordering, execution

## Required interface

Every plugin implements:

- `Name() string`
- `Version() string`
- `Description() string`
- `OnRequest(ctx, req) (*ProxyRequest, error)`
- `OnResponse(ctx, req, resp) (*ProxyResponse, error)`

## Lifecycle

1. Engine startup creates `pluginrt.Runtime`.
2. Built-ins are registered in `cmd/apix-engine/main.go`.
3. For each request:
   - runtime executes `OnRequest` hooks in registration order.
   - runtime executes `OnResponse` hooks in registration order.
4. A non-nil returned request/response replaces the current value.
5. Errors abort the chain for that request flow.

## Runtime behavior guarantees

- Deterministic ordering by registration sequence.
- Panic recovery wraps plugin panics and returns errors.
- Concurrent safety for registration/listing with internal locks.

## Current limitations

- No per-plugin sandboxing.
- No per-plugin CPU or memory quotas.
- No generic dynamic config endpoint for plugin settings.
