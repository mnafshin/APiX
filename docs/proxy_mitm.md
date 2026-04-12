# HTTP Proxying & TLS MITM (APiX)

Overview

APiX acts as an intercepting proxy. For HTTPS, it performs a MITM TLS handshake (using an in-repo CA) to capture and modify traffic. This page explains the high-level flow and where to find the implementation.

Key concepts
- CONNECT: browser/agent requests a TCP tunnel to the destination.
- MITM: APiX generates per-host certificates using an on-disk CA and performs a TLS handshake with the client.
- Request capture: once the TLS session is established, APiX reads HTTP requests from the tunnel and forwards them upstream.

Where to look in code
- internal/proxy/http.go — HTTP proxy and CONNECT handling
- internal/proxy/https.go — MITM TLS handling and request loop
- internal/proxy/certauthority.go — CA management (cert generation)

Security notes
- Users must install the CA certificate to trust intercepted TLS traffic.
- The repo limits MITM scope: generated certs are per-host with SNI.

Acceptance criteria
- Document explains CONNECT vs MITM clearly for new contributors
- Links to the exact files to edit for changes
