package builtins

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mnafshin/apix/pkg/plugins"
)

// JWTAuthConfig holds configuration for the JWTAuthPlugin.
type JWTAuthConfig struct {
	// Secret is the HMAC secret key used to validate HS256/HS384/HS512 tokens.
	Secret string
	// Algorithm is the expected signing algorithm. Defaults to "HS256".
	Algorithm string
	// HeaderName is the request header that carries the token. Defaults to "Authorization".
	// The plugin strips a leading "Bearer " prefix automatically.
	HeaderName string
	// ClaimsToHeaders maps JWT claim names to request header names.
	// For each entry, the claim value is injected as the named header on the request.
	ClaimsToHeaders map[string]string
	// Optional controls behaviour when no token is present.
	// When true the request passes through; when false (default) a 401 is returned.
	Optional bool
}

// JWTAuthPlugin validates incoming JWT bearer tokens and injects selected
// claims as request headers for consumption by upstream services.
// The plugin is read-only after construction and is safe for concurrent use.
type JWTAuthPlugin struct {
	cfg    JWTAuthConfig
	method jwt.SigningMethod
}

// NewJWTAuthPlugin constructs a JWTAuthPlugin from the provided config.
// It returns an error if the algorithm is not one of HS256, HS384, or HS512.
func NewJWTAuthPlugin(cfg JWTAuthConfig) (*JWTAuthPlugin, error) {
	if cfg.Algorithm == "" {
		cfg.Algorithm = "HS256"
	}
	if cfg.HeaderName == "" {
		cfg.HeaderName = "Authorization"
	}

	var method jwt.SigningMethod
	switch cfg.Algorithm {
	case "HS256":
		method = jwt.SigningMethodHS256
	case "HS384":
		method = jwt.SigningMethodHS384
	case "HS512":
		method = jwt.SigningMethodHS512
	default:
		return nil, fmt.Errorf("jwt-auth: unsupported algorithm %q (supported: HS256, HS384, HS512)", cfg.Algorithm)
	}

	return &JWTAuthPlugin{cfg: cfg, method: method}, nil
}

func (p *JWTAuthPlugin) Name() string        { return "jwt-auth" }
func (p *JWTAuthPlugin) Version() string     { return "1.0.0" }
func (p *JWTAuthPlugin) Description() string {
	return "Validate JWT bearer tokens and inject selected claims as request headers."
}

// OnRequest validates the JWT token and injects claims into request headers.
func (p *JWTAuthPlugin) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	raw := req.Headers.Get(p.cfg.HeaderName)
	token := strings.TrimPrefix(raw, "Bearer ")

	if token == "" {
		if p.cfg.Optional {
			return nil, nil
		}
		return p.reject(req, http.StatusUnauthorized, `{"error":"missing token"}`), nil
	}

	parsed, err := jwt.Parse(token,
		func(t *jwt.Token) (interface{}, error) {
			if t.Method.Alg() != p.method.Alg() {
				return nil, fmt.Errorf("unexpected signing algorithm: %s", t.Method.Alg())
			}
			return []byte(p.cfg.Secret), nil
		},
		jwt.WithValidMethods([]string{p.cfg.Algorithm}),
	)
	if err != nil || !parsed.Valid {
		return p.reject(req, http.StatusUnauthorized, `{"error":"invalid token"}`), nil
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || len(p.cfg.ClaimsToHeaders) == 0 {
		return nil, nil
	}

	// Read existing body so the clone can be re-read.
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	clone := req.Clone(io.NopCloser(bytes.NewReader(body)))

	for claimKey, headerName := range p.cfg.ClaimsToHeaders {
		if val, exists := claims[claimKey]; exists {
			clone.Headers.Set(headerName, fmt.Sprintf("%v", val))
		}
	}
	return clone, nil
}

// OnResponse is a no-op; JWT authentication decisions are made at request time.
func (p *JWTAuthPlugin) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	return nil, nil
}

// reject builds a synthetic 401 response attached to a clone of the request.
func (p *JWTAuthPlugin) reject(req *plugins.ProxyRequest, status int, body string) *plugins.ProxyRequest {
	hdrs := http.Header{"Content-Type": []string{"application/json"}}
	clone := req.Clone(req.Body)
	clone.MockedResponse = &plugins.ProxyResponse{
		StatusCode: status,
		Status:     http.StatusText(status),
		Headers:    hdrs,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	return clone
}
