package proxy

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mnafshin/apix/internal/config"
)

func TestValidateInboundRequest(t *testing.T) {
	t.Parallel()

	baseURL, err := url.Parse("http://example.com/path")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	tests := []struct {
		name      string
		cfg       *config.Config
		req       *http.Request
		wantError string
	}{
		{
			name: "valid request",
			cfg: &config.Config{
				MaxHeadersPerRequest: 5,
				MaxHeaderValueBytes:  64,
				MaxTotalHeaderBytes:  256,
				MaxURLLength:         64,
			},
			req: &http.Request{
				URL: baseURL,
				Header: http.Header{
					"X-Test": []string{"ok"},
				},
			},
		},
		{
			name: "url too long",
			cfg: &config.Config{
				MaxURLLength: 8,
			},
			req: &http.Request{
				URL: baseURL,
				Header: http.Header{
					"X-Test": []string{"ok"},
				},
			},
			wantError: "url too long",
		},
		{
			name: "too many header fields",
			cfg: &config.Config{
				MaxHeadersPerRequest: 1,
			},
			req: &http.Request{
				URL: baseURL,
				Header: http.Header{
					"X-One": []string{"1"},
					"X-Two": []string{"2"},
				},
			},
			wantError: "too many header fields",
		},
		{
			name: "header value too large",
			cfg: &config.Config{
				MaxHeaderValueBytes: 3,
			},
			req: &http.Request{
				URL: baseURL,
				Header: http.Header{
					"X-One": []string{"toolong"},
				},
			},
			wantError: "value too large",
		},
		{
			name: "total headers too large",
			cfg: &config.Config{
				MaxTotalHeaderBytes: 10,
			},
			req: &http.Request{
				URL: baseURL,
				Header: http.Header{
					"X-One": []string{"1234567890"},
				},
			},
			wantError: "total headers too large",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateInboundRequest(tc.cfg, tc.req)
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantError)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected error containing %q, got %q", tc.wantError, err.Error())
			}
		})
	}
}
