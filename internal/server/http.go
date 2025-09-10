package server

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"

	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/internal/engine"
)

func StartHTTPProxy(ctx context.Context, eng *engine.Engine, port string) {
	transport := &http.Transport{}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("HTTP proxy received request: %s %s", r.Method, r.URL)

		targetURL := r.URL
		if !targetURL.IsAbs() {
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

		req, err := http.NewRequest(r.Method, targetURL.String(), r.Body)
		if err != nil {
			http.Error(w, "Failed to create request", http.StatusInternalServerError)
			log.Printf("Failed to create request: %v", err)
			return
		}

		req.Header = r.Header.Clone()
		resp, err := transport.RoundTrip(req)
		if err != nil {
			http.Error(w, "Failed to reach destination", http.StatusBadGateway)
			log.Printf("Failed to reach destination %s: %v", targetURL.String(), err)
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)

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
		eng.AddRequest(reqInfo)
	})

	srv := &http.Server{Addr: ":" + port}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	log.Printf("Starting HTTP proxy server on :%s", port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
	log.Println("HTTP proxy server stopped")
}