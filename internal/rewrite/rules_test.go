package rewrite_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mnafshin/apix/internal/rewrite"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

func makeRule(action apix.RewriteAction, match *apix.MatchCriteria, opts ...func(*apix.RewriteRule)) *apix.RewriteRule {
	r := &apix.RewriteRule{
		Id:       "rule-1",
		Name:     "test",
		Enabled:  true,
		Priority: 100,
		Match:    match,
		Action:   action,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

func newGET(url string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	return req
}

// ---- Request rule tests ----

func TestApplyRequestRules_AddHeader(t *testing.T) {
	req := newGET("http://example.com/api")
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_ADD_REQUEST_HEADER, nil, func(r *apix.RewriteRule) {
		r.ParamKey = "X-Custom"
		r.ParamValue = "hello"
	})}
	synth, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth != nil {
		t.Fatalf("expected no synthetic response")
	}
	if got := req.Header.Get("X-Custom"); got != "hello" {
		t.Errorf("expected X-Custom: hello, got %q", got)
	}
}

func TestApplyRequestRules_RemoveHeader(t *testing.T) {
	req := newGET("http://example.com/api")
	req.Header.Set("Authorization", "Bearer token")
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_REMOVE_REQUEST_HEADER, nil, func(r *apix.RewriteRule) {
		r.ParamKey = "Authorization"
	})}
	_, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("expected Authorization to be removed, got %q", got)
	}
}

func TestApplyRequestRules_ReplaceHeader(t *testing.T) {
	req := newGET("http://example.com/api")
	req.Header.Set("X-Version", "old")
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_REPLACE_REQUEST_HEADER, nil, func(r *apix.RewriteRule) {
		r.ParamKey = "X-Version"
		r.ParamValue = "new"
	})}
	_, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := req.Header.Get("X-Version"); got != "new" {
		t.Errorf("expected X-Version: new, got %q", got)
	}
}

func TestApplyRequestRules_Block(t *testing.T) {
	req := newGET("http://example.com/blocked")
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_BLOCK, nil)}
	synth, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth == nil {
		t.Fatal("expected synthetic response")
	}
	if synth.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", synth.StatusCode)
	}
}

func TestApplyRequestRules_Redirect(t *testing.T) {
	req := newGET("http://example.com/old")
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_REDIRECT, nil, func(r *apix.RewriteRule) {
		r.ParamValue = "http://example.com/new"
	})}
	synth, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth == nil {
		t.Fatal("expected synthetic response")
	}
	if synth.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", synth.StatusCode)
	}
	if synth.Headers.Get("Location") != "http://example.com/new" {
		t.Errorf("unexpected Location: %s", synth.Headers.Get("Location"))
	}
	if synth.RedirectURL != "http://example.com/new" {
		t.Errorf("unexpected RedirectURL: %s", synth.RedirectURL)
	}
}

func TestApplyRequestRules_Respond(t *testing.T) {
	req := newGET("http://example.com/mock")
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_RESPOND, nil, func(r *apix.RewriteRule) {
		r.ResponseStatus = 200
		r.ResponseBody = []byte(`{"ok":true}`)
		r.ResponseContentType = "application/json"
	})}
	synth, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth == nil {
		t.Fatal("expected synthetic response")
	}
	if synth.StatusCode != 200 {
		t.Errorf("expected 200, got %d", synth.StatusCode)
	}
	if !bytes.Equal(synth.Body, []byte(`{"ok":true}`)) {
		t.Errorf("unexpected body: %s", synth.Body)
	}
	if synth.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("unexpected Content-Type: %s", synth.Headers.Get("Content-Type"))
	}
}

func TestApplyRequestRules_MatchURLPattern(t *testing.T) {
	req := newGET("http://example.com/api/v1")
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_BLOCK, &apix.MatchCriteria{
		UrlPattern: "/api/v2",
	})}
	synth, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Pattern doesn't match, so no synthetic response
	if synth != nil {
		t.Errorf("expected no synthetic response for non-matching URL pattern")
	}
}

func TestApplyRequestRules_MatchMethod(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/api", nil)
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_BLOCK, &apix.MatchCriteria{
		Method: "GET",
	})}
	synth, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth != nil {
		t.Errorf("POST request should not match GET method rule")
	}
}

func TestApplyRequestRules_MatchBodyPattern(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/api", bytes.NewBufferString(`{"secret":"value"}`))
	body := []byte(`{"secret":"value"}`)
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_BLOCK, &apix.MatchCriteria{
		BodyPattern: "secret",
	})}
	synth, err := rewrite.ApplyRequestRules(rules, req, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth == nil || synth.StatusCode != http.StatusForbidden {
		t.Errorf("expected block on body pattern match")
	}
}

func TestApplyRequestRules_DisabledRule(t *testing.T) {
	req := newGET("http://example.com/api")
	rule := makeRule(apix.RewriteAction_BLOCK, nil)
	rule.Enabled = false
	synth, err := rewrite.ApplyRequestRules([]*apix.RewriteRule{rule}, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth != nil {
		t.Errorf("disabled rule should not trigger")
	}
}

func TestApplyRequestRules_PriorityOrder(t *testing.T) {
	req := newGET("http://example.com/api")
	// Low priority BLOCK (should fire last but here we test order)
	// High priority ADD header (lower number = higher priority)
	rules := []*apix.RewriteRule{
		{Id: "low", Enabled: true, Priority: 200, Action: apix.RewriteAction_BLOCK},
		{Id: "high", Enabled: true, Priority: 50, Action: apix.RewriteAction_ADD_REQUEST_HEADER, ParamKey: "X-Applied", ParamValue: "yes"},
	}
	// With priority order: high (50) runs first, low (200) BLOCK runs after
	synth, err := rewrite.ApplyRequestRules(rules, req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// BLOCK at priority 200 should still fire after the header rule
	if synth == nil || synth.StatusCode != http.StatusForbidden {
		t.Errorf("expected block from low-priority rule after high-priority header rule")
	}
	// Header should have been set before block
	if req.Header.Get("X-Applied") != "yes" {
		t.Errorf("expected X-Applied header to be set before block")
	}
}

// ---- Response rule tests ----

func TestApplyResponseRules_AddHeader(t *testing.T) {
	req := newGET("http://example.com/api")
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
	}
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_ADD_RESPONSE_HEADER, nil, func(r *apix.RewriteRule) {
		r.ParamKey = "X-Injected"
		r.ParamValue = "true"
	})}
	rewrite.ApplyResponseRules(rules, req, resp, nil)
	if got := resp.Header.Get("X-Injected"); got != "true" {
		t.Errorf("expected X-Injected: true, got %q", got)
	}
}

func TestApplyResponseRules_RemoveHeader(t *testing.T) {
	req := newGET("http://example.com/api")
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Server": []string{"nginx"}},
	}
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_REMOVE_RESPONSE_HEADER, nil, func(r *apix.RewriteRule) {
		r.ParamKey = "Server"
	})}
	rewrite.ApplyResponseRules(rules, req, resp, nil)
	if got := resp.Header.Get("Server"); got != "" {
		t.Errorf("expected Server header removed, got %q", got)
	}
}

func TestApplyResponseRules_ReplaceBody(t *testing.T) {
	req := newGET("http://example.com/api")
	resp := &http.Response{StatusCode: 200, Header: make(http.Header)}
	body := []byte(`original`)
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_REPLACE_RESPONSE_BODY, nil, func(r *apix.RewriteRule) {
		r.BodyTemplate = []byte(`replaced`)
	})}
	result := rewrite.ApplyResponseRules(rules, req, resp, body)
	if !bytes.Equal(result, []byte(`replaced`)) {
		t.Errorf("expected replaced body, got %s", result)
	}
}

func TestApplyResponseRules_StatusCodeMatch(t *testing.T) {
	req := newGET("http://example.com/api")
	resp := &http.Response{StatusCode: 404, Header: make(http.Header)}
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_ADD_RESPONSE_HEADER, &apix.MatchCriteria{
		StatusCode: 200,
	}, func(r *apix.RewriteRule) {
		r.ParamKey = "X-Status"
		r.ParamValue = "ok"
	})}
	rewrite.ApplyResponseRules(rules, req, resp, nil)
	if got := resp.Header.Get("X-Status"); got != "" {
		t.Errorf("status 404 should not match status_code 200 criteria")
	}
}

// ---- Integration: httptest round-trip simulation ----

func TestApplyRequestRules_BlockWritesResponse(t *testing.T) {
	req := newGET("http://example.com/blocked")
	rules := []*apix.RewriteRule{makeRule(apix.RewriteAction_BLOCK, nil)}
	synth, _ := rewrite.ApplyRequestRules(rules, req, nil)
	if synth == nil {
		t.Fatal("expected synthetic response")
	}
	w := httptest.NewRecorder()
	for k, vv := range synth.Headers {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(synth.StatusCode)
	if len(synth.Body) > 0 {
		_, _ = w.Write(synth.Body)
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("blocked")) {
		t.Errorf("expected 'blocked' in body, got %s", w.Body.Bytes())
	}
}
