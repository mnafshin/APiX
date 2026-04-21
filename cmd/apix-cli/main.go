package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/mnafshin/apix/internal/config"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type rootOptions struct {
	host        string
	port        int
	tls         bool
	token       string
	verbose     bool
	debug       bool
	output      string
	noColor     bool
	timeout     time.Duration
	configPath  string
	configCheck bool
}

type app struct {
	opts   rootOptions
	out    io.Writer
	errw   io.Writer
	stdin  io.Reader
	cfg    *config.Config
	conn   *grpc.ClientConn
	client apix.EngineClient
	// Color functions (may be no-op if colors are disabled)
	errorColor   func(string, ...interface{}) string
	warnColor    func(string, ...interface{}) string
	successColor func(string, ...interface{}) string
	infoColor    func(string, ...interface{}) string
}

type historyListOptions struct {
	limit      int
	offset     int
	urlFilter  string
	method     string
	statusCode int
	sinceMs    int64
}

type historyGetOptions struct {
	pageSize  int
	requestID string
}

type watchOptions struct {
	count      int
	method     string
	urlPattern string
}

type filterOptions struct {
	method     string
	urlPattern string
	body       string
	limit      int
}

type exportOptions struct {
	format string
	output string
	limit  int
}

type bodyFlag struct {
	value string
	set   bool
}

func (b *bodyFlag) String() string { return b.value }
func (b *bodyFlag) Set(v string) error {
	b.value = v
	b.set = true
	return nil
}

type sendOptions struct {
	method          string
	url             string
	headers         headerFlags
	body            bodyFlag
	followRedirects bool
}

type replayOptions struct {
	headers         headerFlags
	body            bodyFlag
	followRedirects bool
}

type templatesSaveOptions struct {
	id      string
	name    string
	method  string
	url     string
	headers headerFlags
	body    string
}

type breakpointAddOptions struct {
	urlPattern string
	methods    stringSliceFlags
	label      string
	enabled    bool
}

type pausedForwardOptions struct {
	requestID string
	method    string
	url       string
	headers   headerFlags
	body      string
}

type pausedRespondOptions struct {
	requestID  string
	statusCode int
	statusText string
	headers    headerFlags
	body       string
}

type headerFlags []string

func (h *headerFlags) String() string { return strings.Join(*h, ",") }
func (h *headerFlags) Set(v string) error {
	*h = append(*h, v)
	return nil
}

func parseHeader(raw string) (string, string, error) {
	idx := strings.Index(raw, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid header %q (want key:value)", raw)
	}
	key := strings.TrimSpace(raw[:idx])
	if key == "" {
		return "", "", fmt.Errorf("invalid header %q (empty key)", raw)
	}
	val := strings.TrimSpace(raw[idx+1:])
	return key, val, nil
}

func (h headerFlags) Map() (map[string]string, error) {
	out := make(map[string]string, len(h))
	for _, raw := range h {
		key, val, err := parseHeader(raw)
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
	return out, nil
}

type stringSliceFlags []string

func (s *stringSliceFlags) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlags) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func writeLine(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func writeString(w io.Writer, s string) {
	_, _ = io.WriteString(w, s)
}

func stdinIsTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func readBodyFromStdin(r io.Reader) (string, error) {
	if r == nil {
		return "", nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// checkHelpFlag returns true if args contain --help or -h, and returns the remaining args
func checkHelpFlag(args []string) (bool, []string) {
	for i, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			remaining := append(args[:i], args[i+1:]...)
			return true, remaining
		}
	}
	return false, args
}

func int32FromInt(v int, field string) (int32, error) {
	if v < -1<<31 || v > 1<<31-1 {
		return 0, status.Errorf(codes.InvalidArgument, "%s out of range for int32", field)
	}
	return int32(v), nil
}

func capitalizeASCII(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// getEnvInt retrieves an integer environment variable, returning the default if not set or invalid.
func getEnvInt(envVar string, defaultVal int) int {
	if val, ok := os.LookupEnv(envVar); ok {
		if n, err := strconv.Atoi(val); err == nil && n >= 0 && n <= 65535 {
			return n
		}
	}
	return defaultVal
}

// getEnvDuration retrieves a duration environment variable, returning the default if not set or invalid.
func getEnvDuration(envVar string, defaultVal time.Duration) time.Duration {
	if val, ok := os.LookupEnv(envVar); ok {
		if dur, err := time.ParseDuration(val); err == nil {
			return dur
		}
	}
	return defaultVal
}

// getEnvBool retrieves a boolean environment variable, returning the default if not set or invalid.
func getEnvBool(envVar string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(envVar); ok {
		switch strings.ToLower(val) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return defaultVal
}

func Run(args []string, out io.Writer, errw io.Writer) int {
	return runWithStdin(args, out, errw, os.Stdin)
}

func runWithStdin(args []string, out io.Writer, errw io.Writer, stdin io.Reader) int {
	// Read environment variables for defaults (will be overridden by CLI flags)
	envHost := os.Getenv("APIX_HOST")
	if envHost == "" {
		envHost = "localhost"
	}
	envPort := getEnvInt("APIX_PORT", 9090)
	envTLS := getEnvBool("APIX_TLS", false)
	envToken := os.Getenv("APIX_TOKEN")
	envOutput := os.Getenv("APIX_OUTPUT")
	if envOutput == "" {
		envOutput = "text"
	}
	envTimeout := getEnvDuration("APIX_TIMEOUT", 0)

	fs := flag.NewFlagSet("apix", flag.ContinueOnError)
	fs.SetOutput(errw)

	var opts rootOptions
	fs.StringVar(&opts.host, "host", envHost, "engine host (env: APIX_HOST)")
	fs.IntVar(&opts.port, "port", envPort, "engine gRPC port (env: APIX_PORT)")
	fs.BoolVar(&opts.tls, "tls", envTLS, "use TLS for gRPC connection (env: APIX_TLS)")
	fs.StringVar(&opts.token, "token", envToken, "auth token (env: APIX_TOKEN)")
	fs.BoolVar(&opts.verbose, "verbose", false, "enable diagnostic logs")
	fs.BoolVar(&opts.verbose, "v", false, "shorthand for --verbose")
	fs.BoolVar(&opts.debug, "debug", false, "enable detailed diagnostic logs")
	fs.StringVar(&opts.output, "output", envOutput, "output format: text|json|ndjson (env: APIX_OUTPUT)")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable color output")
	fs.DurationVar(&opts.timeout, "timeout", envTimeout, "per-command timeout (0 = sensible default) (env: APIX_TIMEOUT)")
	fs.StringVar(&opts.configPath, "config", "", "path to config file (default: APiX search path)")
	fs.BoolVar(&opts.configCheck, "config-check", false, "validate config and exit (0=ok, 1=invalid)")
	fs.Usage = func() {
		writeLine(errw, "Usage: apix [global flags] <command> [args]")
		writeLine(errw)
		writeLine(errw, "Environment Variables:")
		writeLine(errw, "  APIX_HOST        Engine host (default: localhost)")
		writeLine(errw, "  APIX_PORT        Engine gRPC port (default: 9090)")
		writeLine(errw, "  APIX_TLS         Use TLS for connection (default: false)")
		writeLine(errw, "  APIX_TOKEN       Authentication token")
		writeLine(errw, "  APIX_OUTPUT      Output format: text|json|ndjson (default: text)")
		writeLine(errw, "  APIX_TIMEOUT     Per-command timeout (default: 0)")
		writeLine(errw)
		writeLine(errw, "Commands:")
		writeLine(errw, "  status")
		writeLine(errw, "  plugins list")
		writeLine(errw, "  history list|get|clear")
		writeLine(errw, "  watch [traffic]")
		writeLine(errw, "  filter")
		writeLine(errw, "  export")
		writeLine(errw, "  breakpoints list|add|delete|enable|disable")
		writeLine(errw, "  paused watch|forward|drop|respond")
		writeLine(errw, "  send")
		writeLine(errw, "  templates save|list|delete")
		writeLine(errw, "  replay")
		writeLine(errw, "  cert status")
		writeLine(errw, "  config show|reload")
		writeLine(errw, "  setup [profile]")
		writeLine(errw, "  completion <bash|zsh|fish>")
		writeLine(errw, "  doctor")
		writeLine(errw, "  help")
		writeLine(errw)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		err := status.Error(codes.InvalidArgument, "no command provided")
		emitRootError(out, errw, opts.output, err)
		if opts.output == "text" {
			fs.Usage()
		}
		return exitCodeForError(err)
	}

	cfgPath := opts.configPath
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	app := &app{
		opts: opts,
		out:  out,
		errw: errw,
		stdin: stdin,
		cfg:  config.LoadConfig(cfgPath),
	}

	// Initialize color functions based on noColor flag
	if opts.noColor {
		app.errorColor = func(format string, args ...interface{}) string {
			return fmt.Sprintf(format, args...)
		}
		app.warnColor = func(format string, args ...interface{}) string {
			return fmt.Sprintf(format, args...)
		}
		app.successColor = func(format string, args ...interface{}) string {
			return fmt.Sprintf(format, args...)
		}
		app.infoColor = func(format string, args ...interface{}) string {
			return fmt.Sprintf(format, args...)
		}
	} else {
		app.errorColor = color.RedString
		app.warnColor = color.YellowString
		app.successColor = color.GreenString
		app.infoColor = color.BlueString
	}

	defer app.close()

	// --config-check: validate config then exit without starting anything.
	if opts.configCheck {
		return app.runConfigCheck(cfgPath)
	}

	switch fs.Arg(0) {
	case "status":
		return app.exec("status", app.cmdStatus)
	case "version":
		return app.exec("version", app.cmdVersion)
	case "plugins":
		return app.exec("plugins", func() error { return app.cmdPlugins(fs.Args()[1:]) })
	case "history":
		return app.exec("history", func() error { return app.cmdHistory(fs.Args()[1:]) })
	case "watch":
		return app.exec("watch", func() error { return app.cmdWatch(fs.Args()[1:]) })
	case "filter":
		return app.exec("filter", func() error { return app.cmdFilter(fs.Args()[1:]) })
	case "export":
		return app.exec("export", func() error { return app.cmdExport(fs.Args()[1:]) })
	case "breakpoints":
		return app.exec("breakpoints", func() error { return app.cmdBreakpoints(fs.Args()[1:]) })
	case "paused":
		return app.exec("paused", func() error { return app.cmdPaused(fs.Args()[1:]) })
	case "send":
		return app.exec("send", func() error { return app.cmdSend(fs.Args()[1:]) })
	case "templates":
		return app.exec("templates", func() error { return app.cmdTemplates(fs.Args()[1:]) })
	case "replay":
		return app.exec("replay", func() error { return app.cmdReplay(fs.Args()[1:]) })
	case "cert":
		return app.exec("cert", func() error { return app.cmdCert(fs.Args()[1:]) })
	case "config":
		return app.exec("config", func() error { return app.cmdConfig(fs.Args()[1:]) })
	case "completion":
		return app.exec("completion", func() error { return app.cmdCompletion(fs.Args()[1:]) })
	case "setup":
		return app.exec("setup", func() error { return app.cmdSetup(fs.Args()[1:]) })
	case "doctor":
		return app.exec("doctor", app.cmdDoctor)
	case "help":
		fs.Usage()
		return 0
	default:
		err := status.Errorf(codes.InvalidArgument, "unknown command: %s", fs.Arg(0))
		emitRootError(out, errw, opts.output, err)
		return exitCodeForError(err)
	}
}

func (a *app) wrapErr(err error) int {
	if err == nil {
		return 0
	}
	exitCode := exitCodeForError(err)
	if emitsStructuredErrors(a.opts.output) {
		if emitErr := emitStructuredError(a.errw, a.opts.output, err, exitCode); emitErr == nil {
			return exitCode
		}
	}
	// Replace raw gRPC Unavailable errors with an actionable message for human output.
	displayErr := err
	if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
		displayErr = fmt.Errorf("APiX engine is not running.\n  Start it with: apix-engine\n  Or check: apix-cli status")
	}
	if a.opts.output == "text" {
		errMsg := a.errorColor("error: %v", displayErr)
		writeLine(a.errw, errMsg)
	} else {
		writeLine(a.errw, displayErr)
	}
	return exitCode
}

func (a *app) exec(name string, fn func() error) int {
	start := time.Now()
	a.diagf("running command: %s", name)
	err := fn()
	a.diagf("command %s finished in %v", name, time.Since(start).Round(time.Millisecond))
	return a.wrapErr(err)
}

func (a *app) close() {
	if a.conn != nil {
		_ = a.conn.Close()
	}
}

func (a *app) diagnosticsEnabled() bool {
	return a.opts.verbose || a.opts.debug
}

func (a *app) diagf(format string, args ...any) {
	if !a.diagnosticsEnabled() {
		return
	}
	writef(a.errw, "[debug] "+format+"\n", args...)
}

func (a *app) clientConn() (apix.EngineClient, error) {
	if a.client != nil {
		return a.client, nil
	}

	target := fmt.Sprintf("%s:%d", a.opts.host, a.opts.port)
	a.diagf("dialing engine: target=%s tls=%t", target, a.opts.tls)
	var creds credentials.TransportCredentials
	if a.opts.tls {
		serverName := a.opts.host
		if serverName == "" {
			serverName = "localhost"
		}
		creds = credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: serverName,
		})
	} else {
		creds = insecure.NewCredentials()
	}

	dialStart := time.Now()
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", target, err)
	}
	a.diagf("dial established in %v", time.Since(dialStart).Round(time.Millisecond))
	a.conn = conn
	a.client = apix.NewEngineClient(conn)
	return a.client, nil
}

func (a *app) unaryContext() (context.Context, context.CancelFunc) {
	timeout := a.opts.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	a.diagf("unary timeout=%v", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	if a.opts.token != "" {
		a.diagf("using bearer token for unary request")
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+a.opts.token)
	}
	return ctx, cancel
}

func (a *app) streamContext() (context.Context, context.CancelFunc) {
	if a.opts.timeout > 0 {
		a.diagf("stream timeout=%v", a.opts.timeout)
		ctx, cancel := context.WithTimeout(context.Background(), a.opts.timeout)
		if a.opts.token != "" {
			a.diagf("using bearer token for stream request")
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+a.opts.token)
		}
		return ctx, cancel
	}
	a.diagf("stream timeout=none")
	ctx := context.Background()
	if a.opts.token != "" {
		a.diagf("using bearer token for stream request")
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+a.opts.token)
	}
	return ctx, func() {}
}

func emitJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func emitNDJSON(out io.Writer, v any) error {
	return emitJSON(out, v)
}

func emitsStructuredErrors(output string) bool {
	return output == "json" || output == "ndjson"
}

func emitRootError(out io.Writer, errw io.Writer, output string, err error) {
	exitCode := exitCodeForError(err)
	if emitsStructuredErrors(output) {
		if emitErr := emitStructuredError(errw, output, err, exitCode); emitErr == nil {
			return
		}
	}
	writeLine(errw, err)
}

func emitStructuredError(w io.Writer, output string, err error, exitCode int) error {
	payload := map[string]any{
		"error": map[string]any{
			"code":      errorCodeForError(err),
			"message":   err.Error(),
			"grpc_code": grpcCodeForError(err),
			"exit_code": exitCode,
		},
	}
	if output == "ndjson" {
		return emitNDJSON(w, payload)
	}
	return emitJSON(w, payload)
}

func errorCodeForError(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return "internal"
	}
	switch st.Code() {
	case codes.OK:
		return "ok"
	case codes.InvalidArgument:
		return "invalid_argument"
	case codes.NotFound:
		return "not_found"
	case codes.Unauthenticated:
		return "unauthenticated"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.Unavailable:
		return "unavailable"
	case codes.DeadlineExceeded:
		return "deadline_exceeded"
	default:
		return "internal"
	}
}

func grpcCodeForError(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return "Unknown"
	}
	return st.Code().String()
}

func exitCodeForError(err error) int {
	st, ok := status.FromError(err)
	if !ok {
		return 2
	}
	switch st.Code() {
	case codes.OK:
		return 0
	case codes.InvalidArgument:
		return 2
	case codes.NotFound:
		return 3
	case codes.Unauthenticated:
		return 4
	case codes.PermissionDenied:
		return 5
	case codes.Unavailable:
		return 6
	case codes.DeadlineExceeded:
		return 7
	default:
		return 1
	}
}

func (a *app) cmdStatus() error {
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	resp, err := client.GetStatus(ctx, &apix.StatusRequest{})
	if err != nil {
		return err
	}
	if a.opts.output == "json" {
		return emitJSON(a.out, map[string]any{
			"status":      resp.Status,
			"version":     resp.Version,
			"proxy_port":  resp.ProxyPort,
			"grpc_port":   resp.GrpcPort,
			"tls_enabled": resp.TlsEnabled,
		})
	}
	statusLabel := resp.Status
	if resp.Status == "running" && a.opts.output == "text" {
		statusLabel = a.successColor(resp.Status)
	}
	writef(a.out, "APiX engine: %s\tversion=%s\tproxy=%d\tgrpc=%d\ttls=%t\n",
		statusLabel, resp.Version, resp.ProxyPort, resp.GrpcPort, resp.TlsEnabled)
	return nil
}

// cmdVersion calls GetVersion and prints the engine API version + compatibility info.
func (a *app) cmdVersion() error {
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	resp, err := client.GetVersion(ctx, &apix.VersionRequest{})
	if err != nil {
		return err
	}
	if a.opts.output == "json" {
		return emitJSON(a.out, map[string]any{
			"engine_version":     resp.EngineVersion,
			"api_version":        resp.ApiVersion,
			"min_client_version": resp.MinClientVersion,
		})
	}
	writef(a.out, "engine=%s  api=%s  min_client=%s\n",
		resp.EngineVersion, resp.ApiVersion, resp.MinClientVersion)
	return nil
}

func (a *app) cmdPlugins(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: plugins list")
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	resp, err := client.ListPlugins(ctx, &apix.PluginListRequest{})
	if err != nil {
		return err
	}
	if a.opts.output == "json" {
		items := make([]map[string]any, 0, len(resp.Plugins))
		for _, p := range resp.Plugins {
			items = append(items, map[string]any{
				"name":        p.Name,
				"version":     p.Version,
				"description": p.Description,
				"enabled":     p.Enabled,
			})
		}
		return emitJSON(a.out, items)
	}
	if len(resp.Plugins) == 0 {
		writeLine(a.out, "No plugins installed")
		return nil
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	writeLine(tw, "NAME\tVERSION\tENABLED\tDESCRIPTION")
	for _, p := range resp.Plugins {
		writef(tw, "%s\t%s\t%t\t%s\n", p.Name, p.Version, p.Enabled, p.Description)
	}
	return tw.Flush()
}

func (a *app) cmdHistory(args []string) error {
	if len(args) == 0 {
		writeLine(a.out, "Usage: history [--help] <subcommand>")
		writeLine(a.out, "")
		writeLine(a.out, "Subcommands:")
		writeLine(a.out, "  list    List captured transactions (default limit 100)")
		writeLine(a.out, "  get     Get a specific transaction by ID")
		writeLine(a.out, "  clear   Clear all captured transactions")
		return nil
	}

	helpRequested, remaining := checkHelpFlag(args)
	if helpRequested && len(remaining) == 0 {
		writeLine(a.out, "Usage: history [--help] <subcommand>")
		writeLine(a.out, "")
		writeLine(a.out, "Subcommands:")
		writeLine(a.out, "  list    List captured transactions (default limit 100)")
		writeLine(a.out, "  get     Get a specific transaction by ID")
		writeLine(a.out, "  clear   Clear all captured transactions")
		return nil
	}

	switch remaining[0] {
	case "list":
		if helpRequested {
			writeLine(a.out, "Usage: history list [flags]")
			writeLine(a.out, "")
			writeLine(a.out, "Flags:")
			writeLine(a.out, "  --limit N        Max number of results (default 100)")
			writeLine(a.out, "  --offset N       Result offset (default 0)")
			writeLine(a.out, "  --url-filter S   URL substring/regex filter")
			writeLine(a.out, "  --method M       HTTP method filter")
			writeLine(a.out, "  --status CODE    HTTP status code filter")
			writeLine(a.out, "  --since-ms MS    Only include transactions since unix ms")
			return nil
		}
		return a.cmdHistoryList(remaining[1:])
	case "get":
		if helpRequested {
			writeLine(a.out, "Usage: history get [flags]")
			writeLine(a.out, "")
			writeLine(a.out, "Flags:")
			writeLine(a.out, "  --request-id ID   Transaction request ID")
			writeLine(a.out, "  --page-size N     Page size for streaming (default 4096)")
			return nil
		}
		return a.cmdHistoryGet(remaining[1:])
	case "clear":
		if helpRequested {
			writeLine(a.out, "Usage: history clear")
			return nil
		}
		return a.cmdHistoryClear(remaining[1:])
	default:
		return fmt.Errorf("unknown history subcommand: %s", remaining[0])
	}
}

func (a *app) cmdHistoryList(args []string) error {
	fs := flag.NewFlagSet("history list", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := historyListOptions{limit: 100}
	fs.IntVar(&opts.limit, "limit", 100, "max number of results")
	fs.IntVar(&opts.offset, "offset", 0, "result offset")
	fs.StringVar(&opts.urlFilter, "url-filter", "", "URL substring/regex filter")
	fs.StringVar(&opts.method, "method", "", "HTTP method filter")
	fs.IntVar(&opts.statusCode, "status", 0, "HTTP status filter")
	fs.Int64Var(&opts.sinceMs, "since-ms", 0, "only include transactions since unix ms")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	limit, err := int32FromInt(opts.limit, "limit")
	if err != nil {
		return err
	}
	offset, err := int32FromInt(opts.offset, "offset")
	if err != nil {
		return err
	}
	statusFilter, err := int32FromInt(opts.statusCode, "status")
	if err != nil {
		return err
	}
	stream, err := client.GetHistory(ctx, &apix.HistoryQuery{
		Limit:        limit,
		Offset:       offset,
		UrlFilter:    opts.urlFilter,
		MethodFilter: opts.method,
		StatusFilter: statusFilter,
		SinceMs:      opts.sinceMs,
	})
	if err != nil {
		return err
	}
	items, err := recvHistory(stream)
	if err != nil {
		return err
	}
	if a.opts.output == "json" {
		return emitJSON(a.out, historyToJSON(items))
	}
	if len(items) == 0 {
		writeLine(a.out, "No history items")
		return nil
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	writeLine(tw, "ID\tREQUEST_ID\tMETHOD\tURL\tSTATUS\tDURATION_MS")
	for _, tx := range items {
		statusCode := int32(0)
		if tx.Response != nil {
			statusCode = tx.Response.StatusCode
		}
		writef(tw, "%s\t%s\t%s\t%s\t%d\t%d\n",
			tx.Id,
			historyRequestID(tx),
			tx.Request.Method,
			tx.Request.Url,
			statusCode,
			tx.DurationMs,
		)
	}
	return tw.Flush()
}

func (a *app) cmdHistoryGet(args []string) error {
	fs := flag.NewFlagSet("history get", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := historyGetOptions{pageSize: 100}
	fs.IntVar(&opts.pageSize, "page-size", 100, "page size while searching for an id")
	fs.StringVar(&opts.requestID, "request-id", "", "lookup by logical request_id (X-Request-ID)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" && opts.requestID == "" {
		return fmt.Errorf("usage: history get <id> [--page-size N] or history get --request-id <uuid> [--page-size N]")
	}
	if id != "" && opts.requestID != "" {
		return fmt.Errorf("provide either <id> or --request-id, not both")
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	targetID := id
	lookupByRequestID := false
	if opts.requestID != "" {
		targetID = opts.requestID
		lookupByRequestID = true
	}
	offset := 0
	for {
		ctx, cancel := a.unaryContext()
		limit, convErr := int32FromInt(opts.pageSize, "page-size")
		if convErr != nil {
			cancel()
			return convErr
		}
		queryOffset, convErr := int32FromInt(offset, "offset")
		if convErr != nil {
			cancel()
			return convErr
		}
		stream, err := client.GetHistory(ctx, &apix.HistoryQuery{Limit: limit, Offset: queryOffset})
		if err != nil {
			cancel()
			return err
		}
		items, recvErr := recvHistory(stream)
		cancel()
		if recvErr != nil {
			return recvErr
		}
		if len(items) == 0 {
			return status.Error(codes.NotFound, "history item not found")
		}
		for _, tx := range items {
			if (lookupByRequestID && historyRequestID(tx) == targetID) || (!lookupByRequestID && tx.Id == targetID) {
				if a.opts.output == "json" {
					return emitJSON(a.out, historyItemToJSON(tx))
				}
				b, _ := json.MarshalIndent(historyItemToJSON(tx), "", "  ")
				writeLine(a.out, string(b))
				return nil
			}
		}
		offset += len(items)
	}
}

func (a *app) cmdHistoryClear(args []string) error {
	fs := flag.NewFlagSet("history clear", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	force := fs.Bool("force", false, "clear history without confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*force {
		return fmt.Errorf("history clear requires --force")
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	if _, err := client.ClearHistory(ctx, &apix.Empty{}); err != nil {
		return err
	}
	if a.opts.output == "json" {
		return emitJSON(a.out, map[string]any{"cleared": true})
	}
	writeLine(a.out, "History cleared")
	return nil
}

func recvHistory(stream grpc.ServerStreamingClient[apix.HttpTransaction]) ([]*apix.HttpTransaction, error) {
	var items []*apix.HttpTransaction
	for {
		tx, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		items = append(items, tx)
	}
}

func historyToJSON(items []*apix.HttpTransaction) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, tx := range items {
		out = append(out, historyItemToJSON(tx))
	}
	return out
}

func historyItemToJSON(tx *apix.HttpTransaction) map[string]any {
	item := map[string]any{
		"id":          tx.Id,
		"request_id":  historyRequestID(tx),
		"timestamp":   tx.Timestamp,
		"duration_ms": tx.DurationMs,
	}
	if tx.Request != nil {
		item["request"] = map[string]any{
			"id":         tx.Request.Id,
			"method":     tx.Request.Method,
			"url":        tx.Request.Url,
			"headers":    tx.Request.Headers,
			"body":       string(tx.Request.Body),
			"timestamp":  tx.Request.Timestamp,
			"request_id": historyRequestID(tx),
		}
	}
	if tx.Response != nil {
		item["response"] = map[string]any{
			"status_code": tx.Response.StatusCode,
			"status_text": tx.Response.StatusText,
			"headers":     tx.Response.Headers,
			"body":        string(tx.Response.Body),
		}
	}
	return item
}

func validateStreamingOutput(output string) error {
	if output == "json" {
		return status.Error(codes.InvalidArgument, "--output json is not supported for streaming commands; use --output ndjson")
	}
	return nil
}

func historyRequestID(tx *apix.HttpTransaction) string {
	if tx.RequestId != "" {
		return tx.RequestId
	}
	if tx.Request != nil {
		for key, value := range tx.Request.Headers {
			if strings.EqualFold(key, "X-Request-ID") && value != "" {
				return value
			}
		}
	}
	return tx.Id
}

func (a *app) cmdWatch(args []string) error {
	if len(args) > 0 && args[0] == "traffic" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := watchOptions{}
	fs.IntVar(&opts.count, "count", 0, "stop after N events (for automation/testing)")
	fs.StringVar(&opts.method, "method", "", "filter by HTTP method (case-insensitive)")
	fs.StringVar(&opts.urlPattern, "url-pattern", "", "filter by URL substring")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateStreamingOutput(a.opts.output); err != nil {
		return err
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.streamContext()
	defer cancel()
	stream, err := client.CaptureTraffic(ctx, &apix.CaptureRequest{})
	if err != nil {
		return err
	}
	seen := 0
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if opts.method != "" && !strings.EqualFold(msg.Method, opts.method) {
			continue
		}
		if opts.urlPattern != "" && !strings.Contains(msg.Url, opts.urlPattern) {
			continue
		}
		if a.opts.output == "ndjson" || a.opts.output == "json" {
			requestID := msg.Id
			for key, value := range msg.Headers {
				if strings.EqualFold(key, "X-Request-ID") && value != "" {
					requestID = value
					break
				}
			}
			if err := emitNDJSON(a.out, map[string]any{
				"event":      "request",
				"id":         msg.Id,
				"request_id": requestID,
				"method":     msg.Method,
				"url":        msg.Url,
				"headers":    msg.Headers,
				"body":       string(msg.Body),
				"timestamp":  msg.Timestamp,
			}); err != nil {
				return err
			}
		} else {
			requestID := msg.Id
			for key, value := range msg.Headers {
				if strings.EqualFold(key, "X-Request-ID") && value != "" {
					requestID = value
					break
				}
			}
			writef(a.out, "%s\t%s\t%s\t%s\n", msg.Id, requestID, msg.Method, msg.Url)
		}
		seen++
		if opts.count > 0 && seen >= opts.count {
			return nil
		}
	}
}

func (a *app) cmdFilter(args []string) error {
	fs := flag.NewFlagSet("filter", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := filterOptions{limit: 100}
	fs.StringVar(&opts.method, "method", "", "filter by HTTP method (case-insensitive)")
	fs.StringVar(&opts.urlPattern, "url-pattern", "", "filter by URL substring")
	fs.StringVar(&opts.body, "body", "", "filter by request or response body substring")
	fs.IntVar(&opts.limit, "limit", 100, "max number of results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	limit, err := int32FromInt(opts.limit, "limit")
	if err != nil {
		return err
	}
	stream, err := client.GetHistory(ctx, &apix.HistoryQuery{
		Limit:        limit,
		MethodFilter: strings.ToUpper(opts.method),
		UrlFilter:    opts.urlPattern,
		BodyFilter:   opts.body,
	})
	if err != nil {
		return err
	}
	items, err := recvHistory(stream)
	if err != nil {
		return err
	}
	if a.opts.output == "json" {
		return emitJSON(a.out, historyToJSON(items))
	}
	if a.opts.output == "ndjson" {
		for _, tx := range items {
			if err := emitNDJSON(a.out, historyItemToJSON(tx)); err != nil {
				return err
			}
		}
		return nil
	}
	if len(items) == 0 {
		writeLine(a.out, "No matching history items")
		return nil
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	writeLine(tw, "ID\tREQUEST_ID\tMETHOD\tURL\tSTATUS\tDURATION_MS")
	for _, tx := range items {
		statusCode := int32(0)
		if tx.Response != nil {
			statusCode = tx.Response.StatusCode
		}
		writef(tw, "%s\t%s\t%s\t%s\t%d\t%d\n",
			tx.Id,
			historyRequestID(tx),
			tx.Request.Method,
			tx.Request.Url,
			statusCode,
			tx.DurationMs,
		)
	}
	return tw.Flush()
}

func (a *app) cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := exportOptions{format: "ndjson", limit: 100}
	fs.StringVar(&opts.format, "format", "ndjson", "export format: ndjson|har")
	fs.StringVar(&opts.output, "output", "", "output file path (default: stdout)")
	fs.IntVar(&opts.limit, "limit", 100, "max number of transactions to export")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if opts.format != "ndjson" && opts.format != "har" {
		return fmt.Errorf("unsupported export format %q (use ndjson or har)", opts.format)
	}

	dest := a.out
	if opts.output != "" {
		f, err := os.Create(opts.output)
		if err != nil {
			return fmt.Errorf("open output file: %w", err)
		}
		defer f.Close() //nolint:errcheck
		dest = f
	}

	client, err := a.clientConn()
	if err != nil {
		return err
	}

	if opts.format == "har" {
		ctx, cancel := a.unaryContext()
		defer cancel()
		resp, err := client.ExportHAR(ctx, &apix.ExportHARRequest{})
		if err != nil {
			return err
		}
		writeString(dest, resp.HarJson)
		if !strings.HasSuffix(resp.HarJson, "\n") {
			writeString(dest, "\n")
		}
		return nil
	}

	// ndjson: stream history and emit one JSON object per line
	ctx, cancel := a.unaryContext()
	defer cancel()
	limit, err := int32FromInt(opts.limit, "limit")
	if err != nil {
		return err
	}
	stream, err := client.GetHistory(ctx, &apix.HistoryQuery{Limit: limit})
	if err != nil {
		return err
	}
	for {
		tx, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := emitNDJSON(dest, historyItemToJSON(tx)); err != nil {
			return err
		}
	}
}

func (a *app) cmdBreakpoints(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: breakpoints list|add|delete|enable|disable")
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		ctx, cancel := a.unaryContext()
		defer cancel()
		resp, err := client.ListBreakpoints(ctx, &apix.Empty{})
		if err != nil {
			return err
		}
		if a.opts.output == "json" {
			items := make([]map[string]any, 0, len(resp.Breakpoints))
			for _, bp := range resp.Breakpoints {
				items = append(items, breakpointToJSON(bp))
			}
			return emitJSON(a.out, items)
		}
		if len(resp.Breakpoints) == 0 {
			writeLine(a.out, "No breakpoints configured")
			return nil
		}
		tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		writeLine(tw, "ID\tENABLED\tMETHODS\tPATTERN\tLABEL")
		for _, bp := range resp.Breakpoints {
			methods := strings.Join(bp.Methods, ",")
			if methods == "" {
				methods = "ALL"
			}
			writef(tw, "%s\t%t\t%s\t%s\t%s\n", bp.Id, bp.Enabled, methods, bp.UrlPattern, bp.Label)
		}
		return tw.Flush()
	case "add":
		return a.cmdBreakpointAdd(client, args[1:])
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: breakpoints delete <id>")
		}
		ctx, cancel := a.unaryContext()
		defer cancel()
		_, err := client.DeleteBreakpoint(ctx, &apix.BreakpointID{Id: args[1]})
		if err != nil {
			return err
		}
		if a.opts.output == "json" {
			return emitJSON(a.out, map[string]any{"deleted": args[1]})
		}
		writef(a.out, "Deleted breakpoint %s\n", args[1])
		return nil
	case "enable", "disable":
		if len(args) < 2 {
			return fmt.Errorf("usage: breakpoints %s <id>", args[0])
		}
		return a.cmdBreakpointToggle(client, args[1], args[0] == "enable")
	default:
		return fmt.Errorf("unknown breakpoints subcommand: %s", args[0])
	}
}

func breakpointToJSON(bp *apix.BreakpointRule) map[string]any {
	return map[string]any{
		"id":          bp.Id,
		"url_pattern": bp.UrlPattern,
		"methods":     bp.Methods,
		"enabled":     bp.Enabled,
		"label":       bp.Label,
	}
}

func (a *app) cmdBreakpointAdd(client apix.EngineClient, args []string) error {
	fs := flag.NewFlagSet("breakpoints add", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := breakpointAddOptions{enabled: true}
	fs.StringVar(&opts.urlPattern, "url-pattern", "", "URL pattern to match")
	fs.Var(&opts.methods, "method", "repeatable HTTP method filter")
	fs.StringVar(&opts.label, "label", "", "optional label")
	fs.BoolVar(&opts.enabled, "enabled", true, "whether the breakpoint starts enabled")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if opts.urlPattern == "" {
		return fmt.Errorf("breakpoints add requires --url-pattern")
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	resp, err := client.SetBreakpoint(ctx, &apix.BreakpointRule{
		UrlPattern: opts.urlPattern,
		Methods:    []string(opts.methods),
		Enabled:    opts.enabled,
		Label:      opts.label,
	})
	if err != nil {
		return err
	}
	if a.opts.output == "json" {
		return emitJSON(a.out, breakpointToJSON(resp.Breakpoint))
	}
	writef(a.out, "Added breakpoint %s\n", resp.Breakpoint.Id)
	return nil
}

func (a *app) cmdBreakpointToggle(client apix.EngineClient, id string, enabled bool) error {
	ctx, cancel := a.unaryContext()
	defer cancel()
	list, err := client.ListBreakpoints(ctx, &apix.Empty{})
	if err != nil {
		return err
	}
	for _, bp := range list.Breakpoints {
		if bp.Id != id {
			continue
		}
		ctx2, cancel2 := a.unaryContext()
		defer cancel2()
		resp, err := client.SetBreakpoint(ctx2, &apix.BreakpointRule{
			Id:         bp.Id,
			UrlPattern: bp.UrlPattern,
			Methods:    bp.Methods,
			Enabled:    enabled,
			Label:      bp.Label,
		})
		if err != nil {
			return err
		}
		if a.opts.output == "json" {
			return emitJSON(a.out, breakpointToJSON(resp.Breakpoint))
		}
		writef(a.out, "%s breakpoint %s\n", map[bool]string{true: "Enabled", false: "Disabled"}[enabled], id)
		return nil
	}
	return status.Error(codes.NotFound, "breakpoint not found")
}

func (a *app) cmdPaused(args []string) error {
	if len(args) == 0 {
		writeLine(a.out, "Usage: paused [--help] <subcommand>")
		writeLine(a.out, "")
		writeLine(a.out, "Subcommands:")
		writeLine(a.out, "  watch     Watch for paused requests")
		writeLine(a.out, "  forward   Forward a paused request")
		writeLine(a.out, "  drop      Drop a paused request")
		writeLine(a.out, "  respond   Respond to a paused request")
		return nil
	}
	helpRequested, remaining := checkHelpFlag(args)
	if helpRequested && len(remaining) == 0 {
		writeLine(a.out, "Usage: paused [--help] <subcommand>")
		writeLine(a.out, "")
		writeLine(a.out, "Subcommands:")
		writeLine(a.out, "  watch     Watch for paused requests")
		writeLine(a.out, "  forward   Forward a paused request")
		writeLine(a.out, "  drop      Drop a paused request")
		writeLine(a.out, "  respond   Respond to a paused request")
		return nil
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	switch remaining[0] {
	case "watch":
		if helpRequested {
			writeLine(a.out, "Usage: paused watch [flags]")
			writeLine(a.out, "")
			writeLine(a.out, "Watch for requests paused at breakpoints")
			return nil
		}
		return a.cmdPausedWatch(client, remaining[1:])
	case "forward":
		if helpRequested {
			writeLine(a.out, "Usage: paused forward <request-id> [flags]")
			return nil
		}
		return a.cmdPausedForward(client, remaining[1:])
	case "drop":
		if helpRequested {
			writeLine(a.out, "Usage: paused drop <request-id>")
			return nil
		}
		return a.cmdPausedDrop(client, remaining[1:])
	case "respond":
		if helpRequested {
			writeLine(a.out, "Usage: paused respond <request-id> [flags]")
			writeLine(a.out, "")
			writeLine(a.out, "Flags:")
			writeLine(a.out, "  --status N         HTTP status code")
			writeLine(a.out, "  --body FILE        Response body from file")
			return nil
		}
		return a.cmdPausedRespond(client, remaining[1:])
	default:
		return fmt.Errorf("unknown paused subcommand: %s", remaining[0])
	}
}

func (a *app) cmdPausedWatch(client apix.EngineClient, args []string) error {
	fs := flag.NewFlagSet("paused watch", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	count := fs.Int("count", 0, "stop after N events")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateStreamingOutput(a.opts.output); err != nil {
		return err
	}
	ctx, cancel := a.streamContext()
	defer cancel()
	stream, err := client.WatchPausedRequests(ctx, &apix.Empty{})
	if err != nil {
		return err
	}
	seen := 0
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		payload := map[string]any{
			"request_id":    msg.RequestId,
			"breakpoint_id": msg.BreakpointId,
			"paused_at":     msg.PausedAt,
			"request": map[string]any{
				"id":        msg.Request.Id,
				"method":    msg.Request.Method,
				"url":       msg.Request.Url,
				"headers":   msg.Request.Headers,
				"body":      string(msg.Request.Body),
				"timestamp": msg.Request.Timestamp,
			},
		}
		if a.opts.output == "ndjson" || a.opts.output == "json" {
			if err := emitNDJSON(a.out, payload); err != nil {
				return err
			}
		} else {
			writef(a.out, "%s\t%s\t%s\n", msg.RequestId, msg.Request.Method, msg.Request.Url)
		}
		seen++
		if *count > 0 && seen >= *count {
			return nil
		}
	}
}

func (a *app) cmdPausedForward(client apix.EngineClient, args []string) error {
	fs := flag.NewFlagSet("paused forward", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := pausedForwardOptions{}
	fs.StringVar(&opts.requestID, "request-id", "", "paused request id")
	fs.StringVar(&opts.method, "method", "", "override method")
	fs.StringVar(&opts.url, "url", "", "override URL")
	fs.Var(&opts.headers, "header", "repeatable header override key:value")
	fs.StringVar(&opts.body, "body", "", "override body")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if opts.requestID == "" {
		return fmt.Errorf("paused forward requires --request-id")
	}
	headers, err := opts.headers.Map()
	if err != nil {
		return err
	}
	req := &apix.ResumeAction{
		RequestId: opts.requestID,
		Action:    apix.ResumeAction_FORWARD,
	}
	if opts.method != "" || opts.url != "" || len(headers) > 0 || opts.body != "" {
		req.ModifiedRequest = &apix.HttpRequest{
			Method:  opts.method,
			Url:     opts.url,
			Headers: headers,
			Body:    []byte(opts.body),
		}
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	if _, err := client.ResumeRequest(ctx, req); err != nil {
		return err
	}
	return a.simpleResult("forwarded", opts.requestID)
}

func (a *app) cmdPausedDrop(client apix.EngineClient, args []string) error {
	fs := flag.NewFlagSet("paused drop", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	requestID := fs.String("request-id", "", "paused request id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *requestID == "" {
		return fmt.Errorf("paused drop requires --request-id")
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	if _, err := client.ResumeRequest(ctx, &apix.ResumeAction{RequestId: *requestID, Action: apix.ResumeAction_DROP}); err != nil {
		return err
	}
	return a.simpleResult("dropped", *requestID)
}

func (a *app) cmdPausedRespond(client apix.EngineClient, args []string) error {
	fs := flag.NewFlagSet("paused respond", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := pausedRespondOptions{statusCode: 200, statusText: "OK"}
	fs.StringVar(&opts.requestID, "request-id", "", "paused request id")
	fs.IntVar(&opts.statusCode, "status-code", 200, "response status code")
	fs.StringVar(&opts.statusText, "status-text", "OK", "response status text")
	fs.Var(&opts.headers, "header", "repeatable header key:value")
	fs.StringVar(&opts.body, "body", "", "response body")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if opts.requestID == "" {
		return fmt.Errorf("paused respond requires --request-id")
	}
	headers, err := opts.headers.Map()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	statusCode, err := int32FromInt(opts.statusCode, "status-code")
	if err != nil {
		return err
	}
	if _, err := client.ResumeRequest(ctx, &apix.ResumeAction{
		RequestId: opts.requestID,
		Action:    apix.ResumeAction_RESPOND,
		ModifiedResponse: &apix.HttpResponse{
			StatusCode: statusCode,
			StatusText: opts.statusText,
			Headers:    headers,
			Body:       []byte(opts.body),
		},
	}); err != nil {
		return err
	}
	return a.simpleResult("responded", opts.requestID)
}

func (a *app) simpleResult(action, id string) error {
	if a.opts.output == "json" {
		return emitJSON(a.out, map[string]any{"result": action, "request_id": id})
	}
	writef(a.out, "%s %s\n", capitalizeASCII(action), id)
	return nil
}

func (a *app) cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := sendOptions{method: "GET"}
	fs.StringVar(&opts.method, "method", "GET", "HTTP method")
	fs.StringVar(&opts.url, "url", "", "request URL")
	fs.Var(&opts.headers, "header", "repeatable header key:value")
	fs.Var(&opts.body, "body", "request body")
	fs.BoolVar(&opts.followRedirects, "follow-redirects", true, "follow redirects")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if opts.url == "" {
		return fmt.Errorf("send requires --url")
	}
	headers, err := opts.headers.Map()
	if err != nil {
		return err
	}
	body := opts.body.value
	if !opts.body.set {
		if stdinIsTerminal(a.stdin) {
			body = ""
		} else {
			body, err = readBodyFromStdin(a.stdin)
			if err != nil {
				return err
			}
		}
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	resp, err := client.ComposeRequest(ctx, &apix.ComposeSpec{
		Request: &apix.HttpRequest{
			Method:  opts.method,
			Url:     opts.url,
			Headers: headers,
			Body:    []byte(body),
		},
		FollowRedirects: opts.followRedirects,
	})
	if err != nil {
		return err
	}
	return a.renderResponse(resp)
}

func (a *app) cmdTemplates(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: templates save|list|delete")
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	switch args[0] {
	case "save":
		fs := flag.NewFlagSet("templates save", flag.ContinueOnError)
		fs.SetOutput(a.errw)
		opts := templatesSaveOptions{method: "GET"}
		fs.StringVar(&opts.id, "id", "", "template ID (optional; generated if omitted)")
		fs.StringVar(&opts.name, "name", "", "template name")
		fs.StringVar(&opts.method, "method", "GET", "HTTP method")
		fs.StringVar(&opts.url, "url", "", "request URL")
		fs.Var(&opts.headers, "header", "repeatable header key:value")
		fs.StringVar(&opts.body, "body", "", "request body")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if opts.url == "" {
			return fmt.Errorf("templates save requires --url")
		}
		headers, err := opts.headers.Map()
		if err != nil {
			return err
		}
		ctx, cancel := a.unaryContext()
		defer cancel()
		resp, err := client.SaveRequestTemplate(ctx, &apix.RequestTemplate{
			Id:   opts.id,
			Name: opts.name,
			Request: &apix.HttpRequest{
				Method:  opts.method,
				Url:     opts.url,
				Headers: headers,
				Body:    []byte(opts.body),
			},
		})
		if err != nil {
			return err
		}
		if a.opts.output == "json" {
			return emitJSON(a.out, map[string]any{
				"id":         resp.Id,
				"name":       resp.Name,
				"method":     resp.GetRequest().GetMethod(),
				"url":        resp.GetRequest().GetUrl(),
				"headers":    resp.GetRequest().GetHeaders(),
				"body":       string(resp.GetRequest().GetBody()),
				"updated_at": resp.UpdatedAt,
			})
		}
		writef(a.out, "Saved template %s\n", resp.Id)
		return nil
	case "list":
		ctx, cancel := a.unaryContext()
		defer cancel()
		resp, err := client.ListRequestTemplates(ctx, &apix.Empty{})
		if err != nil {
			return err
		}
		if a.opts.output == "json" {
			items := make([]map[string]any, 0, len(resp.Templates))
			for _, tpl := range resp.Templates {
				items = append(items, map[string]any{
					"id":         tpl.Id,
					"name":       tpl.Name,
					"method":     tpl.GetRequest().GetMethod(),
					"url":        tpl.GetRequest().GetUrl(),
					"headers":    tpl.GetRequest().GetHeaders(),
					"body":       string(tpl.GetRequest().GetBody()),
					"updated_at": tpl.UpdatedAt,
				})
			}
			return emitJSON(a.out, items)
		}
		if len(resp.Templates) == 0 {
			writeLine(a.out, "No saved templates")
			return nil
		}
		tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		writeLine(tw, "ID\tNAME\tMETHOD\tURL")
		for _, tpl := range resp.Templates {
			writef(tw, "%s\t%s\t%s\t%s\n", tpl.Id, tpl.Name, tpl.GetRequest().GetMethod(), tpl.GetRequest().GetUrl())
		}
		return tw.Flush()
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: templates delete <id>")
		}
		ctx, cancel := a.unaryContext()
		defer cancel()
		if _, err := client.DeleteRequestTemplate(ctx, &apix.RequestTemplateID{Id: args[1]}); err != nil {
			return err
		}
		if a.opts.output == "json" {
			return emitJSON(a.out, map[string]any{"deleted": true, "id": args[1]})
		}
		writef(a.out, "Deleted template %s\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown templates subcommand: %s", args[0])
	}
}

func (a *app) cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := replayOptions{}
	fs.Var(&opts.headers, "header", "repeatable override header key:value")
	fs.Var(&opts.body, "body", "override body")
	fs.BoolVar(&opts.followRedirects, "follow-redirects", true, "follow redirects")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: replay <request-id> [--header key:value] [--body BODY]")
	}
	id := fs.Arg(0)
	headers, err := opts.headers.Map()
	if err != nil {
		return err
	}
	body := opts.body.value
	if !opts.body.set {
		if stdinIsTerminal(a.stdin) {
			body = ""
		} else {
			body, err = readBodyFromStdin(a.stdin)
			if err != nil {
				return err
			}
		}
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	resp, err := client.ReplayRequest(ctx, &apix.ReplaySpec{
		Source:          &apix.ReplaySpec_RequestId{RequestId: id},
		OverrideHeaders: headers,
		OverrideBody:    []byte(body),
		FollowRedirects: opts.followRedirects,
	})
	if err != nil {
		return err
	}
	return a.renderResponse(resp)
}

func (a *app) renderResponse(resp *apix.HttpResponse) error {
	if a.opts.output == "json" {
		return emitJSON(a.out, map[string]any{
			"status_code": resp.StatusCode,
			"status_text": resp.StatusText,
			"headers":     resp.Headers,
			"body":        string(resp.Body),
		})
	}
	writef(a.out, "Status: %d %s\n", resp.StatusCode, resp.StatusText)
	if len(resp.Headers) > 0 {
		keys := make([]string, 0, len(resp.Headers))
		for k := range resp.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		writeLine(a.out, "Headers:")
		for _, k := range keys {
			writef(a.out, "  %s: %s\n", k, resp.Headers[k])
		}
	}
	writef(a.out, "\n%s\n", string(resp.Body))
	return nil
}

func (a *app) cmdCert(args []string) error {
	helpRequested, remaining := checkHelpFlag(args)
	if helpRequested || len(remaining) == 0 {
		writeLine(a.out, "Usage: cert [--help] status")
		writeLine(a.out, "")
		writeLine(a.out, "Show certificate status (CA cert and key ready for proxy)")
		return nil
	}
	if remaining[0] != "status" {
		return fmt.Errorf("usage: cert status")
	}
	info := certInfo(a.cfg)
	if a.opts.output == "json" {
		return emitJSON(a.out, info)
	}
	writef(a.out, "CA cert: %s (%s)\n", info["cert_path"], info["cert_status"])
	writef(a.out, "CA key: %s (%s)\n", info["key_path"], info["key_status"])
	return nil
}

func certInfo(cfg *config.Config) map[string]any {
	certExists := fileExists(cfg.CACertPath)
	keyExists := fileExists(cfg.CAKeyPath)
	return map[string]any{
		"cert_path":   cfg.CACertPath,
		"cert_status": map[bool]string{true: "present", false: "missing"}[certExists],
		"key_path":    cfg.CAKeyPath,
		"key_status":  map[bool]string{true: "present", false: "missing"}[keyExists],
		"ready":       certExists && keyExists,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runConfigCheck validates the loaded config, prints a summary, and exits with
// code 0 on success or 1 on failure. It is triggered by the --config-check flag.
func (a *app) runConfigCheck(cfgPath string) int {
	err := a.cfg.Validate()
	if err == nil {
		if a.opts.output == "json" {
			_ = emitJSON(a.out, map[string]any{"path": cfgPath, "valid": true, "errors": []string{}})
		} else {
			writef(a.out, "config ok: %s\n", cfgPath)
		}
		return 0
	}
	if a.opts.output == "json" {
		var msgs []string
		if ve, ok := err.(*config.ValidationError); ok {
			for _, e := range ve.Errs {
				msgs = append(msgs, e.Error())
			}
		} else {
			msgs = []string{err.Error()}
		}
		_ = emitJSON(a.out, map[string]any{"path": cfgPath, "valid": false, "errors": msgs})
	} else {
		writeLine(a.errw, err.Error())
	}
	return 1
}

func (a *app) cmdConfig(args []string) error {
	helpRequested, remaining := checkHelpFlag(args)
	if helpRequested || len(remaining) == 0 {
		writeLine(a.out, "Usage: config [--help] <subcommand>")
		writeLine(a.out, "")
		writeLine(a.out, "Subcommands:")
		writeLine(a.out, "  show    Show current configuration")
		writeLine(a.out, "  reload  Reload configuration from disk")
		return nil
	}
	switch remaining[0] {
	case "show":
		if helpRequested {
			writeLine(a.out, "Usage: config show")
			return nil
		}
		path := a.opts.configPath
		if path == "" {
			path = config.DefaultPath()
		}
		validation := "ok"
		if err := a.cfg.ValidateRuntime(); err != nil {
			validation = err.Error()
		}
		payload := map[string]any{
			"path":       path,
			"validation": validation,
			"config": map[string]any{
				"http_port":              a.cfg.HTTPPort,
				"grpc_port":              a.cfg.GRPCPort,
				"grpc_bind_address":      a.cfg.GRPCBindAddress,
				"db_path":                a.cfg.DBPath,
				"ca_cert_path":           a.cfg.CACertPath,
				"ca_key_path":            a.cfg.CAKeyPath,
				"tls_enabled":            a.cfg.TLSEnabled,
				"auth_token_set":         a.cfg.AuthToken != "",
				"max_body_size_mb":       a.cfg.MaxBodySizeMB,
				"replay_skip_tls_verify": a.cfg.ReplaySkipTLSVerify,
			},
		}
		if a.opts.output == "json" {
			return emitJSON(a.out, payload)
		}
		writef(a.out, "Path: %s\nValidation: %s\n", path, validation)
		writef(a.out, "gRPC: %s:%s (tls=%t)\n", a.cfg.GRPCBindAddress, a.cfg.GRPCPort, a.cfg.TLSEnabled)
		writef(a.out, "DB: %s\nCA cert: %s\nCA key: %s\n", a.cfg.DBPath, a.cfg.CACertPath, a.cfg.CAKeyPath)
		return nil
	case "reload":
		if helpRequested {
			writeLine(a.out, "Usage: config reload [--path FILE]")
			writeLine(a.out, "")
			writeLine(a.out, "Reload configuration from disk. Fields that require engine restart are skipped.")
			return nil
		}
		fs := flag.NewFlagSet("config reload", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := ""
		fs.StringVar(&path, "path", "", "config file path (defaults to engine startup config path)")
		if err := fs.Parse(remaining[1:]); err != nil {
			return fmt.Errorf("usage: config reload [--path <file>]")
		}
		client, err := a.clientConn()
		if err != nil {
			return err
		}
		ctx, cancel := a.unaryContext()
		defer cancel()
		resp, err := client.ReloadConfig(ctx, &apix.ConfigReloadRequest{Path: path})
		if err != nil {
			return err
		}
		payload := map[string]any{
			"config_path":    resp.GetConfigPath(),
			"applied_fields": resp.GetAppliedFields(),
			"skipped_fields": resp.GetSkippedFields(),
		}
		if a.opts.output == "json" {
			return emitJSON(a.out, payload)
		}
		writef(a.out, "Config reloaded: %s\n", resp.GetConfigPath())
		writef(a.out, "Applied: %s\n", strings.Join(resp.GetAppliedFields(), ", "))
		writef(a.out, "Skipped (restart required): %s\n", strings.Join(resp.GetSkippedFields(), ", "))
		return nil
	default:
		return fmt.Errorf("usage: config show|reload")
	}
}

func (a *app) cmdCompletion(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: completion <bash|zsh|fish>")
	}
	script, err := completionScript(args[0])
	if err != nil {
		return err
	}
	writeString(a.out, script)
	return nil
}

func completionScript(shell string) (string, error) {
	const bash = `# bash completion for apix
_apix() {
  local cur prev words cword
  _init_completion || return
  local commands="status plugins history watch breakpoints paused send templates replay cert config completion doctor help"
  local subcommands="list get clear add delete enable disable watch forward drop respond save show reload status"
  COMPREPLY=( $(compgen -W "${commands} ${subcommands}" -- "$cur") )
}
complete -F _apix apix
`
	const zsh = `#compdef apix
_apix() {
  local -a commands
  commands=(
    'status:Get engine status'
    'plugins:Plugin commands'
    'history:History commands'
    'watch:Watch traffic'
    'breakpoints:Breakpoint commands'
    'paused:Paused request commands'
    'send:Send a raw request'
    'templates:Manage saved request templates'
    'replay:Replay a stored request'
    'cert:Certificate commands'
    'config:Configuration commands'
    'completion:Generate shell completion'
    'doctor:Run diagnostics'
    'help:Show help'
  )
  _describe 'command' commands
}
_apix "$@"
`
	const fish = `complete -c apix -f -a "status plugins history watch breakpoints paused send templates replay cert config completion doctor help"
`
	switch shell {
	case "bash":
		return bash, nil
	case "zsh":
		return zsh, nil
	case "fish":
		return fish, nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
}

// ---------------------------------------------------------------------------
// setup command — guided first-capture onboarding
// ---------------------------------------------------------------------------

// setupProfile describes a guided capture profile.
type setupProfile struct {
	name        string
	description string
	proxyEnvVar string // environment variable to set, if applicable
	guide       string // terminal instructions for this profile
}

var setupProfiles = []setupProfile{
	{
		name:        "terminal",
		description: "Capture HTTP(S) traffic from shell commands (curl, wget, etc.)",
		proxyEnvVar: "HTTP_PROXY / HTTPS_PROXY",
		guide: `To capture traffic from the current terminal session, run:

  export HTTP_PROXY=http://localhost:{{.HTTPPort}}
  export HTTPS_PROXY=http://localhost:{{.HTTPPort}}

Then run any HTTP command — e.g.:

  curl https://api.example.com/health

To stop capturing: unset HTTP_PROXY HTTPS_PROXY`,
	},
	{
		name:        "system",
		description: "Capture all system HTTP traffic via OS proxy settings",
		proxyEnvVar: "OS proxy",
		guide: `Set your system HTTP proxy to: localhost:{{.HTTPPort}}

macOS:    System Settings → Network → Proxies → Web Proxy (HTTP)
          Host: localhost  Port: {{.HTTPPort}}
          Enable for HTTPS too.

Linux:    Network Manager → Proxy → Manual
          HTTP: localhost:{{.HTTPPort}}
          HTTPS: localhost:{{.HTTPPort}}

Windows:  Settings → Proxy → Manual proxy setup
          Address: localhost  Port: {{.HTTPPort}}`,
	},
	{
		name:        "browser",
		description: "Capture browser traffic (Chrome / Firefox / curl)",
		proxyEnvVar: "browser proxy",
		guide: `Configure your browser to use APiX as an HTTP proxy:

  Proxy: localhost  Port: {{.HTTPPort}}

Chrome (via flag):
  google-chrome --proxy-server="http://localhost:{{.HTTPPort}}"

Firefox:
  Settings → Network Settings → Manual proxy configuration
  HTTP: localhost:{{.HTTPPort}}  Use this proxy for all protocols ✓

Then install the APiX CA certificate in your browser's trust store
to intercept HTTPS traffic (see 'apix cert status').`,
	},
}

// cmdSetup runs the guided first-capture setup flow.
//
// Subcommands:
//
//	apix setup                — interactive walkthrough (default: shows status
//	                            and prints instructions for all profiles)
//	apix setup list           — list available capture profiles
//	apix setup terminal|system|browser  — print profile-specific instructions
func (a *app) cmdSetup(args []string) error {
	profile := ""
	if len(args) > 0 {
		profile = args[0]
	}

	// --- certificate status ---
	cert := certInfo(a.cfg)
	certReady, _ := cert["ready"].(bool)

	// --- engine reachability ---
	engineReachable := false
	proxyPort := a.cfg.HTTPPort
	var engineErr string

	client, connErr := a.clientConn()
	if connErr == nil {
		ctx, cancel := a.unaryContext()
		resp, err := client.GetStatus(ctx, &apix.StatusRequest{})
		cancel()
		if err == nil {
			engineReachable = true
			proxyPort = fmt.Sprintf("%d", resp.ProxyPort)
		} else {
			engineErr = err.Error()
		}
	} else {
		engineErr = connErr.Error()
	}

	// --- handle subcommands ---
	switch profile {
	case "list":
		return a.setupList()
	case "terminal", "system", "browser":
		return a.setupPrintProfile(profile, proxyPort)
	case "":
		// Interactive guided mode: print full status + default profile.
	default:
		return fmt.Errorf("unknown setup profile %q — run 'apix setup list' to see available profiles", profile)
	}

	// JSON mode: emit structured status.
	if a.opts.output == "json" {
		return emitJSON(a.out, map[string]any{
			"cert_ready":       certReady,
			"cert_path":        cert["cert_path"],
			"engine_reachable": engineReachable,
			"engine_error":     engineErr,
			"proxy_port":       proxyPort,
			"profiles":         setupProfileNames(),
		})
	}

	// Text mode: print guided walkthrough.
	writeLine(a.out, "=== APiX Setup ===")
	writeLine(a.out)

	// Step 1: engine health
	writeLine(a.out, "Step 1 — Engine")
	if engineReachable {
		writef(a.out, "  ✓ Engine is running (HTTP proxy on port %s)\n", proxyPort)
	} else {
		writeLine(a.out, "  ✗ Engine is not reachable.")
		writeLine(a.out, "    Start it with:  apix-engine  (or press F5 in VS Code)")
		if engineErr != "" {
			writef(a.out, "    Error: %s\n", engineErr)
		}
	}
	writeLine(a.out)

	// Step 2: certificate
	writeLine(a.out, "Step 2 — CA Certificate")
	if certReady {
		writef(a.out, "  ✓ CA certificate present at %s\n", cert["cert_path"])
	} else {
		writeLine(a.out, "  ✗ CA certificate not found.")
		writef(a.out, "    Expected at: %s\n", cert["cert_path"])
		writeLine(a.out, "    The engine generates the certificate on first start.")
		writeLine(a.out, "    To trust it for HTTPS interception, run:")
		writef(a.out, "      sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain %s  # macOS\n", cert["cert_path"])
		writef(a.out, "      sudo cp %s /usr/local/share/ca-certificates/ && sudo update-ca-certificates          # Linux\n", cert["cert_path"])
	}
	writeLine(a.out)

	// Step 3: capture profile instructions.
	writeLine(a.out, "Step 3 — First Capture (terminal profile)")
	if proxyPort == "" {
		proxyPort = a.cfg.HTTPPort
	}
	if err := a.setupPrintProfile("terminal", proxyPort); err != nil {
		return err
	}

	writeLine(a.out)
	writeLine(a.out, "Other profiles: run 'apix setup list' or 'apix setup <profile>'")
	return nil
}

func setupProfileNames() []string {
	names := make([]string, len(setupProfiles))
	for i, p := range setupProfiles {
		names[i] = p.name
	}
	return names
}

func (a *app) setupList() error {
	if a.opts.output == "json" {
		var out []map[string]string
		for _, p := range setupProfiles {
			out = append(out, map[string]string{
				"name":        p.name,
				"description": p.description,
			})
		}
		return emitJSON(a.out, out)
	}
	writeLine(a.out, "Available capture profiles:")
	for _, p := range setupProfiles {
		writef(a.out, "  %-12s  %s\n", p.name, p.description)
	}
	return nil
}

func (a *app) setupPrintProfile(name, proxyPort string) error {
	for _, p := range setupProfiles {
		if p.name != name {
			continue
		}
		if a.opts.output == "json" {
			return emitJSON(a.out, map[string]string{
				"name":        p.name,
				"description": p.description,
				"proxy_port":  proxyPort,
				"guide":       strings.ReplaceAll(p.guide, "{{.HTTPPort}}", proxyPort),
			})
		}
		writef(a.out, "Profile: %s — %s\n\n", p.name, p.description)
		writeLine(a.out, strings.ReplaceAll(p.guide, "{{.HTTPPort}}", proxyPort))
		return nil
	}
	return fmt.Errorf("unknown profile %q — run 'apix setup list'", name)
}

func (a *app) cmdDoctor() error {
	configPath := a.opts.configPath
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	configValidation := "ok"
	if err := a.cfg.ValidateRuntime(); err != nil {
		configValidation = err.Error()
	}
	cert := certInfo(a.cfg)
	engine := map[string]any{
		"reachable": false,
	}
	client, connErr := a.clientConn()
	if connErr == nil {
		ctx, cancel := a.unaryContext()
		resp, err := client.GetStatus(ctx, &apix.StatusRequest{})
		cancel()
		if err == nil {
			engine = map[string]any{
				"reachable":    true,
				"status":       resp.Status,
				"version":      resp.Version,
				"proxy_port":   resp.ProxyPort,
				"grpc_port":    resp.GrpcPort,
				"tls_enabled":  resp.TlsEnabled,
				"connect_host": a.opts.host,
			}
		} else {
			engine["error"] = err.Error()
		}
	} else {
		engine["error"] = connErr.Error()
	}

	payload := map[string]any{
		"config_path":       configPath,
		"config_validation": configValidation,
		"cert":              cert,
		"engine":            engine,
	}
	if a.opts.output == "json" {
		if err := emitJSON(a.out, payload); err != nil {
			return err
		}
		if reachable, _ := engine["reachable"].(bool); !reachable {
			if msg, _ := engine["error"].(string); msg != "" {
				return status.Error(codes.Unavailable, msg)
			}
			return status.Error(codes.Unavailable, "engine unreachable")
		}
		if configValidation != "ok" {
			return errors.New(configValidation)
		}
		return nil
	}

	writef(a.out, "Config: %s\n", configPath)
	configMsg := configValidation
	if configValidation != "ok" && a.opts.output == "text" {
		configMsg = a.errorColor(configValidation)
	}
	writef(a.out, "Config validation: %s\n", configMsg)

	certReady := cert["ready"]
	certMsg := fmt.Sprintf("%v", certReady)
	if ok, _ := certReady.(bool); !ok && a.opts.output == "text" {
		certMsg = a.warnColor(certMsg)
	}
	writef(a.out, "Cert ready: %s\n", certMsg)

	if reachable, _ := engine["reachable"].(bool); reachable {
		status := a.successColor("reachable")
		writef(a.out, "Engine: %s (%s)\n", status, engine["version"])
	} else {
		writef(a.out, "Engine: unreachable (%v)\n", engine["error"])
	}
	if reachable, _ := engine["reachable"].(bool); !reachable {
		if msg, _ := engine["error"].(string); msg != "" {
			return status.Error(codes.Unavailable, msg)
		}
		return status.Error(codes.Unavailable, "engine unreachable")
	}
	if configValidation != "ok" {
		return errors.New(configValidation)
	}
	return nil
}

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
