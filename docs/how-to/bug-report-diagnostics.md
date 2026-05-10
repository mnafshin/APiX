# Bug Reports with Diagnostic Bundles

Use APiX CLI diagnostics to attach consistent troubleshooting context to bug reports.

## 1. Generate a bundle

```bash
apix doctor bundle --file apix-diagnostic.zip
```

This creates a zip with:

- `doctor.json` (engine reachability and cert/config checks)
- `cli.json` (CLI connection/runtime flags)
- `config_snapshot.json` (safe snapshot; secrets redacted by default)
- `environment.json` (selected APiX environment variables)
- `reproduction.txt` and `README.txt`

## 2. Add reproduction notes (optional)

```bash
apix doctor bundle --file apix-diagnostic.zip --notes "Steps and observed errors"
```

## 3. Review and attach

1. Open the zip and verify content is safe to share.
2. Attach it to your GitHub bug report.
3. Include exact commands and expected vs actual behavior.

## Redaction behavior

- Redaction is enabled by default (`--redact=true`).
- To include raw values for private/internal triage only, use `--redact=false`.
