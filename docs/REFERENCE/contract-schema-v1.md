# APiX Contract Schema v1

APiX contract files are the design-time source of truth for endpoint behavior.

## Versioning

- `schema_version` is mandatory.
- Current version is `apix.contract/v1`.
- Unknown versions are rejected.

## Top-level structure

```yaml
schema_version: apix.contract/v1
info:
  title: Billing API
  version: 1.0.0
  description: Optional text
servers:
  - url: https://api.example.com
endpoints:
  - path: /users/{id}
    operations:
      GET:
        parameters:
          - name: id
            in: path
            required: true
            schema:
              type: string
        responses:
          "200":
            description: OK
```

## Validation rules (deterministic)

1. `info.title` and `info.version` are required.
2. At least one endpoint is required.
3. Endpoint paths must start with `/`.
4. Operations must use supported HTTP methods (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`).
5. Every operation must define at least one response.
6. Response keys must be `default` or 3-digit status codes.
7. Parameters must use `in: path|query|header|cookie` and valid primitive/object/array JSON types.
8. Path placeholders like `{id}` must have a matching `in: path` parameter with `required: true`.

## CLI

- Create scaffold: `apix contract init --output contract.apix.yaml`
- Validate file: `apix contract validate --file contract.apix.yaml`

## Engine loading

Set `contract_paths` in config to load and validate contracts during engine startup and `--config-check` runs.
