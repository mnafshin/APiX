package builtins

import (
	"context"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/mnafshin/apix/pkg/plugins"
)

func makeReq(method, rawURL, body string) *plugins.ProxyRequest {
	u, _ := url.Parse(rawURL)
	hdrs := make(map[string][]string)
	return &plugins.ProxyRequest{
		ID:      "test",
		Method:  method,
		URL:     u,
		Headers: hdrs,
		Body:    io.NopCloser(strings.NewReader(body)),
	}
}

func makeResp(status int, body string) *plugins.ProxyResponse {
	return &plugins.ProxyResponse{
		StatusCode: status,
		Headers:    make(map[string][]string),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// ---- HeaderEditor tests ----

func TestHeaderEditorAddHeader(t *testing.T) {
	t.Parallel()
	p := &HeaderEditor{RequestHeaders: map[string]string{"X-Added": "yes"}}
	req := makeReq("GET", "https://example.com", "")

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request, got nil")
	}
	if result.Headers.Get("X-Added") != "yes" {
		t.Errorf("X-Added: got %q", result.Headers.Get("X-Added"))
	}
}

func TestHeaderEditorRemoveHeader(t *testing.T) {
	t.Parallel()
	p := &HeaderEditor{RequestHeaders: map[string]string{"X-Remove": ""}}
	req := makeReq("GET", "https://example.com", "")
	req.Headers.Set("X-Remove", "present")

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request, got nil")
	}
	if val := result.Headers.Get("X-Remove"); val != "" {
		t.Errorf("expected header deleted, got %q", val)
	}
}

func TestHeaderEditorResponseHeaders(t *testing.T) {
	t.Parallel()
	p := &HeaderEditor{ResponseHeaders: map[string]string{"X-Resp": "added", "X-Del": ""}}
	resp := makeResp(200, "body")
	resp.Headers.Set("X-Del", "should-be-deleted")

	req := makeReq("GET", "https://example.com", "")
	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified response, got nil")
	}
	if result.Headers.Get("X-Resp") != "added" {
		t.Errorf("X-Resp: got %q", result.Headers.Get("X-Resp"))
	}
	if val := result.Headers.Get("X-Del"); val != "" {
		t.Errorf("X-Del should be deleted, got %q", val)
	}
}

// ---- EnvSubst tests ----

func TestEnvSubstReplacesVariable(t *testing.T) {
	t.Setenv("FOO", "bar")

	p := &EnvSubst{}
	req := makeReq("POST", "https://example.com", "value is {{FOO}}")

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request, got nil")
	}
	body, _ := io.ReadAll(result.Body)
	if string(body) != "value is bar" {
		t.Errorf("body: got %q want %q", string(body), "value is bar")
	}
}

func TestEnvSubstMissingVariable(t *testing.T) {
	os.Unsetenv("NONEXIST_TEST_VAR_XYZ")

	p := &EnvSubst{}
	req := makeReq("POST", "https://example.com", "value is {{NONEXIST_TEST_VAR_XYZ}}")

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	body, _ := io.ReadAll(result.Body)
	// Implementation keeps the original {{VAR}} when not found.
	if string(body) != "value is {{NONEXIST_TEST_VAR_XYZ}}" {
		t.Errorf("body: got %q want original placeholder", string(body))
	}
}

func TestEnvSubstHeaderValues(t *testing.T) {
	t.Setenv("TOKEN", "secret123")

	p := &EnvSubst{}
	req := makeReq("GET", "https://example.com", "")
	req.Headers.Set("Authorization", "Bearer {{TOKEN}}")

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if got := result.Headers.Get("Authorization"); got != "Bearer secret123" {
		t.Errorf("Authorization: got %q want %q", got, "Bearer secret123")
	}
}

// ---- MockResponse tests ----

func TestMockResponseMatches(t *testing.T) {
	t.Parallel()
	p := &MockResponse{
		URLPattern: `.*api/test.*`,
		StatusCode: 200,
		Headers:    map[string]string{"X-Mocked": "true"},
		Body:       []byte(`{"mocked":true}`),
	}

	req := makeReq("GET", "https://example.com/api/test/resource", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request, got nil")
	}
	if result.MockedResponse == nil {
		t.Fatal("expected MockedResponse to be set")
	}
	if result.MockedResponse.StatusCode != 200 {
		t.Errorf("StatusCode: got %d", result.MockedResponse.StatusCode)
	}
	if result.MockedResponse.Headers.Get("X-Mocked") != "true" {
		t.Errorf("X-Mocked: got %q", result.MockedResponse.Headers.Get("X-Mocked"))
	}
	body, _ := io.ReadAll(result.MockedResponse.Body)
	if string(body) != `{"mocked":true}` {
		t.Errorf("body: got %q", string(body))
	}
}

func TestMockResponseNoMatch(t *testing.T) {
	t.Parallel()
	p := &MockResponse{
		URLPattern: `.*api/test.*`,
		StatusCode: 200,
	}

	req := makeReq("GET", "https://example.com/other/path", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	// Should return nil (pass-through) when URL doesn't match.
	if result != nil {
		t.Error("expected nil (no match), got modified request")
	}
}
