package rewrite

import (
	"bytes"
	"net/http"
	"sort"
	"strings"

	apix "github.com/mnafshin/apix/pkg/api/generated"
)

// matchesRule reports whether req satisfies the MatchCriteria of rule.
// For request-phase matching, statusCode in match criteria is ignored.
func matchesRule(rule *apix.RewriteRule, req *http.Request, reqBody []byte) bool {
	if !rule.Enabled {
		return false
	}
	m := rule.Match
	if m == nil {
		return true
	}
	if m.UrlPattern != "" && !strings.Contains(req.URL.String(), m.UrlPattern) {
		return false
	}
	if m.Method != "" && !strings.EqualFold(req.Method, m.Method) {
		return false
	}
	if m.HeaderName != "" {
		v := req.Header.Get(m.HeaderName)
		if v == "" {
			return false
		}
		if m.HeaderValue != "" && !strings.Contains(v, m.HeaderValue) {
			return false
		}
	}
	if m.BodyPattern != "" && !bytes.Contains(reqBody, []byte(m.BodyPattern)) {
		return false
	}
	return true
}

// matchesResponseRule reports whether the response satisfies the MatchCriteria.
func matchesResponseRule(rule *apix.RewriteRule, req *http.Request, resp *http.Response, respBody []byte) bool {
	if !rule.Enabled {
		return false
	}
	m := rule.Match
	if m == nil {
		return true
	}
	if m.UrlPattern != "" && !strings.Contains(req.URL.String(), m.UrlPattern) {
		return false
	}
	if m.Method != "" && !strings.EqualFold(req.Method, m.Method) {
		return false
	}
	if m.StatusCode != 0 && resp.StatusCode != int(m.StatusCode) {
		return false
	}
	if m.HeaderName != "" {
		v := resp.Header.Get(m.HeaderName)
		if v == "" {
			return false
		}
		if m.HeaderValue != "" && !strings.Contains(v, m.HeaderValue) {
			return false
		}
	}
	if m.BodyPattern != "" && !bytes.Contains(respBody, []byte(m.BodyPattern)) {
		return false
	}
	return true
}

// SyntheticResponse is a short-circuit HTTP response produced by a rewrite rule.
type SyntheticResponse struct {
	StatusCode  int
	Headers     http.Header
	Body        []byte
	RedirectURL string // non-empty for REDIRECT action
}

// ApplyRequestRules applies enabled rewrite rules to an outgoing request in
// priority order. It returns a SyntheticResponse if a terminal action fires
// (REDIRECT, BLOCK, RESPOND), otherwise nil.
// reqBody is the buffered request body (may be nil).
func ApplyRequestRules(rules []*apix.RewriteRule, req *http.Request, reqBody []byte) (*SyntheticResponse, error) {
	sorted := sortedRules(rules)
	for _, rule := range sorted {
		if !matchesRule(rule, req, reqBody) {
			continue
		}
		switch rule.Action {
		case apix.RewriteAction_ADD_REQUEST_HEADER:
			if rule.ParamKey != "" {
				req.Header.Add(rule.ParamKey, rule.ParamValue)
			}
		case apix.RewriteAction_REMOVE_REQUEST_HEADER:
			if rule.ParamKey != "" {
				req.Header.Del(rule.ParamKey)
			}
		case apix.RewriteAction_REPLACE_REQUEST_HEADER:
			if rule.ParamKey != "" {
				req.Header.Set(rule.ParamKey, rule.ParamValue)
			}
		case apix.RewriteAction_REPLACE_REQUEST_BODY:
			if len(rule.BodyTemplate) > 0 {
				reqBody = rule.BodyTemplate
				req.Body = http.NoBody
				req.ContentLength = int64(len(reqBody))
			}
		case apix.RewriteAction_REDIRECT:
			target := rule.ParamValue
			if target == "" {
				target = rule.ParamKey
			}
			hdrs := make(http.Header)
			hdrs.Set("Location", target)
			return &SyntheticResponse{
				StatusCode:  http.StatusFound,
				Headers:     hdrs,
				RedirectURL: target,
			}, nil
		case apix.RewriteAction_BLOCK:
			return &SyntheticResponse{
				StatusCode: http.StatusForbidden,
				Headers:    make(http.Header),
				Body:       []byte("blocked by rewrite rule"),
			}, nil
		case apix.RewriteAction_RESPOND:
			statusCode := int(rule.ResponseStatus)
			if statusCode == 0 {
				statusCode = http.StatusOK
			}
			hdrs := make(http.Header)
			if rule.ResponseContentType != "" {
				hdrs.Set("Content-Type", rule.ResponseContentType)
			}
			return &SyntheticResponse{
				StatusCode: statusCode,
				Headers:    hdrs,
				Body:       rule.ResponseBody,
			}, nil
		}
	}
	return nil, nil
}

// ApplyResponseRules applies enabled rewrite rules to an upstream response in
// priority order. It mutates resp headers in place and returns the (possibly
// modified) body bytes.
func ApplyResponseRules(rules []*apix.RewriteRule, req *http.Request, resp *http.Response, respBody []byte) []byte {
	sorted := sortedRules(rules)
	for _, rule := range sorted {
		if !matchesResponseRule(rule, req, resp, respBody) {
			continue
		}
		switch rule.Action {
		case apix.RewriteAction_ADD_RESPONSE_HEADER:
			if rule.ParamKey != "" {
				resp.Header.Add(rule.ParamKey, rule.ParamValue)
			}
		case apix.RewriteAction_REMOVE_RESPONSE_HEADER:
			if rule.ParamKey != "" {
				resp.Header.Del(rule.ParamKey)
			}
		case apix.RewriteAction_REPLACE_RESPONSE_HEADER:
			if rule.ParamKey != "" {
				resp.Header.Set(rule.ParamKey, rule.ParamValue)
			}
		case apix.RewriteAction_REPLACE_RESPONSE_BODY:
			if len(rule.BodyTemplate) > 0 {
				respBody = rule.BodyTemplate
			}
		}
	}
	return respBody
}

// sortedRules returns a copy of rules sorted by priority ascending (lower = higher priority).
func sortedRules(rules []*apix.RewriteRule) []*apix.RewriteRule {
	sorted := make([]*apix.RewriteRule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}
