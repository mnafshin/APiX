package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
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
	flag.Parse()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	// 1. Load config from well-known search paths.
	cfg := config.LoadConfig(config.DefaultPath())

	// If invoked with --config-check bail out after validating the config.
	if *configCheck {
		if err := cfg.Validate(); err != nil {
			log.Printf("config validation failed: %v", err)
			log.Fatalf("%s", usermsg.UserMessage(err))
		}
		log.Println("config: validation passed")
		return
	}

	// 2. Open SQLite database.
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		log.Printf("open database: %v", err)
		log.Fatalf("%s", usermsg.UserMessage(err))
	}
	defer db.Close()

	// 3. Create CertAuthority.
	ca, err := proxy.NewCertAuthority(cfg.CACertPath, cfg.CAKeyPath)
	if err != nil {
		log.Printf("create cert authority: %v", err)
		log.Fatalf("%s", usermsg.UserMessage(err))
	}

	// 4. Create breakpoints manager.
	bpManager := breakpoints.NewManager()

	// 5. Create plugin runtime and register built-ins.
	pluginRT := pluginrt.NewRuntime()
	if err := pluginRT.Register(&builtins.HeaderEditor{}); err != nil {
		log.Printf("register header-editor: %v", err)
	}
	if err := pluginRT.Register(&builtins.MockResponse{}); err != nil {
		log.Printf("register mock-response: %v", err)
	}
	if err := pluginRT.Register(&builtins.EnvSubst{}); err != nil {
		log.Printf("register env-subst: %v", err)
	}

	// 6. Create Engine.
	eng := engine.New(db, bpManager, pluginRT)

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
	httpProxy := proxy.NewHTTPProxy(":"+cfg.HTTPPort, tlsProxy, eng, transportOpts, cfg)

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
			log.Printf("HTTP proxy stopped: %v", err)
		}
	}()

	// 11. Wait for shutdown signal.
	<-stop
	log.Println("Shutting down…")
	cancel()
	
	// 12. Close proxies to release file descriptors.
	httpProxy.Close()
	tlsProxy.Close()
	
	// 13. Wait for all goroutines to finish, with a 15-second timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		log.Println("Goodbye.")
	case <-time.After(15 * time.Second):
		log.Println("FATAL: shutdown timeout exceeded (15s), forcing exit")
		os.Exit(1)
	}
}