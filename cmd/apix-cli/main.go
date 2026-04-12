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
	configPath string
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
	fs.Usage = func() {
		fmt.Fprintln(errw, "Usage: apix [global flags] <command> [args]")
		fmt.Fprintln(errw, "")
		fmt.Fprintln(errw, "Commands:")
		fmt.Fprintln(errw, "  status")
		fmt.Fprintln(errw, "  plugins list")
		fmt.Fprintln(errw, "  history list|get")
		fmt.Fprintln(errw, "  watch [traffic]")
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

	switch fs.Arg(0) {
	case "status":
		return app.wrapErr(app.cmdStatus())
	case "plugins":
		return app.wrapErr(app.cmdPlugins(fs.Args()[1:]))
	case "history":
		return app.wrapErr(app.cmdHistory(fs.Args()[1:]))
	case "watch":
		return app.wrapErr(app.cmdWatch(fs.Args()[1:]))
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
		return fmt.Errorf("usage: history list|get")
	}
	switch args[0] {
	case "list":
		return a.cmdHistoryList(args[1:])
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: history get <id> [--page-size N]")
		}
		return a.cmdHistoryGet(args[1], args[2:])
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

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
