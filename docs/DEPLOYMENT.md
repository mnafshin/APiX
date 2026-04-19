# Deployment Guide

This guide covers deploying APiX for production use: local desktop, remote servers, and Docker containers.

## Prerequisites

- Git
- Go 1.25+ (for source builds)
- VS Code 1.85+ (for extension)
- Docker (optional, for container deployments)

---

## Installation

### Option 1: Pre-built Binary (Recommended)

**macOS (Apple Silicon)**:
```bash
curl -L https://github.com/mnafshin/apix/releases/download/v1.0.0/apix-engine-darwin-arm64 \
  -o apix-engine
chmod +x apix-engine
./apix-engine
```

**macOS (Intel)**:
```bash
curl -L https://github.com/mnafshin/apix/releases/download/v1.0.0/apix-engine-darwin-amd64 \
  -o apix-engine
chmod +x apix-engine
./apix-engine
```

**Linux (x86_64)**:
```bash
curl -L https://github.com/mnafshin/apix/releases/download/v1.0.0/apix-engine-linux-amd64 \
  -o apix-engine
chmod +x apix-engine
./apix-engine
```

**Windows (PowerShell)**:
```powershell
$url = "https://github.com/mnafshin/apix/releases/download/v1.0.0/apix-engine-windows-amd64.exe"
Invoke-WebRequest -Uri $url -OutFile apix-engine.exe
.\apix-engine.exe
```

### Option 2: Homebrew (macOS)

```bash
brew tap mnafshin/apix https://github.com/mnafshin/homebrew-apix
brew install apix
apix-engine
```

### Option 3: Docker

```bash
docker run -p 8080:8080 -p 9090:9090 \
  -e APIX_AUTH_TOKEN=your-secret-token \
  mnafshin/apix:1.0.0
```

For persistence and configuration:
```bash
docker run -p 8080:8080 -p 9090:9090 \
  -v ~/.apix:/root/.apix \
  -e APIX_CONFIG=/root/.apix/config.yaml \
  -e APIX_AUTH_TOKEN=your-secret-token \
  mnafshin/apix:1.0.0
```

### Option 4: Source Build

```bash
git clone https://github.com/mnafshin/apix.git
cd apix
go build -o apix-engine ./cmd/apix-engine/
./apix-engine
```

---

## Configuration

APiX looks for configuration in this order:
1. `$APIX_CONFIG` environment variable
2. `~/.apix/config.yaml`
3. `/etc/apix/config.yaml`
4. `./config.yaml`

### Example Config

Create `~/.apix/config.yaml`:

```yaml
# HTTP proxy port (plain HTTP + HTTPS CONNECT)
http_port: "8080"

# gRPC server port (for VS Code extension / CLI clients)
grpc_port: "9090"
grpc_bind_address: "127.0.0.1" # use 0.0.0.0 only with TLS + auth for remote mode

# MCP server (AI assistants)
mcp_enabled: false
mcp_port: "9093"
mcp_bind_address: "127.0.0.1"
mcp_allow_replay: false
mcp_allow_compose: false

# SQLite database path (stores traffic history)
db_path: "~/.apix/apix.db"

# Optional: Auth token for remote deployments
# IMPORTANT: Use env var APIX_AUTH_TOKEN instead to avoid plaintext secrets!
# auth_token: "your-secret-token-here"

# Optional: TLS for remote engines (for vscode.dev support)
tls_enabled: false
grpc_cert_path: "/etc/apix/grpc-server.pem"      # required when tls_enabled=true
grpc_key_path: "/etc/apix/grpc-server-key.pem"   # required when tls_enabled=true
ca_cert_path: "~/.apix/ca.pem"
ca_key_path: "~/.apix/ca-key.pem"

# HTTP transport settings
max_idle_conns_per_host: 10
idle_conn_timeout_sec: 90
dial_timeout_sec: 10

# Replay engine: allow self-signed certificates (dev only!)
replay_skip_tls_verify: false
```

### Using Environment Variables

For deployment, prefer environment variables over config files:

```bash
export APIX_AUTH_TOKEN="your-secret-token"  # Auth token for remote access
export APIX_CONFIG="/etc/apix/config.yaml"    # Custom config path

./apix-engine
```

---

## Linux Systemd Service

Create `/etc/systemd/system/apix.service`:

```ini
[Unit]
Description=APiX - HTTP/HTTPS API Debugger
Documentation=https://github.com/mnafshin/apix
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=apix
Group=apix
WorkingDirectory=/opt/apix
ExecStart=/usr/local/bin/apix-engine
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal

# Security hardening
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
NoNewPrivileges=true
ReadWritePaths=/var/lib/apix

# Environment variables
Environment="APIX_CONFIG=/etc/apix/config.yaml"
Environment="APIX_AUTH_TOKEN=your-secret-token"

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable apix
sudo systemctl start apix

# View logs
sudo journalctl -u apix -f
```

---

## Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  apix:
    image: mnafshin/apix:1.0.0
    ports:
      - "8080:8080"   # HTTP proxy
      - "9090:9090"   # gRPC API
    environment:
      APIX_AUTH_TOKEN: "your-secret-token"
      APIX_CONFIG: "/etc/apix/config.yaml"
    volumes:
      - ./config.yaml:/etc/apix/config.yaml:ro
      - apix-data:/root/.apix
    restart: unless-stopped
    # Optional: limits
    deploy:
      resources:
        limits:
          cpus: '1'
          memory: 512M

volumes:
  apix-data:
    driver: local
```

Start:

```bash
docker-compose up -d
```

---

## VS Code Extension

1. Open VS Code
2. Go to Extensions (Ctrl+Shift+X / Cmd+Shift+X)
3. Search for "APiX"
4. Click Install
5. Restart VS Code

The extension will:
- Auto-download the latest engine binary for your platform
- Auto-start the engine when you first use the traffic view
- Auto-connect to the local gRPC API (`:9090`)

---

## Quick Test

After starting the engine, test the proxy:

```bash
# In a new terminal, configure proxy
export http_proxy=http://localhost:8080
export https_proxy=http://localhost:8080

# Make a request
curl https://api.github.com/users/github

# Or use curl's built-in proxy flag
curl -x http://localhost:8080 https://api.github.com/users/github
```

Then in VS Code, open the APiX Traffic view to see the captured request.

---

## Production Checklist

- [ ] Config file created at `~/.apix/config.yaml` or `/etc/apix/config.yaml`
- [ ] `APIX_AUTH_TOKEN` environment variable set (for remote access)
- [ ] TLS certificates generated (if remote access needed)
  - [ ] Firewall rules updated (port 8080 for proxy, 9090 for gRPC)
  - [ ] MCP firewall rules configured (if `mcp_enabled: true`)
- [ ] Systemd service enabled and running (Linux)
- [ ] Log rotation configured (if running as service)
- [ ] Backup of `~/.apix/apix.db` (if storing important traffic)
- [ ] gRPC server firewall rules configured (restrict to loopback or specific IPs)
- [ ] Read/write timeouts configured (prevents DoS)

---

## Troubleshooting

### Engine won't start

**Check port in use**:
```bash
lsof -i :8080 -i :9090
```

**Check config file**:
```bash
cat ~/.apix/config.yaml
```

**Check logs**:
```bash
# If running as systemd service
sudo journalctl -u apix -n 50

# If running directly, check stdout/stderr
```

### Extension can't find engine

1. Ensure engine is running: `curl http://localhost:9090/` (should fail gracefully)
2. Check extension output: View → Output → APiX
3. Manually start engine: `./apix-engine` in terminal
4. Restart VS Code

### Traffic not captured

1. Verify proxy is configured correctly:
   ```bash
   echo $http_proxy $https_proxy
   ```

2. Test proxy directly:
   ```bash
   curl -v -x http://localhost:8080 https://example.com
   ```

3. Check breakpoints (disable if set): APiX Breakpoints view

---

## Remote Engine (vscode.dev)

APiX engine supports remote gRPC access over TLS for vscode.dev/browser clients.

1. Configure secure remote bind:
   ```yaml
   grpc_bind_address: "0.0.0.0"
   grpc_port: "9090"
   tls_enabled: true
   grpc_cert_path: "/etc/apix/grpc-server.pem"
   grpc_key_path: "/etc/apix/grpc-server-key.pem"
   # Prefer APIX_AUTH_TOKEN env var in production:
   auth_token: "your-secret-token"
   ```

2. Set token via environment variable (recommended):
   ```bash
   export APIX_AUTH_TOKEN="your-secret-token"
   ```

3. Start engine and open firewall for TCP/9090 only to trusted clients.

4. Connect from VS Code/vscode.dev using TLS endpoint:
   - `https://your-server:9090`
   - `Authorization: Bearer <token>`

---

## Getting Help

- **Documentation**: https://github.com/mnafshin/apix/tree/main/docs
- **Issues**: https://github.com/mnafshin/apix/issues
- **Discussions**: https://github.com/mnafshin/apix/discussions
