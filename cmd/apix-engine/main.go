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

	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Engine struct {
	mu          sync.Mutex
	requests    []*apix.HttpRequest
	subscribers []chan *apix.HttpRequest
}

func (e *Engine) AddRequest(req *apix.HttpRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = append(e.requests, req)
	for _, sub := range e.subscribers {
		select {
		case sub <- req:
		default:
		}
	}
}

func (e *Engine) Subscribe() chan *apix.HttpRequest {
	ch := make(chan *apix.HttpRequest, 10)
	e.mu.Lock()
	e.subscribers = append(e.subscribers, ch)
	e.mu.Unlock()
	return ch
}

var engine = &Engine{}

type server struct {
	apix.UnimplementedEngineServer
}

func (s *server) GetStatus(ctx context.Context, req *apix.StatusRequest) (*apix.StatusResponse, error) {
	return &apix.StatusResponse{
		Status:  "OK",
		Version: "1.0.0",
	}, nil
}

func (s *server) CaptureTraffic(req *apix.CaptureRequest, stream apix.Engine_CaptureTrafficServer) error {
	ch := engine.Subscribe()
	defer func() {
		engine.mu.Lock()
		for i, sub := range engine.subscribers {
			if sub == ch {
				engine.subscribers = append(engine.subscribers[:i], engine.subscribers[i+1:]...)
				break
			}
		}
		engine.mu.Unlock()
		close(ch)
	}()

	for {
		select {
		case reqInfo, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(reqInfo); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (s *server) ListPlugins(ctx context.Context, req *apix.PluginListRequest) (*apix.PluginListResponse, error) {
	return &apix.PluginListResponse{
		Plugins: []*apix.PluginInfo{
			{
				Name:        "PluginA",
				Description: "Dummy plugin A",
				Version:     "0.1",
			},
			{
				Name:        "PluginB",
				Description: "Dummy plugin B",
				Version:     "0.2",
			},
		},
	}, nil
}

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
					Scheme:   scheme,
					Host:     r.Host,
					Path:     r.URL.Path,
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

			reqInfo := &apix.HttpRequest{
				Method:  r.Method,
				Url:     targetURL.String(),
				Headers: map[string]string{},
			}
			for k, vv := range r.Header {
				if len(vv) > 0 {
					reqInfo.Headers[k] = vv[0]
				}
			}

			engine.AddRequest(reqInfo)
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
		apix.RegisterEngineServer(grpcServer, &server{})
		reflection.Register(grpcServer)
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
