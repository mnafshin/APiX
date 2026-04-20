package main

import (
	"context"
	"flag"
	"fmt"
	logging "github.com/mnafshin/apix/internal/logging"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/metrics"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/pluginrt/builtins"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/server"
	"github.com/mnafshin/apix/internal/storage"
	usermsg "github.com/mnafshin/apix/internal/usermsg"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

func main() {
	configCheck := flag.Bool("config-check", false, "Validate config and exit")
	logFormat := flag.String("log-format", "", "Log output format: 'text' or 'json' (overrides LOG_FORMAT env var; default 'text')")
	logLevel := flag.String("log-level", "", "Log level: 'debug', 'info', 'warn', or 'error' (overrides LOG_LEVEL env var; default 'info')")
	flag.Parse()

	stop := make(chan os.Signal, 1)
	reloadSignal := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	signal.Notify(reloadSignal, syscall.SIGHUP)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	// 1. Load config from well-known search paths.
	cfgPath := config.DefaultPath()
	cfg := config.LoadConfig(cfgPath)
	format := firstNonEmpty(*logFormat, os.Getenv("LOG_FORMAT"), cfg.LogFormat, "text")
	level := firstNonEmpty(*logLevel, os.Getenv("LOG_LEVEL"), cfg.LogLevel, "info")
	logging.InitWithFormatAndLevel(os.Stdout, format, level)

	// Initialize metrics (Prometheus endpoint + slowlog)
	metrics.Init(cfg.MetricsEnabled)
	if cfg.MetricsEnabled {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		addr := ":" + cfg.MetricsPort
		startHTTPServer(ctx, wg, addr, mux, fmt.Sprintf("metrics endpoint listening on %s", addr), "metrics server")
	}

	// Always start the health endpoint (unless disabled via empty health_port).
	if cfg.HealthPort != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
		addr := ":" + cfg.HealthPort
		startHTTPServer(ctx, wg, addr, mux, fmt.Sprintf("health endpoint listening on %s/healthz", addr), "health server")
	}

	// If invoked with --config-check bail out after validating the config.
	if *configCheck {
		if err := cfg.Validate(); err != nil {
			logging.Errorf(ctx, "config validation failed: %v", err)
			logging.Fatalf(ctx, "%s", usermsg.UserMessage(err))
		}
		logging.Infof(ctx, "config: validation passed")
		return
	}

	// 2. Open SQLite database.
	db := initStorage(ctx, cfg)
	defer func() {
		if err := db.Close(); err != nil {
			logging.Warnf(ctx, "close database: %v", err)
		}
	}()

	// 3. Create CertAuthority.
	ca, err := proxy.NewCertAuthority(cfg.CACertPath, cfg.CAKeyPath)
	if err != nil {
		logging.Errorf(ctx, "create cert authority: %v", err)
		logging.Fatalf(ctx, "%s", usermsg.UserMessage(err))
	}

	// 4. Create breakpoints manager.
	bpManager := breakpoints.NewManager()

	// 5. Create plugin runtime and register built-ins.
	pluginRT, otelTracingPlugin := initPlugins(ctx, cfg)

	// 6. Create Engine.
	eng := engine.NewWithConfig(db, bpManager, pluginRT, cfg)

	// 7. Create replay Engine with TLS config from settings.
	replayEng := replay.NewEngine(db, &replay.ClientConfig{
		SkipTLSVerify:         cfg.ReplaySkipTLSVerify,
		DialTimeout:           time.Duration(cfg.DialTimeoutSec) * time.Second,
		IdleConnTimeout:       time.Duration(cfg.IdleConnTimeoutSec) * time.Second,
		TLSHandshakeTimeout:   time.Duration(cfg.UpstreamTLSHandshakeTimeoutSec) * time.Second,
		ResponseHeaderTimeout: time.Duration(cfg.UpstreamResponseHeaderTimeoutSec) * time.Second,
		ExpectContinueTimeout: time.Duration(cfg.UpstreamExpectContinueTimeoutSec) * time.Second,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
	})

	// 8. Create TLS + HTTP proxies.
	httpProxy, tlsProxy := initProxies(cfg, ca, eng, pluginRT)
	reloader := &runtimeConfigReloader{
		cfgPath:   cfgPath,
		cfg:       cfg,
		engine:    eng,
		httpProxy: httpProxy,
		db:        db,
	}
	server.SetConfigReloader(reloader.Reload)

	// 9. Start gRPC server.
	wg.Add(1)
	go func() {
		defer wg.Done()
		server.StartGRPCServer(ctx, eng, replayEng, cfg)
	}()

	// 10. Start HTTP proxy.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpProxy.Start(ctx); err != nil {
			logging.Errorf(ctx, "HTTP proxy stopped: %v", err)
		}
	}()

	// 11. Start MCP server (optional).
	if cfg.MCPEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.StartMCPServer(ctx, eng, replayEng, cfg)
		}()
	}

	// 12. Wait for shutdown signal.
running:
	for {
		select {
		case <-stop:
			break running
		case <-reloadSignal:
			if _, err := reloader.Reload(ctx, ""); err != nil {
				logging.Errorf(ctx, "config reload failed: %v", err)
			}
		}
	}
	logging.Infof(ctx, "Shutting down…")
	cancel()
	if otelTracingPlugin != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := otelTracingPlugin.Shutdown(shutdownCtx); err != nil {
			logging.Warnf(ctx, "otel-tracing shutdown: %v", err)
		}
		shutdownCancel()
	}

	// 13. Close proxies to release file descriptors.
	httpProxy.Close()
	tlsProxy.Close()

	// 14. Wait for all goroutines to finish, with a 15-second timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logging.Infof(ctx, "Goodbye.")
	case <-time.After(15 * time.Second):
		logging.Fatalf(ctx, "FATAL: shutdown timeout exceeded (15s), forcing exit")
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func startHTTPServer(ctx context.Context, wg *sync.WaitGroup, addr string, handler http.Handler, startupMessage, logTag string) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			<-ctx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutCtx); err != nil {
				logging.Errorf(ctx, "%s shutdown: %v", logTag, err)
			}
		}()
		logging.Infof(ctx, "%s", startupMessage)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Errorf(ctx, "%s error: %v", logTag, err)
		}
	}()
}

func initStorage(ctx context.Context, cfg *config.Config) *storage.DB {
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		logging.Errorf(ctx, "open database: %v", err)
		logging.Fatalf(ctx, "%s", usermsg.UserMessage(err))
	}
	if cfg.VacuumIntervalHours > 0 {
		db.StartPeriodicVacuum(ctx, time.Duration(cfg.VacuumIntervalHours)*time.Hour)
	}
	db.StartPeriodicPrune(ctx, 24*time.Hour, cfg.HistoryMaxAgeDays, cfg.HistoryMaxRows)
	return db
}

func initPlugins(ctx context.Context, cfg *config.Config) (*pluginrt.Runtime, *builtins.OTelTracing) {
	pluginRT := pluginrt.NewRuntime()
	var otelTracingPlugin *builtins.OTelTracing
	if err := pluginRT.Register(&builtins.HeaderEditor{}); err != nil {
		logging.Warnf(ctx, "register header-editor: %v", err)
	}
	if err := pluginRT.Register(&builtins.MockResponse{}); err != nil {
		logging.Warnf(ctx, "register mock-response: %v", err)
	}
	if err := pluginRT.Register(&builtins.EnvSubst{}); err != nil {
		logging.Warnf(ctx, "register env-subst: %v", err)
	}
	if cfg.OTelEnabled {
		var err error
		otelTracingPlugin, err = builtins.NewOTelTracing(ctx, builtins.OTelTracingConfig{
			Endpoint:    cfg.OTelEndpoint,
			ServiceName: cfg.OTelServiceName,
			Insecure:    cfg.OTelInsecure,
			SampleRate:  cfg.OTelSampleRate,
		})
		if err != nil {
			logging.Fatalf(ctx, "initialize otel-tracing plugin: %v", err)
		}
		if err := pluginRT.Register(otelTracingPlugin); err != nil {
			logging.Fatalf(ctx, "register otel-tracing plugin: %v", err)
		}
		logging.Infof(ctx, "otel tracing enabled (endpoint=%s service=%s sample_rate=%.2f)",
			cfg.OTelEndpoint, cfg.OTelServiceName, cfg.OTelSampleRate)
	}
	return pluginRT, otelTracingPlugin
}

func initProxies(cfg *config.Config, ca *proxy.CertAuthority, eng *engine.Engine, pluginRT *pluginrt.Runtime) (*proxy.HTTPProxy, *proxy.TLSProxy) {
	transportOpts := proxy.TransportOptions{
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       time.Duration(cfg.IdleConnTimeoutSec) * time.Second,
		DialTimeout:           time.Duration(cfg.DialTimeoutSec) * time.Second,
		TLSHandshakeTimeout:   time.Duration(cfg.UpstreamTLSHandshakeTimeoutSec) * time.Second,
		ResponseHeaderTimeout: time.Duration(cfg.UpstreamResponseHeaderTimeoutSec) * time.Second,
		ExpectContinueTimeout: time.Duration(cfg.UpstreamExpectContinueTimeoutSec) * time.Second,
	}
	tlsProxy := proxy.NewTLSProxy(ca, eng, transportOpts, cfg)
	tlsProxy.SetPlugins(pluginRT)
	httpProxy := proxy.NewHTTPProxy(":"+cfg.HTTPPort, tlsProxy, eng, transportOpts, cfg)
	httpProxy.SetPlugins(pluginRT)
	return httpProxy, tlsProxy
}

type runtimeConfigReloader struct {
	mu        sync.Mutex
	cfgPath   string
	cfg       *config.Config
	engine    *engine.Engine
	httpProxy *proxy.HTTPProxy
	db        *storage.DB
}

func (r *runtimeConfigReloader) Reload(ctx context.Context, explicitPath string) (*apix.ConfigReloadResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	path := r.cfgPath
	if explicitPath != "" {
		path = explicitPath
	}
	next := config.LoadConfig(path)
	if err := next.ValidateRuntime(); err != nil {
		return nil, err
	}

	applied := make([]string, 0, 8)
	skipped := make([]string, 0, 8)
	applyInt := func(name string, dst *int, newVal int) bool {
		if *dst == newVal {
			return false
		}
		logging.Infof(ctx, "config reloaded: %s %d->%d", name, *dst, newVal)
		*dst = newVal
		applied = append(applied, name)
		return true
	}
	skipIfChanged := func(name string, current, incoming any) {
		if current != incoming {
			skipped = append(skipped, name)
		}
	}

	applyInt("breakpoint_pause_timeout_sec", &r.cfg.BreakpointPauseTimeoutSec, next.BreakpointPauseTimeoutSec)
	r.engine.SetBreakpointPauseTimeoutSec(r.cfg.BreakpointPauseTimeoutSec)
	applyInt("max_body_size_mb", &r.cfg.MaxBodySizeMB, next.MaxBodySizeMB)
	applyInt("slowlog_threshold_ms", &r.cfg.SlowlogThresholdMs, next.SlowlogThresholdMs)

	changedRate := applyInt("proxy_rate_limit_per_sec", &r.cfg.ProxyRateLimitPerSec, next.ProxyRateLimitPerSec)
	changedConcurrent := applyInt("proxy_max_concurrent_connections", &r.cfg.ProxyMaxConcurrentConnections, next.ProxyMaxConcurrentConnections)
	if changedRate || changedConcurrent {
		r.httpProxy.SetRateLimits(r.cfg.ProxyRateLimitPerSec, r.cfg.ProxyMaxConcurrentConnections)
	}

	changedAge := applyInt("history_max_age_days", &r.cfg.HistoryMaxAgeDays, next.HistoryMaxAgeDays)
	changedRows := applyInt("history_max_rows", &r.cfg.HistoryMaxRows, next.HistoryMaxRows)
	if changedAge || changedRows {
		if err := r.db.PruneOldTransactions(r.cfg.HistoryMaxAgeDays, r.cfg.HistoryMaxRows); err != nil {
			logging.Warnf(ctx, "config reload prune: %v", err)
		}
	}

	skipIfChanged("http_port", r.cfg.HTTPPort, next.HTTPPort)
	skipIfChanged("grpc_port", r.cfg.GRPCPort, next.GRPCPort)
	skipIfChanged("grpc_bind_address", r.cfg.GRPCBindAddress, next.GRPCBindAddress)
	skipIfChanged("tls_enabled", r.cfg.TLSEnabled, next.TLSEnabled)
	skipIfChanged("grpc_cert_path", r.cfg.GRPCCertPath, next.GRPCCertPath)
	skipIfChanged("grpc_key_path", r.cfg.GRPCKeyPath, next.GRPCKeyPath)
	skipIfChanged("db_path", r.cfg.DBPath, next.DBPath)

	logging.Infof(ctx, "config reload complete: applied=%d skipped=%d", len(applied), len(skipped))
	return &apix.ConfigReloadResponse{
		ConfigPath:    path,
		AppliedFields: applied,
		SkippedFields: skipped,
	}, nil
}
