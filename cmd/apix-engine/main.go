package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
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

		transport := &http.Transport{}

		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log.Printf("HTTP proxy received request: %s %s", r.Method, r.URL)

			// Clone the request URL to modify it for the proxy request
			targetURL := r.URL
			if !targetURL.IsAbs() {
				// If URL is not absolute, construct it from Host and Scheme
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				targetURL = &url.URL{
					Scheme: scheme,
					Host:   r.Host,
					Path:   r.URL.Path,
					RawQuery: r.URL.RawQuery,
				}
			}

			// Create a new request to the target URL
			req, err := http.NewRequest(r.Method, targetURL.String(), r.Body)
			if err != nil {
				http.Error(w, "Failed to create request", http.StatusInternalServerError)
				log.Printf("Failed to create request: %v", err)
				return
			}

			// Copy headers
			req.Header = make(http.Header)
			for k, vv := range r.Header {
				for _, v := range vv {
					req.Header.Add(k, v)
				}
			}

			// Use the transport to perform the request
			resp, err := transport.RoundTrip(req)
			if err != nil {
				http.Error(w, "Failed to reach destination", http.StatusBadGateway)
				log.Printf("Failed to reach destination %s: %v", targetURL.String(), err)
				return
			}
			defer resp.Body.Close()

			// Copy response headers
			for k, vv := range resp.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}

			w.WriteHeader(resp.StatusCode)

			// Stream response body
			_, err = io.Copy(w, resp.Body)
			if err != nil {
				log.Printf("Error copying response body: %v", err)
			}
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
