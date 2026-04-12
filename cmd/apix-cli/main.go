package main

import (
"encoding/json"
"flag"
"fmt"
"io"
"os"
"strconv"
"time"
)

// Run executes the CLI with args (excluding program name). Returns exit code.
func Run(args []string, out io.Writer, errw io.Writer) int {
fs := flag.NewFlagSet("apix", flag.ContinueOnError)
host := fs.String("host", "localhost", "engine host")
port := fs.Int("port", 9090, "engine gRPC port")
_ = fs.Bool("tls", false, "use TLS for connection")
_ = fs.String("token", "", "auth token")
output := fs.String("output", "text", "output format: text|json|ndjson")
_ = fs.Bool("no-color", false, "disable color output")
if err := fs.Parse(args); err != nil {
fmt.Fprintln(errw, err)
return 2
}
if fs.NArg() == 0 {
fmt.Fprintln(out, "apix-cli: no command provided. Try 'status' or 'help'")
return 2
}
cmd := fs.Arg(0)
_ = host
_ = port

switch cmd {
case "status":
return cmdStatus(*output, out)
case "plugins":
// subcommands: list
if fs.NArg() < 2 || fs.Arg(1) != "list" {
fmt.Fprintln(errw, "usage: plugins list")
return 2
}
return cmdPluginsList(*output, out)
case "history":
if fs.NArg() < 2 {
fmt.Fprintln(errw, "usage: history list|get <id>")
return 2
}
sub := fs.Arg(1)
switch sub {
case "list":
return cmdHistoryList(*output, out)
case "get":
if fs.NArg() < 3 {
fmt.Fprintln(errw, "usage: history get <id>")
return 2
}
id := fs.Arg(2)
return cmdHistoryGet(*output, id, out)
default:
fmt.Fprintf(errw, "unknown history subcommand: %s\n", sub)
return 2
}
case "watch":
// watch --output ndjson --count N
count := 0
for i := 1; i < fs.NArg(); i++ {
if fs.Arg(i) == "--count" && i+1 < fs.NArg() {
v, _ := strconv.Atoi(fs.Arg(i+1))
count = v
}
}
return cmdWatch(*output, count, out)
case "breakpoints":
// breakpoints list|add|delete|enable|disable
if fs.NArg() < 2 {
fmt.Fprintln(errw, "usage: breakpoints list|add|delete|enable|disable")
return 2
}
sub := fs.Arg(1)
return cmdBreakpoints(sub, fs.Args()[2:], out)
case "send":
// send --method METHOD --url URL [--header k:v] --body BODY
fmt.Fprintln(out, "send: not implemented in this scaffold (stub)")
return 0
case "replay":
fmt.Fprintln(out, "replay: not implemented in this scaffold (stub)")
return 0
case "cert":
// cert status
if fs.NArg() < 2 || fs.Arg(1) != "status" {
fmt.Fprintln(errw, "usage: cert status")
return 2
}
return cmdCertStatus(*output, out)
case "config":
// config show
if fs.NArg() < 2 || fs.Arg(1) != "show" {
fmt.Fprintln(errw, "usage: config show")
return 2
}
return cmdConfigShow(*output, out)
case "completion":
fmt.Fprintln(out, "completion: shell completion generation not implemented in scaffold")
return 0
case "doctor":
fmt.Fprintln(out, "doctor: not implemented (stub). Shows connection, cert state, and config info.")
return 0
case "help":
fmt.Fprintln(out, "Commands: status, plugins list, history list|get, watch, breakpoints, cert status, config show, doctor")
return 0
default:
fmt.Fprintf(errw, "unknown command: %s\n", cmd)
return 2
}
}

func cmdStatus(output string, out io.Writer) int {
if output == "json" {
m := map[string]string{"status": "ok", "engine": "apix", "version": "dev"}
b, _ := json.Marshal(m)
fmt.Fprintln(out, string(b))
return 0
}
fmt.Fprintln(out, "APiX engine: ok (version: dev)")
return 0
}

func cmdPluginsList(output string, out io.Writer) int {
// TODO: wire to engine gRPC plugin list
plugins := []map[string]string{}
if output == "json" {
b, _ := json.Marshal(plugins)
fmt.Fprintln(out, string(b))
return 0
}
fmt.Fprintln(out, "No plugins installed")
return 0
}

func cmdHistoryList(output string, out io.Writer) int {
// TODO: query engine history
items := []map[string]string{}
if output == "json" {
b, _ := json.Marshal(items)
fmt.Fprintln(out, string(b))
return 0
}
fmt.Fprintln(out, "No history items")
return 0
}

func cmdHistoryGet(output, id string, out io.Writer) int {
// TODO: fetch by id
if id == "" {
fmt.Fprintln(out, "history item not found")
return 1
}
item := map[string]string{"id": id}
if output == "json" {
b, _ := json.Marshal(item)
fmt.Fprintln(out, string(b))
return 0
}
fmt.Fprintf(out, "History %s: (stub)\n", id)
return 0
}

func cmdWatch(output string, count int, out io.Writer) int {
// Streaming watch. For now emit a small number of NDJSON events when requested.
if output == "ndjson" {
if count <= 0 {
count = 5
}
for i := 0; i < count; i++ {
e := map[string]interface{}{"event": "request", "seq": i, "ts": time.Now().Unix()}
b, _ := json.Marshal(e)
fmt.Fprintln(out, string(b))
// small delay for readability in manual runs
time.Sleep(10 * time.Millisecond)
}
return 0
}
// non-streaming text fallback
fmt.Fprintln(out, "Starting watch (use --output ndjson for machine-readable streaming)")
return 0
}

func cmdBreakpoints(sub string, args []string, out io.Writer) int {
// TODO: implement breakpoint management commands that call engine via gRPC
switch sub {
case "list":
fmt.Fprintln(out, "No breakpoints configured")
return 0
case "add":
fmt.Fprintln(out, "add: not implemented (stub)")
return 0
case "delete":
fmt.Fprintln(out, "delete: not implemented (stub)")
return 0
case "enable":
fmt.Fprintln(out, "enable: not implemented (stub)")
return 0
case "disable":
fmt.Fprintln(out, "disable: not implemented (stub)")
return 0
default:
fmt.Fprintf(out, "unknown breakpoints subcommand: %s\n", sub)
return 2
}
}

func cmdCertStatus(output string, out io.Writer) int {
// TODO: inspect local cert file and engine trust
info := map[string]string{"status": "missing", "detail": "no certs found (stub)"}
if output == "json" {
b, _ := json.Marshal(info)
fmt.Fprintln(out, string(b))
return 0
}
fmt.Fprintln(out, "Cert status: missing (stub)")
return 0
}

func cmdConfigShow(output string, out io.Writer) int {
// TODO: show effective config; for now print a minimal stub
cfg := map[string]interface{}{"host": "localhost", "port": 9090}
if output == "json" {
b, _ := json.Marshal(cfg)
fmt.Fprintln(out, string(b))
return 0
}
fmt.Fprintln(out, "host: localhost\nport: 9090")
return 0
}

func main() {
os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
