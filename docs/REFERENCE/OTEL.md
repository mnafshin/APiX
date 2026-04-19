# OpenTelemetry Collector (OTel) integration

APiX exposes Prometheus-format metrics at `/metrics` when `metrics_enabled` is set in `config.yaml`. The recommended integration path is to run the OpenTelemetry Collector to scrape APiX's Prometheus endpoint and forward metrics via OTLP to your observability backend.

Quick start

1. Enable metrics in `config.yaml`:

```yaml
metrics_enabled: true
metrics_port: "9091"
```

2. Start APiX:

```bash
./apix-engine
```

3. Run the OpenTelemetry Collector with the sample config included in this repo:

```bash
docker run --rm \
  -v "$(pwd)/docs/otel-collector-config.yaml:/etc/otel-collector-config.yaml" \
  -p 4317:4317 \
  otel/opentelemetry-collector-contrib:latest \
  --config /etc/otel-collector-config.yaml
```

Notes

- If the Collector runs in Docker and APiX runs on the host, use `host.docker.internal:9091` (macOS/Windows) as the scrape target or use host networking on Linux.
- Adjust the OTLP exporter endpoint and TLS/insecure settings to match your backend.
- The sample config is intentionally minimal — production deployments should tune receivers/exporters and add security.

See `docs/otel-collector-config.yaml` for an example collector configuration.
