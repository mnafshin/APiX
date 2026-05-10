package learn

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type Observation struct {
	Method          string
	URL             string
	RequestHeaders  map[string]string
	RequestBody     []byte
	ResponseStatus  int
	ResponseHeaders map[string]string
	ResponseBody    []byte
}

type endpointAggregate struct {
	Method              string
	Path                string
	PathParams          []PathParameter
	QueryValueSamples   map[string][]string
	ObservedCount       int
	AuthObserved        bool
	RequestBodySamples  []any
	ResponseBodySamples map[int][]any
}

// BuildOpenAPISpec infers a draft OpenAPI 3.0 spec from observed traffic.
func BuildOpenAPISpec(observations []Observation, title, version string) map[string]any {
	aggregates := map[string]*endpointAggregate{}
	for _, obs := range observations {
		method := strings.ToLower(strings.TrimSpace(obs.Method))
		if method == "" {
			continue
		}
		path, pathParams := NormalizePath(obs.URL)
		key := method + " " + path
		agg, ok := aggregates[key]
		if !ok {
			agg = &endpointAggregate{
				Method:              method,
				Path:                path,
				PathParams:          pathParams,
				QueryValueSamples:   map[string][]string{},
				ResponseBodySamples: map[int][]any{},
			}
			aggregates[key] = agg
		}
		agg.ObservedCount++
		if hasAuthorizationHeader(obs.RequestHeaders) {
			agg.AuthObserved = true
		}
		collectQuerySamples(agg, obs.URL)
		if parsed := parseJSONBody(obs.RequestBody); parsed != nil {
			agg.RequestBodySamples = append(agg.RequestBodySamples, parsed)
		}
		if parsed := parseJSONBody(obs.ResponseBody); parsed != nil {
			status := obs.ResponseStatus
			if status <= 0 {
				status = 200
			}
			agg.ResponseBodySamples[status] = append(agg.ResponseBodySamples[status], parsed)
		}
	}

	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       title,
			"version":     version,
			"description": "Draft proposal inferred from observed APiX traffic. Review before promotion to source-of-truth contracts.",
		},
		"paths": map[string]any{},
	}

	paths := spec["paths"].(map[string]any)
	securityUsed := false
	keys := make([]string, 0, len(aggregates))
	for key := range aggregates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		agg := aggregates[key]
		item, ok := paths[agg.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[agg.Path] = item
		}
		op := map[string]any{
			"summary": "Draft inferred from " + strconv.Itoa(agg.ObservedCount) + " observed transaction(s)",
		}
		if parameters := buildParameters(agg); len(parameters) > 0 {
			op["parameters"] = parameters
		}
		if len(agg.RequestBodySamples) > 0 {
			op["requestBody"] = map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": inferSchemaFromSamples(agg.RequestBodySamples),
					},
				},
			}
		}
		op["responses"] = buildResponses(agg.ResponseBodySamples)
		if agg.AuthObserved {
			op["security"] = []map[string][]string{{"bearerAuth": []string{}}}
			securityUsed = true
		}
		item[agg.Method] = op
	}

	if securityUsed {
		spec["components"] = map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
		}
	}
	return spec
}

func hasAuthorizationHeader(headers map[string]string) bool {
	for key, value := range headers {
		if strings.EqualFold(key, "authorization") && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func collectQuerySamples(agg *endpointAggregate, rawURL string) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	for key, values := range parsed.Query() {
		agg.QueryValueSamples[key] = append(agg.QueryValueSamples[key], values...)
	}
}

func buildParameters(agg *endpointAggregate) []map[string]any {
	params := make([]map[string]any, 0, len(agg.PathParams)+len(agg.QueryValueSamples))
	for _, p := range agg.PathParams {
		schema := map[string]any{"type": p.Type}
		if p.Format != "" {
			schema["format"] = p.Format
		}
		params = append(params, map[string]any{
			"name":     p.Name,
			"in":       "path",
			"required": true,
			"schema":   schema,
		})
	}

	queryKeys := make([]string, 0, len(agg.QueryValueSamples))
	for key := range agg.QueryValueSamples {
		queryKeys = append(queryKeys, key)
	}
	sort.Strings(queryKeys)
	for _, key := range queryKeys {
		schema := inferScalarSchema(agg.QueryValueSamples[key])
		params = append(params, map[string]any{
			"name":     key,
			"in":       "query",
			"required": false,
			"schema":   schema,
		})
	}
	return params
}

func buildResponses(samples map[int][]any) map[string]any {
	if len(samples) == 0 {
		return map[string]any{
			"default": map[string]any{"description": "Observed response"},
		}
	}
	keys := make([]int, 0, len(samples))
	for status := range samples {
		keys = append(keys, status)
	}
	sort.Ints(keys)

	out := map[string]any{}
	for _, status := range keys {
		entry := map[string]any{
			"description": "Observed response",
		}
		if len(samples[status]) > 0 {
			entry["content"] = map[string]any{
				"application/json": map[string]any{
					"schema": inferSchemaFromSamples(samples[status]),
				},
			}
		}
		out[strconv.Itoa(status)] = entry
	}
	return out
}

func parseJSONBody(raw []byte) any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil
	}
	return parsed
}

func inferSchemaFromSamples(samples []any) map[string]any {
	nonNil := make([]any, 0, len(samples))
	for _, sample := range samples {
		if sample != nil {
			nonNil = append(nonNil, sample)
		}
	}
	if len(nonNil) == 0 {
		return map[string]any{"type": "object"}
	}
	return inferSchemaValue(nonNil)
}

func inferSchemaValue(values []any) map[string]any {
	if len(values) == 0 {
		return map[string]any{"type": "string"}
	}
	allObjects := true
	allArrays := true
	for _, v := range values {
		if _, ok := v.(map[string]any); !ok {
			allObjects = false
		}
		if _, ok := v.([]any); !ok {
			allArrays = false
		}
	}
	if allObjects {
		return inferObjectSchema(values)
	}
	if allArrays {
		return inferArraySchema(values)
	}
	types := uniqueTypes(values)
	if len(types) == 1 {
		return map[string]any{"type": types[0]}
	}
	oneOf := make([]map[string]any, 0, len(types))
	for _, typ := range types {
		oneOf = append(oneOf, map[string]any{"type": typ})
	}
	return map[string]any{"oneOf": oneOf}
}

func inferObjectSchema(values []any) map[string]any {
	propertySamples := map[string][]any{}
	requiredCounts := map[string]int{}
	for _, raw := range values {
		obj := raw.(map[string]any)
		for key, value := range obj {
			propertySamples[key] = append(propertySamples[key], value)
			requiredCounts[key]++
		}
	}

	props := map[string]any{}
	keys := make([]string, 0, len(propertySamples))
	for key := range propertySamples {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	required := make([]string, 0, len(keys))
	for _, key := range keys {
		props[key] = inferSchemaValue(propertySamples[key])
		if requiredCounts[key] == len(values) {
			required = append(required, key)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func inferArraySchema(values []any) map[string]any {
	allItems := make([]any, 0)
	for _, raw := range values {
		items := raw.([]any)
		allItems = append(allItems, items...)
	}
	if len(allItems) == 0 {
		return map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		}
	}
	return map[string]any{
		"type":  "array",
		"items": inferSchemaValue(allItems),
	}
}

func inferScalarSchema(values []string) map[string]any {
	if len(values) == 0 {
		return map[string]any{"type": "string"}
	}
	allInt := true
	allNumber := true
	allBool := true
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			allInt = false
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			allNumber = false
		}
		if value != "true" && value != "false" {
			allBool = false
		}
	}
	switch {
	case allInt:
		return map[string]any{"type": "integer"}
	case allNumber:
		return map[string]any{"type": "number"}
	case allBool:
		return map[string]any{"type": "boolean"}
	default:
		return map[string]any{"type": "string"}
	}
}

func uniqueTypes(values []any) []string {
	set := map[string]bool{}
	for _, v := range values {
		set[valueType(v)] = true
	}
	out := make([]string, 0, len(set))
	for typ := range set {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

func valueType(v any) string {
	switch v := v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		if v == float64(int64(v)) {
			return "integer"
		}
		return "number"
	default:
		return "string"
	}
}
