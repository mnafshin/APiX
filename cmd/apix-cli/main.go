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
	"strings"
	"text/tabwriter"
	"time"

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
	host       string
	port       int
	tls        bool
	token      string
	output     string
	noColor    bool
	timeout    time.Duration
	configPath  string
	configCheck bool
}

type app struct {
	opts   rootOptions
	out    io.Writer
	errw   io.Writer
	cfg    *config.Config
	conn   *grpc.ClientConn
	client apix.EngineClient
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
	pageSize int
}

type watchOptions struct {
	count int
}

type sendOptions struct {
	method          string
	url             string
	headers         headerFlags
	body            string
	followRedirects bool
}

type replayOptions struct {
	headers         headerFlags
	body            string
	followRedirects bool
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

func (h headerFlags) Map() (map[string]string, error) {
	out := make(map[string]string, len(h))
	for _, raw := range h {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header %q (want key:value)", raw)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid header %q (empty key)", raw)
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

func Run(args []string, out io.Writer, errw io.Writer) int {
	fs := flag.NewFlagSet("apix", flag.ContinueOnError)
	fs.SetOutput(errw)

	var opts rootOptions
	fs.StringVar(&opts.host, "host", "localhost", "engine host")
	fs.IntVar(&opts.port, "port", 9090, "engine gRPC port")
	fs.BoolVar(&opts.tls, "tls", false, "use TLS for gRPC connection")
	fs.StringVar(&opts.token, "token", "", "auth token")
	fs.StringVar(&opts.output, "output", "text", "output format: text|json|ndjson")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable color output")
	fs.DurationVar(&opts.timeout, "timeout", 0, "per-command timeout (0 = sensible default)")
	fs.StringVar(&opts.configPath, "config", "", "path to config file (default: APiX search path)")
	fs.BoolVar(&opts.configCheck, "config-check", false, "validate config and exit (0=ok, 1=invalid)")
	fs.Usage = func() {
		fmt.Fprintln(errw, "Usage: apix [global flags] <command> [args]")
		fmt.Fprintln(errw, "")
		fmt.Fprintln(errw, "Commands:")
		fmt.Fprintln(errw, "  status")
		fmt.Fprintln(errw, "  plugins list")
		fmt.Fprintln(errw, "  history list|get|clear")
		fmt.Fprintln(errw, "  watch [traffic]")
		fmt.Fprintln(errw, "  breakpoints list|add|delete|enable|disable")
		fmt.Fprintln(errw, "  paused watch|forward|drop|respond")
		fmt.Fprintln(errw, "  send")
		fmt.Fprintln(errw, "  replay")
		fmt.Fprintln(errw, "  cert status")
		fmt.Fprintln(errw, "  config show")
		fmt.Fprintln(errw, "  completion <bash|zsh|fish>")
		fmt.Fprintln(errw, "  doctor")
		fmt.Fprintln(errw, "  help")
		fmt.Fprintln(errw, "")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 2
	}

	cfgPath := opts.configPath
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	app := &app{
		opts: opts,
		out:  out,
		errw: errw,
		cfg:  config.LoadConfig(cfgPath),
	}
	defer app.close()

	// --config-check: validate config then exit without starting anything.
	if opts.configCheck {
		return app.runConfigCheck(cfgPath)
	}

	switch fs.Arg(0) {
	case "status":
		return app.wrapErr(app.cmdStatus())
	case "plugins":
		return app.wrapErr(app.cmdPlugins(fs.Args()[1:]))
	case "history":
		return app.wrapErr(app.cmdHistory(fs.Args()[1:]))
	case "watch":
		return app.wrapErr(app.cmdWatch(fs.Args()[1:]))
	case "breakpoints":
		return app.wrapErr(app.cmdBreakpoints(fs.Args()[1:]))
	case "paused":
		return app.wrapErr(app.cmdPaused(fs.Args()[1:]))
	case "send":
		return app.wrapErr(app.cmdSend(fs.Args()[1:]))
	case "replay":
		return app.wrapErr(app.cmdReplay(fs.Args()[1:]))
	case "cert":
		return app.wrapErr(app.cmdCert(fs.Args()[1:]))
	case "config":
		return app.wrapErr(app.cmdConfig(fs.Args()[1:]))
	case "completion":
		return app.wrapErr(app.cmdCompletion(fs.Args()[1:]))
	case "doctor":
		return app.wrapErr(app.cmdDoctor())
	case "help":
		fs.Usage()
		return 0
	default:
		fmt.Fprintf(errw, "unknown command: %s\n", fs.Arg(0))
		return 2
	}
}

func (a *app) wrapErr(err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(a.errw, err)
	return exitCodeForError(err)
}

func (a *app) close() {
	if a.conn != nil {
		_ = a.conn.Close()
	}
}

func (a *app) clientConn() (apix.EngineClient, error) {
	if a.client != nil {
		return a.client, nil
	}

	target := fmt.Sprintf("%s:%d", a.opts.host, a.opts.port)
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

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", target, err)
	}
	a.conn = conn
	a.client = apix.NewEngineClient(conn)
	return a.client, nil
}

func (a *app) unaryContext() (context.Context, context.CancelFunc) {
	timeout := a.opts.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	if a.opts.token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+a.opts.token)
	}
	return ctx, cancel
}

func (a *app) streamContext() (context.Context, context.CancelFunc) {
	if a.opts.timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), a.opts.timeout)
		if a.opts.token != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+a.opts.token)
		}
		return ctx, cancel
	}
	ctx := context.Background()
	if a.opts.token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+a.opts.token)
	}
	return ctx, func() {}
}

func (a *app) emit(v any) error {
	switch a.opts.output {
	case "json":
		return emitJSON(a.out, v)
	case "text":
		return nil
	default:
		return fmt.Errorf("unsupported output mode %q", a.opts.output)
	}
}

func emitJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func emitNDJSON(out io.Writer, v any) error {
	return emitJSON(out, v)
}

func exitCodeForError(err error) int {
	st, ok := status.FromError(err)
	if !ok {
		return 1
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
	fmt.Fprintf(a.out, "APiX engine: %s\tversion=%s\tproxy=%d\tgrpc=%d\ttls=%t\n",
		resp.Status, resp.Version, resp.ProxyPort, resp.GrpcPort, resp.TlsEnabled)
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
		fmt.Fprintln(a.out, "No plugins installed")
		return nil
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVERSION\tENABLED\tDESCRIPTION")
	for _, p := range resp.Plugins {
		fmt.Fprintf(tw, "%s\t%s\t%t\t%s\n", p.Name, p.Version, p.Enabled, p.Description)
	}
	return tw.Flush()
}

func (a *app) cmdHistory(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: history list|get|clear")
	}
	switch args[0] {
	case "list":
		return a.cmdHistoryList(args[1:])
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: history get <id> [--page-size N]")
		}
		return a.cmdHistoryGet(args[1], args[2:])
	case "clear":
		return a.cmdHistoryClear(args[1:])
	default:
		return fmt.Errorf("unknown history subcommand: %s", args[0])
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
	stream, err := client.GetHistory(ctx, &apix.HistoryQuery{
		Limit:        int32(opts.limit),
		Offset:       int32(opts.offset),
		UrlFilter:    opts.urlFilter,
		MethodFilter: opts.method,
		StatusFilter: int32(opts.statusCode),
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
		fmt.Fprintln(a.out, "No history items")
		return nil
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tMETHOD\tURL\tSTATUS\tDURATION_MS")
	for _, tx := range items {
		statusCode := int32(0)
		if tx.Response != nil {
			statusCode = tx.Response.StatusCode
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n", tx.Id, tx.Request.Method, tx.Request.Url, statusCode, tx.DurationMs)
	}
	return tw.Flush()
}

func (a *app) cmdHistoryGet(id string, args []string) error {
	fs := flag.NewFlagSet("history get", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := historyGetOptions{pageSize: 100}
	fs.IntVar(&opts.pageSize, "page-size", 100, "page size while searching for an id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	offset := 0
	for {
		ctx, cancel := a.unaryContext()
		stream, err := client.GetHistory(ctx, &apix.HistoryQuery{Limit: int32(opts.pageSize), Offset: int32(offset)})
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
			if tx.Id == id {
				if a.opts.output == "json" {
					return emitJSON(a.out, historyItemToJSON(tx))
				}
				b, _ := json.MarshalIndent(historyItemToJSON(tx), "", "  ")
				fmt.Fprintln(a.out, string(b))
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
	fmt.Fprintln(a.out, "History cleared")
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
		"timestamp":   tx.Timestamp,
		"duration_ms": tx.DurationMs,
	}
	if tx.Request != nil {
		item["request"] = map[string]any{
			"id":        tx.Request.Id,
			"method":    tx.Request.Method,
			"url":       tx.Request.Url,
			"headers":   tx.Request.Headers,
			"body":      string(tx.Request.Body),
			"timestamp": tx.Request.Timestamp,
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

func (a *app) cmdWatch(args []string) error {
	if len(args) > 0 && args[0] == "traffic" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := watchOptions{}
	fs.IntVar(&opts.count, "count", 0, "stop after N events (for automation/testing)")
	if err := fs.Parse(args); err != nil {
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
		if a.opts.output == "ndjson" || a.opts.output == "json" {
			if err := emitNDJSON(a.out, map[string]any{
				"event":     "request",
				"id":        msg.Id,
				"method":    msg.Method,
				"url":       msg.Url,
				"headers":   msg.Headers,
				"body":      string(msg.Body),
				"timestamp": msg.Timestamp,
			}); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(a.out, "%s\t%s\t%s\n", msg.Id, msg.Method, msg.Url)
		}
		seen++
		if opts.count > 0 && seen >= opts.count {
			return nil
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
			fmt.Fprintln(a.out, "No breakpoints configured")
			return nil
		}
		tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tENABLED\tMETHODS\tPATTERN\tLABEL")
		for _, bp := range resp.Breakpoints {
			methods := strings.Join(bp.Methods, ",")
			if methods == "" {
				methods = "ALL"
			}
			fmt.Fprintf(tw, "%s\t%t\t%s\t%s\t%s\n", bp.Id, bp.Enabled, methods, bp.UrlPattern, bp.Label)
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
		fmt.Fprintf(a.out, "Deleted breakpoint %s\n", args[1])
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
	fmt.Fprintf(a.out, "Added breakpoint %s\n", resp.Breakpoint.Id)
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
		fmt.Fprintf(a.out, "%s breakpoint %s\n", map[bool]string{true: "Enabled", false: "Disabled"}[enabled], id)
		return nil
	}
	return status.Error(codes.NotFound, "breakpoint not found")
}

func (a *app) cmdPaused(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: paused watch|forward|drop|respond")
	}
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	switch args[0] {
	case "watch":
		return a.cmdPausedWatch(client, args[1:])
	case "forward":
		return a.cmdPausedForward(client, args[1:])
	case "drop":
		return a.cmdPausedDrop(client, args[1:])
	case "respond":
		return a.cmdPausedRespond(client, args[1:])
	default:
		return fmt.Errorf("unknown paused subcommand: %s", args[0])
	}
}

func (a *app) cmdPausedWatch(client apix.EngineClient, args []string) error {
	fs := flag.NewFlagSet("paused watch", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	count := fs.Int("count", 0, "stop after N events")
	if err := fs.Parse(args); err != nil {
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
			fmt.Fprintf(a.out, "%s\t%s\t%s\n", msg.RequestId, msg.Request.Method, msg.Request.Url)
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
	if _, err := client.ResumeRequest(ctx, &apix.ResumeAction{
		RequestId: opts.requestID,
		Action:    apix.ResumeAction_RESPOND,
		ModifiedResponse: &apix.HttpResponse{
			StatusCode: int32(opts.statusCode),
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
	fmt.Fprintf(a.out, "%s %s\n", strings.Title(action), id)
	return nil
}

func (a *app) cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := sendOptions{method: "GET"}
	fs.StringVar(&opts.method, "method", "GET", "HTTP method")
	fs.StringVar(&opts.url, "url", "", "request URL")
	fs.Var(&opts.headers, "header", "repeatable header key:value")
	fs.StringVar(&opts.body, "body", "", "request body")
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
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	resp, err := client.ReplayRequest(ctx, &apix.ReplaySpec{
		Source: &apix.ReplaySpec_RawRequest{RawRequest: &apix.HttpRequest{
			Method:  opts.method,
			Url:     opts.url,
			Headers: headers,
			Body:    []byte(opts.body),
		}},
		FollowRedirects: opts.followRedirects,
	})
	if err != nil {
		return err
	}
	return a.renderResponse(resp)
}

func (a *app) cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(a.errw)
	opts := replayOptions{}
	fs.Var(&opts.headers, "header", "repeatable override header key:value")
	fs.StringVar(&opts.body, "body", "", "override body")
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
	client, err := a.clientConn()
	if err != nil {
		return err
	}
	ctx, cancel := a.unaryContext()
	defer cancel()
	resp, err := client.ReplayRequest(ctx, &apix.ReplaySpec{
		Source:          &apix.ReplaySpec_RequestId{RequestId: id},
		OverrideHeaders: headers,
		OverrideBody:    []byte(opts.body),
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
	fmt.Fprintf(a.out, "Status: %d %s\n", resp.StatusCode, resp.StatusText)
	if len(resp.Headers) > 0 {
		keys := make([]string, 0, len(resp.Headers))
		for k := range resp.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintln(a.out, "Headers:")
		for _, k := range keys {
			fmt.Fprintf(a.out, "  %s: %s\n", k, resp.Headers[k])
		}
	}
	fmt.Fprintf(a.out, "\n%s\n", string(resp.Body))
	return nil
}

func (a *app) cmdCert(args []string) error {
	if len(args) == 0 || args[0] != "status" {
		return fmt.Errorf("usage: cert status")
	}
	info := certInfo(a.cfg)
	if a.opts.output == "json" {
		return emitJSON(a.out, info)
	}
	fmt.Fprintf(a.out, "CA cert: %s (%s)\n", info["cert_path"], info["cert_status"])
	fmt.Fprintf(a.out, "CA key: %s (%s)\n", info["key_path"], info["key_status"])
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
			fmt.Fprintf(a.out, "config ok: %s\n", cfgPath)
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
		fmt.Fprintln(a.errw, err.Error())
	}
	return 1
}

func (a *app) cmdConfig(args []string) error {
	if len(args) == 0 || args[0] != "show" {
		return fmt.Errorf("usage: config show")
	}
	path := a.opts.configPath
	if path == "" {
		path = config.DefaultPath()
	}
	validation := "ok"
	if err := a.cfg.Validate(); err != nil {
		validation = err.Error()
	}
	payload := map[string]any{
		"path":       path,
		"validation": validation,
		"config": map[string]any{
			"http_port":             a.cfg.HTTPPort,
			"grpc_port":             a.cfg.GRPCPort,
			"grpc_bind_address":     a.cfg.GRPCBindAddress,
			"db_path":               a.cfg.DBPath,
			"ca_cert_path":          a.cfg.CACertPath,
			"ca_key_path":           a.cfg.CAKeyPath,
			"tls_enabled":           a.cfg.TLSEnabled,
			"auth_token_set":        a.cfg.AuthToken != "",
			"max_body_size_mb":      a.cfg.MaxBodySizeMB,
			"replay_skip_tls_verify": a.cfg.ReplaySkipTLSVerify,
		},
	}
	if a.opts.output == "json" {
		return emitJSON(a.out, payload)
	}
	fmt.Fprintf(a.out, "Path: %s\nValidation: %s\n", path, validation)
	fmt.Fprintf(a.out, "gRPC: %s:%s (tls=%t)\n", a.cfg.GRPCBindAddress, a.cfg.GRPCPort, a.cfg.TLSEnabled)
	fmt.Fprintf(a.out, "DB: %s\nCA cert: %s\nCA key: %s\n", a.cfg.DBPath, a.cfg.CACertPath, a.cfg.CAKeyPath)
	return nil
}

func (a *app) cmdCompletion(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: completion <bash|zsh|fish>")
	}
	script, err := completionScript(args[0])
	if err != nil {
		return err
	}
	fmt.Fprint(a.out, script)
	return nil
}

func completionScript(shell string) (string, error) {
	const bash = `# bash completion for apix
_apix() {
  local cur prev words cword
  _init_completion || return
  local commands="status plugins history watch breakpoints paused send replay cert config completion doctor help"
  local subcommands="list get clear add delete enable disable watch forward drop respond show status"
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
	const fish = `complete -c apix -f -a "status plugins history watch breakpoints paused send replay cert config completion doctor help"
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

func (a *app) cmdDoctor() error {
	configPath := a.opts.configPath
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	configValidation := "ok"
	if err := a.cfg.Validate(); err != nil {
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

	fmt.Fprintf(a.out, "Config: %s\n", configPath)
	fmt.Fprintf(a.out, "Config validation: %s\n", configValidation)
	fmt.Fprintf(a.out, "Cert ready: %v\n", cert["ready"])
	if reachable, _ := engine["reachable"].(bool); reachable {
		fmt.Fprintf(a.out, "Engine: reachable (%s)\n", engine["version"])
	} else {
		fmt.Fprintf(a.out, "Engine: unreachable (%v)\n", engine["error"])
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
