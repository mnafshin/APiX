package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"google.golang.org/grpc"
)

// Placeholder gRPC service implementation
type server struct{}

func main() {
	// Channel to listen for interrupt or termination signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// WaitGroup to wait for servers to shut down
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	// Start HTTP proxy server
	wg.Add(1)
	go func() {
		defer wg.Done()
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log.Printf("HTTP proxy received request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Proxy server placeholder\n"))
		})
		srv := &http.Server{Addr: ":8080"}

		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()

		log.Println("Starting HTTP proxy server on :8080")
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
		log.Println("HTTP proxy server stopped")
	}()

	// Start gRPC server
	wg.Add(1)
	go func() {
		defer wg.Done()
		lis, err := net.Listen("tcp", ":9090")
		if err != nil {
			log.Fatalf("Failed to listen on :9090: %v", err)
		}
		grpcServer := grpc.NewServer()
		// Register your gRPC service here if you have one
		go func() {
			<-ctx.Done()
			grpcServer.GracefulStop()
		}()

		log.Println("Starting gRPC server on :9090")
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
		log.Println("gRPC server stopped")
	}()

	// Wait for interrupt signal
	<-stop
	log.Println("Shutting down servers...")
	cancel()
	wg.Wait()
	log.Println("Servers gracefully stopped")
}
