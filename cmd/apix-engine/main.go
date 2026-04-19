package main

import (
	"context"
	"flag"
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
)

func main() {
	configCheck := flag.Bool("config-check", false, "Validate config and exit")
	logFormat := flag.String("log-format", "", "Log output format: 'text' or 'json' (overrides LOG_FORMAT env var; default 'text')")
	flag.Parse()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	// Resolve log format: flag > LOG_FORMAT env var > default "text".
	format := *logFormat
	if format == "" {
		format = os.Getenv("LOG_FORMAT")
	}
	if format == "" {
		format = "text"
	}
	logging.InitWithFormat(os.Stdout, format)

	// 1. Load config from well-known search paths.
	cfg := config.LoadConfig(config.DefaultPath())

	// Initialize metrics (Prometheus endpoint + slowlog)
	metrics.Init(cfg.MetricsEnabled)
	if cfg.MetricsEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mux := http.NewServeMux()
			mux.Handle("/metrics", metrics.Handler())
			addr := ":" + cfg.MetricsPort
			srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			go func() {
				<-ctx.Done()
				shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := srv.Shutdown(shutCtx); err != nil {
					logging.Errorf(ctx, "metrics server shutdown: %v", err)
				}
			}()
			logging.Infof(ctx, "metrics endpoint listening on %s", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logging.Errorf(ctx, "metrics server error: %v", err)
			}
		}()
	}

	// Always start the health endpoint (unless disabled via empty health_port).
	if cfg.HealthPort != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mux := http.NewServeMux()
			mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			})
			addr := ":" + cfg.HealthPort
			srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			go func() {
				<-ctx.Done()
				shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := srv.Shutdown(shutCtx); err != nil {
					logging.Errorf(ctx, "health server shutdown: %v", err)
				}
			}()
			logging.Infof(ctx, "health endpoint listening on %s/healthz", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logging.Errorf(ctx, "health server error: %v", err)
			}
		}()
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
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		logging.Errorf(ctx, "open database: %v", err)
		logging.Fatalf(ctx, "%s", usermsg.UserMessage(err))
	}
	defer func() {
		if err := db.Close(); err != nil {
			logging.Warnf(ctx, "close database: %v", err)
		}
	}()

	// Start periodic VACUUM when configured (default: every 24 h).
	if cfg.VacuumIntervalHours > 0 {
		db.StartPeriodicVacuum(ctx, time.Duration(cfg.VacuumIntervalHours)*time.Hour)
	}

	// Start periodic history retention pruning (runs daily; no-op when both limits are 0).
	db.StartPeriodicPrune(ctx, 24*time.Hour, cfg.HistoryMaxAgeDays, cfg.HistoryMaxRows)

	// 3. Create CertAuthority.
	ca, err := proxy.NewCertAuthority(cfg.CACertPath, cfg.CAKeyPath)
	if err != nil {
		logging.Errorf(ctx, "create cert authority: %v", err)
		logging.Fatalf(ctx, "%s", usermsg.UserMessage(err))
	}

	// 4. Create breakpoints manager.
	bpManager := breakpoints.NewManager()

	// 5. Create plugin runtime and register built-ins.
	pluginRT := pluginrt.NewRuntime()
	if err := pluginRT.Register(&builtins.HeaderEditor{}); err != nil {
		logging.Warnf(ctx, "register header-editor: %v", err)
	}
	if err := pluginRT.Register(&builtins.MockResponse{}); err != nil {
		logging.Warnf(ctx, "register mock-response: %v", err)
	}
	if err := pluginRT.Register(&builtins.EnvSubst{}); err != nil {
		logging.Warnf(ctx, "register env-subst: %v", err)
	}

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
	<-stop
	logging.Infof(ctx, "Shutting down…")
	cancel()

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
