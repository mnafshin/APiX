package builtins

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mnafshin/apix/pkg/plugins"
)

// signHS256 creates a signed HS256 JWT with the given claims and secret.
func signHS256(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("signHS256: %v", err)
	}
	return s
}

func TestJWTAuth_ValidToken_PassesThrough(t *testing.T) {
	t.Parallel()
	p, err := NewJWTAuthPlugin(JWTAuthConfig{
		Secret:          "mysecret",
		ClaimsToHeaders: map[string]string{"sub": "X-User-ID", "role": "X-User-Role"},
	})
	if err != nil {
		t.Fatalf("NewJWTAuthPlugin: %v", err)
	}

	claims := jwt.MapClaims{
		"sub":  "user-42",
		"role": "admin",
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	token := signHS256(t, "mysecret", claims)

	req := makeReq("GET", "https://example.com/api", "")
	req.Headers.Set("Authorization", "Bearer "+token)

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request (claims injected), got nil")
	}
	if result.MockedResponse != nil {
		t.Fatalf("expected pass-through, got rejection status %d", result.MockedResponse.StatusCode)
	}
	if got := result.Headers.Get("X-User-ID"); got != "user-42" {
		t.Errorf("X-User-ID: got %q want %q", got, "user-42")
	}
	if got := result.Headers.Get("X-User-Role"); got != "admin" {
		t.Errorf("X-User-Role: got %q want %q", got, "admin")
	}
}

func TestJWTAuth_MissingToken_NotOptional_Returns401(t *testing.T) {
	t.Parallel()
	p, err := NewJWTAuthPlugin(JWTAuthConfig{Secret: "s", Optional: false})
	if err != nil {
		t.Fatalf("NewJWTAuthPlugin: %v", err)
	}

	req := makeReq("GET", "https://example.com/", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected 401 rejection, got nil")
	}
	if result.MockedResponse.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode: got %d want 401", result.MockedResponse.StatusCode)
	}
	body, _ := io.ReadAll(result.MockedResponse.Body)
	if string(body) != `{"error":"missing token"}` {
		t.Errorf("body: got %q", string(body))
	}
}

func TestJWTAuth_MissingToken_Optional_PassesThrough(t *testing.T) {
	t.Parallel()
	p, err := NewJWTAuthPlugin(JWTAuthConfig{Secret: "s", Optional: true})
	if err != nil {
		t.Fatalf("NewJWTAuthPlugin: %v", err)
	}

	req := makeReq("GET", "https://example.com/", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil (pass-through), got %+v", result)
	}
}

func TestJWTAuth_ExpiredToken_Returns401(t *testing.T) {
	t.Parallel()
	p, err := NewJWTAuthPlugin(JWTAuthConfig{Secret: "mysecret"})
	if err != nil {
		t.Fatalf("NewJWTAuthPlugin: %v", err)
	}

	claims := jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(-time.Hour).Unix(), // already expired
	}
	token := signHS256(t, "mysecret", claims)

	req := makeReq("GET", "https://example.com/", "")
	req.Headers.Set("Authorization", "Bearer "+token)

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected 401 rejection for expired token")
	}
	if result.MockedResponse.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode: got %d want 401", result.MockedResponse.StatusCode)
	}
	body, _ := io.ReadAll(result.MockedResponse.Body)
	if string(body) != `{"error":"invalid token"}` {
		t.Errorf("body: got %q", string(body))
	}
}

func TestJWTAuth_InvalidSignature_Returns401(t *testing.T) {
	t.Parallel()
	p, err := NewJWTAuthPlugin(JWTAuthConfig{Secret: "correct-secret"})
	if err != nil {
		t.Fatalf("NewJWTAuthPlugin: %v", err)
	}

	// Sign with a different secret.
	claims := jwt.MapClaims{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()}
	token := signHS256(t, "wrong-secret", claims)

	req := makeReq("GET", "https://example.com/", "")
	req.Headers.Set("Authorization", "Bearer "+token)

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected 401 for invalid signature")
	}
	if result.MockedResponse.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode: got %d want 401", result.MockedResponse.StatusCode)
	}
}

func TestJWTAuth_WrongAlgorithmRejected(t *testing.T) {
	t.Parallel()
	// Plugin configured for HS256; token signed with HS512 must be rejected.
	p, err := NewJWTAuthPlugin(JWTAuthConfig{Secret: "mysecret", Algorithm: "HS256"})
	if err != nil {
		t.Fatalf("NewJWTAuthPlugin: %v", err)
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	token, err := tok.SignedString([]byte("mysecret"))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	req := makeReq("GET", "https://example.com/", "")
	req.Headers.Set("Authorization", "Bearer "+token)

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected 401 for wrong algorithm")
	}
	if result.MockedResponse.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode: got %d want 401", result.MockedResponse.StatusCode)
	}
}

func TestJWTAuth_UnsupportedAlgorithmConfig_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := NewJWTAuthPlugin(JWTAuthConfig{Secret: "s", Algorithm: "RS256"})
	if err == nil {
		t.Fatal("expected error for unsupported algorithm RS256")
	}
}

func TestJWTAuth_CustomHeaderName(t *testing.T) {
	t.Parallel()
	p, err := NewJWTAuthPlugin(JWTAuthConfig{
		Secret:     "secret",
		HeaderName: "X-Api-Token",
	})
	if err != nil {
		t.Fatalf("NewJWTAuthPlugin: %v", err)
	}

	claims := jwt.MapClaims{"sub": "u", "exp": time.Now().Add(time.Hour).Unix()}
	token := signHS256(t, "secret", claims)

	req := makeReq("GET", "https://example.com/", "")
	req.Headers.Set("X-Api-Token", token) // no "Bearer " prefix

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	// No claims-to-headers configured → OnRequest returns nil (pass-through).
	if result != nil && result.MockedResponse != nil {
		t.Fatalf("expected pass-through, got rejection %d", result.MockedResponse.StatusCode)
	}
}

func TestJWTAuth_OnResponse_IsNoop(t *testing.T) {
	t.Parallel()
	p, _ := NewJWTAuthPlugin(JWTAuthConfig{Secret: "s"})
	req := makeReq("GET", "https://example.com/", "")
	resp := &plugins.ProxyResponse{StatusCode: 200, Headers: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}

	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result != nil {
		t.Error("OnResponse: expected nil, got non-nil")
	}
}
