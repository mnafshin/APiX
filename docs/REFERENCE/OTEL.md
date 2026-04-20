# OpenTelemetry Collector (OTel) integration

APiX supports both:

- Prometheus metrics at `/metrics` (when `metrics_enabled: true`)
- built-in OTLP trace export via the `otel-tracing` plugin (when `otel_enabled: true`)

The recommended integration path is to run the OpenTelemetry Collector and forward to your observability backend.

Quick start (metrics + traces)

1. Enable OTel settings in `config.yaml`:

```yaml
metrics_enabled: true
metrics_port: "9091"
otel_enabled: true
otel_endpoint: "localhost:4317"
otel_service_name: "apix-proxy"
otel_insecure: true
otel_sample_rate: 1.0
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
- Adjust `otel_endpoint` and `otel_insecure` for your backend.
- The sample config is intentionally minimal — production deployments should tune receivers/exporters and add security.

Common targets:

- Jaeger all-in-one OTLP gRPC endpoint: `localhost:4317`
- Grafana Tempo OTLP gRPC endpoint: `<tempo-host>:4317`

See `docs/otel-collector-config.yaml` for an example collector configuration.
