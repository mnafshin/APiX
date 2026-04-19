package graphql

import (
	"bytes"
	"encoding/json"
	"strings"
)

type Metadata struct {
	Request  *RequestMetadata
	Response *ResponseMetadata
}

type RequestMetadata struct {
	OperationName  string
	Query          string
	VariablesJSON  string
	IsBatch        bool
	OperationCount int
}

type ResponseMetadata struct {
	Errors []ErrorMetadata
}

type ErrorMetadata struct {
	Message        string
	PathJSON       string
	LocationsJSON  string
	ExtensionsJSON string
	RawJSON        string
}

func Extract(reqHeaders map[string]string, reqBody []byte, respHeaders map[string]string, respBody []byte) *Metadata {
	var out Metadata

	if req := extractRequest(reqHeaders, reqBody); req != nil {
		out.Request = req
	}
	if resp := extractResponse(respHeaders, respBody); resp != nil {
		out.Response = resp
	}
	if out.Request == nil && out.Response == nil {
		return nil
	}
	return &out
}

func extractRequest(headers map[string]string, body []byte) *RequestMetadata {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if !isGraphQLishContentType(headerValue(headers, "content-type")) {
		return nil
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	switch v := payload.(type) {
	case map[string]any:
		op, ok := parseOperation(v)
		if !ok {
			return nil
		}
		op.IsBatch = false
		op.OperationCount = 1
		return &op
	case []any:
		parsed := make([]RequestMetadata, 0, len(v))
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			op, ok := parseOperation(obj)
			if ok {
				parsed = append(parsed, op)
			}
		}
		if len(parsed) == 0 {
			return nil
		}
		first := parsed[0]
		first.IsBatch = true
		first.OperationCount = len(parsed)
		return &first
	default:
		return nil
	}
}

func extractResponse(headers map[string]string, body []byte) *ResponseMetadata {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if !isGraphQLishContentType(headerValue(headers, "content-type")) {
		return nil
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	errs := collectErrors(payload)
	if len(errs) == 0 {
		return nil
	}
	return &ResponseMetadata{Errors: errs}
}

func collectErrors(payload any) []ErrorMetadata {
	switch v := payload.(type) {
	case map[string]any:
		return parseErrorsArray(v["errors"])
	case []any:
		var out []ErrorMetadata
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, parseErrorsArray(obj["errors"])...)
		}
		return out
	default:
		return nil
	}
}

func parseErrorsArray(raw any) []ErrorMetadata {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]ErrorMetadata, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		e := ErrorMetadata{
			Message:        asString(obj["message"]),
			PathJSON:       marshalJSON(obj["path"]),
			LocationsJSON:  marshalJSON(obj["locations"]),
			ExtensionsJSON: marshalJSON(obj["extensions"]),
			RawJSON:        marshalJSON(obj),
		}
		out = append(out, e)
	}
	return out
}

func parseOperation(obj map[string]any) (RequestMetadata, bool) {
	if !hasGraphQLFields(obj) {
		return RequestMetadata{}, false
	}
	return RequestMetadata{
		OperationName: asString(obj["operationName"]),
		Query:         asString(obj["query"]),
		VariablesJSON: marshalJSON(obj["variables"]),
	}, true
}

func hasGraphQLFields(obj map[string]any) bool {
	_, hasQuery := obj["query"]
	_, hasOperationName := obj["operationName"]
	_, hasVariables := obj["variables"]
	return hasQuery || hasOperationName || hasVariables
}

func isGraphQLishContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return false
	}
	return strings.Contains(ct, "json") || strings.Contains(ct, "graphql")
}

func headerValue(headers map[string]string, name string) string {
	if len(headers) == 0 {
		return ""
	}
	if v, ok := headers[name]; ok {
		return v
	}
	needle := strings.ToLower(name)
	for k, v := range headers {
		if strings.ToLower(k) == needle {
			return v
		}
	}
	return ""
}

func asString(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func marshalJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
