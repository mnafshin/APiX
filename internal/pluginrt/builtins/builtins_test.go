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

// ---- EnvSubst secret-blocking tests ----

func TestEnvSubstBlocksSecretVars(t *testing.T) {
	// Cannot use t.Parallel() here because subtests call t.Setenv
	secrets := []struct {
		name  string
		value string
	}{
		{"API_KEY", "leaked-api-key"},
		{"MY_API_SECRET", "leaked-api-secret"},
		{"ACCESS_TOKEN", "leaked-access-token"},
		{"REFRESH_TOKEN", "leaked-refresh-token"},
		{"PRIVATE_KEY", "leaked-private-key"},
		{"SECRET_KEY", "leaked-secret"},
		{"AWS_SECRET_ACCESS_KEY", "leaked-aws-key"},
		{"GITHUB_TOKEN", "leaked-github-token"},
		{"GITLAB_TOKEN", "leaked-gitlab-token"},
	}

	for _, s := range secrets {
		t.Run(s.name, func(t *testing.T) {
			t.Setenv(s.name, s.value)

			p := &EnvSubst{}
			body := "credential is {{" + s.name + "}}"
			req := makeReq("POST", "https://api.example.com", body)

			result, err := p.OnRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("OnRequest: %v", err)
			}

			got, _ := io.ReadAll(result.Body)
			if string(got) != body {
				t.Errorf("secret var %s should NOT be substituted: got %q", s.name, string(got))
			}
			// Also check headers are not substituted.
			req2 := makeReq("GET", "https://example.com", "")
			req2.Headers.Set("X-Cred", "Bearer {{"+s.name+"}}")
			result2, _ := p.OnRequest(context.Background(), req2)
			if got := result2.Headers.Get("X-Cred"); got != "Bearer {{"+s.name+"}}" {
				t.Errorf("secret var %s should NOT be substituted in headers: got %q", s.name, got)
			}
		})
	}
}

func TestEnvSubstAllowsNonSecretVars(t *testing.T) {
	// Cannot use t.Parallel() here because subtests call t.Setenv
	allowed := []struct {
		name  string
		value string
	}{
		{"BASE_URL", "https://staging.example.com"},
		{"APP_ENV", "staging"},
		{"REGION", "us-east-1"},
		{"TIMEOUT_MS", "5000"},
	}

	for _, v := range allowed {
		t.Run(v.name, func(t *testing.T) {
			t.Setenv(v.name, v.value)

			p := &EnvSubst{}
			req := makeReq("POST", "https://api.example.com", "value={{"+v.name+"}}")

			result, err := p.OnRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("OnRequest: %v", err)
			}

			got, _ := io.ReadAll(result.Body)
			want := "value=" + v.value
			if string(got) != want {
				t.Errorf("non-secret var %s should be substituted: got %q want %q", v.name, string(got), want)
			}
		})
	}
}

func TestEnvSubstMalformedSyntax(t *testing.T) {
	t.Parallel()
	// Unclosed braces, partial syntax — should be left as-is, no panic.
	cases := []string{
		"{{UNCLOSED",
		"{{",
		"{{}}",
		"{NODOUBRACES}",
	}

	p := &EnvSubst{}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			req := makeReq("POST", "https://example.com", body)
			result, err := p.OnRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("OnRequest should not return error for %q: %v", body, err)
			}
			if result == nil {
				t.Fatalf("expected non-nil result for %q", body)
			}
			got, _ := io.ReadAll(result.Body)
			if string(got) != body {
				t.Errorf("body %q: expected unchanged, got %q", body, string(got))
			}
		})
	}
}

// ---- MockResponse custom status code / header tests ----

func TestMockResponse_CustomStatusCodes(t *testing.T) {
	t.Parallel()
	cases := []int{201, 204, 400, 404, 500, 503}

	for _, code := range cases {
		code := code
		t.Run(string(rune('0'+code/100))+"xx", func(t *testing.T) {
			t.Parallel()
			p := &MockResponse{
				URLPattern: `.*`,
				StatusCode: code,
			}
			req := makeReq("GET", "https://example.com/anything", "")
			result, err := p.OnRequest(context.Background(), req)
			if err != nil {
				t.Fatalf("OnRequest: %v", err)
			}
			if result == nil || result.MockedResponse == nil {
				t.Fatalf("expected MockedResponse for status %d", code)
			}
			if result.MockedResponse.StatusCode != code {
				t.Errorf("StatusCode: got %d want %d", result.MockedResponse.StatusCode, code)
			}
		})
	}
}

func TestMockResponse_ResponseHeadersPresent(t *testing.T) {
	t.Parallel()
	p := &MockResponse{
		URLPattern: `.*`,
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json", "X-Custom": "value"},
		Body:       []byte(`{}`),
	}

	req := makeReq("GET", "https://api.example.com/data", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result.MockedResponse.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q", result.MockedResponse.Headers.Get("Content-Type"))
	}
	if result.MockedResponse.Headers.Get("X-Custom") != "value" {
		t.Errorf("X-Custom: got %q", result.MockedResponse.Headers.Get("X-Custom"))
	}
}

// ---- Fuzz tests ----

// FuzzEnvSubst_Body ensures EnvSubst.OnRequest never panics regardless of the
// body content it receives. Seed corpus covers normal placeholders, malformed
// braces, binary data, and very large inputs.
func FuzzEnvSubst_Body(f *testing.F) {
	f.Add(`{"key": "{{HOME}}"}`)
	f.Add(`{{MISSING_VAR}}`)
	f.Add(`{{`)                             // unclosed brace
	f.Add(`{{}}`)                           // empty name
	f.Add(`{{API_KEY}}`)                    // blocked secret — must be left unchanged
	f.Add(`{{AWS_SECRET}}`)                 // blocked secret with prefix
	f.Add(string([]byte{0x00, 0x01, 0xFF})) // binary data
	f.Add(``)                               // empty body

	p := &EnvSubst{}
	f.Fuzz(func(t *testing.T, body string) {
		req := makeReq("POST", "https://example.com/fuzz", body)
		result, err := p.OnRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("OnRequest returned unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("OnRequest returned nil result")
		}
		// Result body must be readable without panic.
		if _, err := io.ReadAll(result.Body); err != nil {
			t.Fatalf("ReadAll on result body failed: %v", err)
		}
	})
}

// FuzzEnvSubst_Headers ensures EnvSubst.OnRequest never panics when header
// values contain arbitrary content, including placeholder-like patterns.
func FuzzEnvSubst_Headers(f *testing.F) {
	f.Add("Authorization", "Bearer {{TOKEN}}")
	f.Add("X-Custom", "{{API_KEY}}")           // blocked secret
	f.Add("Content-Type", "application/{{")    // malformed placeholder
	f.Add("X-Bin", string([]byte{0xFF, 0x00})) // binary header value

	p := &EnvSubst{}
	f.Fuzz(func(t *testing.T, headerName, headerValue string) {
		req := makeReq("GET", "https://example.com/fuzz", "")
		req.Headers[headerName] = []string{headerValue}
		result, err := p.OnRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("OnRequest returned unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("OnRequest returned nil result")
		}
	})
}
