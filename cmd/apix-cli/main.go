package main

import (
"encoding/json"
"flag"
"fmt"
"io"
"os"
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
_ = output
switch cmd {
case "status":
if *output == "json" {
m := map[string]string{"status": "ok", "engine": "apix", "version": "dev"}
b, _ := json.Marshal(m)
fmt.Fprintln(out, string(b))
return 0
}
fmt.Fprintln(out, "APiX engine: ok (version: dev)")
return 0
case "help":
fmt.Fprintln(out, "Commands: status, plugins, history, watch, breakpoints, doctor")
return 0
default:
fmt.Fprintf(errw, "unknown command: %s\n", cmd)
return 2
}
}

func main() {
os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
