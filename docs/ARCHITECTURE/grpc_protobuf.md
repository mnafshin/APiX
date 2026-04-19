# gRPC and Protobuf (APiX engine contract)

Why gRPC/protobuf
- gRPC provides typed RPCs including streaming; APiX uses it to stream captured traffic and paused requests to clients.
- The protobuf definitions (pkg/api/proto/apix.proto) are the source of truth. Clients (CLI, VS Code extension) rely on stable fields and enum semantics.

What contributors should know
- Unary vs streaming RPCs — CaptureTraffic uses a server stream to continuously push HttpRequest records.
- Do not edit pkg/api/generated/* directly. Run protoc to regenerate after editing pkg/api/proto/apix.proto.

Where to look
- pkg/api/proto/apix.proto — API definitions
- internal/server/grpc.go — gRPC server wiring and interceptors
- apix-vscode/src/engineClient.ts — typed client usage in the extension

Best practices
- Keep backwards-compatible changes in protos where possible (additive fields with defaults).
- Update apix-vscode/proto/apix.proto after regenerating the Go bindings.

Acceptance criteria
- Doc links proto and generated code and demonstrates a quick edit->regenerate workflow.
