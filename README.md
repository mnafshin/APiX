![APiX Project Icon](/public/assets/img/APiX.png)
APiX — The API Extension & Debugging Toolkit

APiX is a developer-first API debugging and extension toolkit.
It combines features of a proxy, man-in-the-middle debugger, and a plugin runtime — giving developers full control over requests and responses.

Think of it as:
	•	proxychains + mitmproxy + plugin framework
	•	With CLI, UI, and CI/CD support.

⸻

✨ Features (MVP Roadmap)

	•	🔌 Core Proxy (HTTP/HTTPS)

Intercept and inspect traffic with built-in MITM support.

	•	🧩 Plugin Runtime

Extend APiX with request/response tampering, mocking, logging, etc.

	•	⚡ Tampering Rules

Modify headers, rewrite bodies, or inject responses.

	•	📦 Cross-Platform

Works on Linux, macOS, and Windows.

	•	🛠 Developer-Friendly CLI

Run APiX CLI commands to interact with the engine:

apix status
apix plugins
apix log


	•	🗂 Storage (MVP)

Capture requests in memory, with optional export to JSON.

⸻

🏗 Project Structure

```
apix/
├── cmd/              # Entry points (binaries)
│   ├── apix-engine/  # Core engine (proxy + API)
│   └── apix-cli/     # Developer CLI
│
├── pkg/              # Core packages
│   ├── api/          # gRPC/REST API
│   ├── proxy/        # HTTP/HTTPS proxy
│   ├── plugins/      # Plugin runtime + SDK
│   ├── storage/      # Logging & persistence
│   ├── tamper/       # Request/response modification
│   └── breakpoints/  # Breakpoint manager (future)
│
├── internal/         # Non-exported helpers
│   ├── config/       # Engine config
│   └── utils/        # Certs, logging, misc
│
├── ui/               # (Future) React-based UI
├── scripts/          # Build/release scripts
├── build/            # Dockerfiles, CI/CD configs
├── tests/            # Integration tests
└── README.md
```

⸻

🔧 Getting Started (MVP)

1. Start the Engine

Build and run the gRPC-enabled engine:

```
cd cmd/apix-engine
go build -o apix-engine
./apix-engine
```

The engine runs a gRPC server with reflection enabled, allowing introspection and interaction.

2. Run the CLI

In another terminal, build and run the CLI to interact with the engine:

```
cd cmd/apix-cli
go build -o apix-cli
./apix-cli status
./apix-cli plugins
./apix-cli log
```

3. Test Traffic Capture

Use `curl` with APiX as an HTTP proxy to generate traffic:

```
curl -x http://localhost:8080 https://example.com
```

This will be intercepted and logged by the engine.

⸻

🛠 CLI Command Examples

- `apix-cli status`

Shows the current status of the APiX engine.

Example output:

```
Engine Status: Running
Uptime: 2m35s
Active Connections: 1
```

- `apix-cli plugins`

Lists loaded plugins and their statuses.

Example output:

```
Loaded Plugins:
- HeaderEditor (enabled)
- EnvSubst (enabled)
- MockResponse (disabled)
```

- `apix-cli log`

Displays captured HTTP requests and responses.

Example output:

```
[1] GET https://example.com/ - 200 OK
[2] POST https://api.example.com/login - 401 Unauthorized
```

⸻

🔍 gRPC Reflection and Testing

The engine enables gRPC reflection, so you can also inspect and interact with it using tools like `grpcurl`:

```
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext localhost:50051 describe apix.EngineService
```

This allows advanced users to query engine internals and manage it programmatically.

⸻

🧩 Plugins

Plugins can extend APiX by hooking into request/response flows.
Example built-in plugins:
	•	HeaderEditor → add/modify headers
	•	EnvSubst → replace ${VARS} with environment values
	•	MockResponse → fake API responses

Custom plugins can be developed using the APiX plugin SDK.

⸻

📍 Roadmap

MVP (v0.1)
	•	HTTP/HTTPS proxy
	•	gRPC API for engine
	•	CLI with run + log
	•	In-memory storage
	•	Basic plugins

v0.2
	•	Breakpoints (pause/resume requests)
	•	Replay modified requests
	•	SQLite storage backend
	•	Remote engine support (auth + TLS)

v1.0
	•	UI frontend (React)
	•	OS-specific plugins (iptables, WinDivert, macOS NE)
	•	CI/CD integration helpers
	•	Advanced tamper scripting

⸻

⚖️ License

Apache 2.0

⸻

🤝 Contributing

Contributions are welcome!
	•	Fork & PR
	•	Add new plugins
	•	Improve docs & tests

⸻

👉 This is developer-friendly and forward-looking but also light enough.
