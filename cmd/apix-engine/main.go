package main

import (
	"context"
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
)

func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	// 1. Load config from well-known search paths.
	cfg := config.LoadConfig(config.DefaultPath())

	// 2. Open SQLite database.
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	// 3. Create CertAuthority.
	ca, err := proxy.NewCertAuthority(cfg.CACertPath, cfg.CAKeyPath)
	if err != nil {
		log.Fatalf("create cert authority: %v", err)
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
		SkipTLSVerify: cfg.ReplaySkipTLSVerify,
	})

	// 8. Create TLS + HTTP proxies.
	transportOpts := proxy.TransportOptions{
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     time.Duration(cfg.IdleConnTimeoutSec) * time.Second,
		DialTimeout:         time.Duration(cfg.DialTimeoutSec) * time.Second,
	}
	tlsProxy := proxy.NewTLSProxy(ca, eng, transportOpts)
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
	
	wg.Wait()
	log.Println("Goodbye.")
}