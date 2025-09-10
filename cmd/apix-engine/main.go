package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/server"
)

func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	cfg := config.LoadConfig("internal/config/config.yaml")
	eng := engine.New()

	wg.Add(1)
	go func() {
		defer wg.Done()
		server.StartHTTPProxy(ctx, eng, cfg.HTTPPort)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		server.StartGRPCServer(ctx, eng, cfg.GRPCPort)
	}()

	<-stop
	log.Println("Shutting down servers...")
	cancel()
	wg.Wait()
	log.Println("Servers gracefully stopped")
}