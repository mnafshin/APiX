package contracts

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func ExportOpenAPI31(c *Contract) (map[string]any, []Diagnostic) {
	diags := make([]Diagnostic, 0)
	addDiag := func(path, msg string) {
		diags = append(diags, Diagnostic{Path: path, Message: msg})
	}

	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       c.Info.Title,
			"version":     c.Info.Version,
			"description": c.Info.Description,
		},
		"paths": map[string]any{},
	}
	if len(c.Servers) > 0 {
		servers := make([]map[string]any, 0, len(c.Servers))
		for _, s := range c.Servers {
			servers = append(servers, map[string]any{"url": s.URL, "description": s.Description})
		}
		doc["servers"] = servers
	}

	securitySchemes := map[string]any{}
	paths := doc["paths"].(map[string]any)
	for epi, ep := range c.Endpoints {
		pathItem := map[string]any{}
		methods := sortedOperationMethods(ep.Operations)
		for _, method := range methods {
			op := ep.Operations[method]
			opOut := map[string]any{}
			if op.Summary != "" {
				opOut["summary"] = op.Summary
			}
			if op.Description != "" {
				opOut["description"] = op.Description
			}
			if len(op.Parameters) > 0 {
				opOut["parameters"] = exportOpenAPIParameters(op.Parameters)
			}
			if op.RequestBody != nil {
				opOut["requestBody"] = exportOpenAPIRequestBody(op.RequestBody)
			}
			opOut["responses"] = exportOpenAPIResponses(op.Responses)
			if len(op.Examples) > 0 {
				opOut["x-apix-examples"] = op.Examples
			}
			if len(op.Auth) > 0 {
				sec, schemes := exportOpenAPISecurity(op.Auth, fmt.Sprintf("endpoints[%d].operations.%s.auth", epi, method), addDiag)
				if len(sec) > 0 {
					opOut["security"] = sec
				}
				for name, value := range schemes {
					securitySchemes[name] = value
				}
			}
			pathItem[strings.ToLower(method)] = opOut
		}
		paths[ep.Path] = pathItem
	}
	if len(securitySchemes) > 0 {
		doc["components"] = map[string]any{
			"securitySchemes": securitySchemes,
		}
	}
	sortDiagnostics(diags)
	return doc, diags
}

func ImportOpenAPI31Bytes(path string, data []byte) (*Contract, []Diagnostic, error) {
	var doc map[string]any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, nil, fmt.Errorf("parse openapi json: %w", err)
		}
	default:
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, nil, fmt.Errorf("parse openapi yaml: %w", err)
		}
	}
	return ImportOpenAPI31(doc)
}

func ImportOpenAPI31(doc map[string]any) (*Contract, []Diagnostic, error) {
	if doc == nil {
		return nil, nil, fmt.Errorf("openapi document is empty")
	}
	diags := make([]Diagnostic, 0)
	addDiag := func(path, msg string) {
		diags = append(diags, Diagnostic{Path: path, Message: msg})
	}

	specVersion, _ := doc["openapi"].(string)
	if !strings.HasPrefix(specVersion, "3.1.") {
		addDiag("openapi", "expected OpenAPI 3.1.x")
	}

	c := &Contract{
		SchemaVersion: CurrentSchemaVersion,
		Info: Info{
			Title:   "Imported API",
			Version: "0.1.0",
		},
		Endpoints: make([]Endpoint, 0),
	}
	if info, ok := doc["info"].(map[string]any); ok {
		if title, ok := info["title"].(string); ok && strings.TrimSpace(title) != "" {
			c.Info.Title = title
		}
		if version, ok := info["version"].(string); ok && strings.TrimSpace(version) != "" {
			c.Info.Version = version
		}
		if description, ok := info["description"].(string); ok {
			c.Info.Description = description
		}
	}
	if servers, ok := doc["servers"].([]any); ok {
		for i, raw := range servers {
			srv, ok := raw.(map[string]any)
			if !ok {
				addDiag(fmt.Sprintf("servers[%d]", i), "must be an object")
				continue
			}
			url, _ := srv["url"].(string)
			if strings.TrimSpace(url) == "" {
				addDiag(fmt.Sprintf("servers[%d].url", i), "is required")
				continue
			}
			c.Servers = append(c.Servers, Server{
				URL:         url,
				Description: asString(srv["description"]),
			})
		}
	}

	securitySchemes := map[string]any{}
	if components, ok := doc["components"].(map[string]any); ok {
		if schemas, ok := components["schemas"].(map[string]any); ok && len(schemas) > 0 {
			addDiag("components.schemas", "is not currently mapped into APiX contracts and is ignored")
		}
		if links, ok := components["links"].(map[string]any); ok && len(links) > 0 {
			addDiag("components.links", "is not currently mapped into APiX contracts and is ignored")
		}
		if ss, ok := components["securitySchemes"].(map[string]any); ok {
			securitySchemes = ss
		}
	}

	pathsRaw, ok := doc["paths"].(map[string]any)
	if !ok {
		addDiag("paths", "is required and must be an object")
		sortDiagnostics(diags)
		return c, diags, nil
	}
	pathKeys := make([]string, 0, len(pathsRaw))
	for k := range pathsRaw {
		pathKeys = append(pathKeys, k)
	}
	sort.Strings(pathKeys)
	for _, p := range pathKeys {
		pathItem, ok := pathsRaw[p].(map[string]any)
		if !ok {
			addDiag("paths."+p, "must be an object")
			continue
		}
		ep := Endpoint{Path: p, Operations: map[string]Operation{}}
		for _, m := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
			rawOp, exists := pathItem[m]
			if !exists {
				continue
			}
			op, opDiags := importOpenAPIOperation(rawOp, p, strings.ToUpper(m), securitySchemes)
			diags = append(diags, opDiags...)
			ep.Operations[strings.ToUpper(m)] = op
		}
		if len(ep.Operations) == 0 {
			addDiag("paths."+p, "contains no supported HTTP operations")
			continue
		}
		c.Endpoints = append(c.Endpoints, ep)
	}
	sortDiagnostics(diags)
	return c, diags, nil
}

func exportOpenAPIParameters(params []Parameter) []map[string]any {
	out := make([]map[string]any, 0, len(params))
	for _, p := range params {
		entry := map[string]any{
			"name":        p.Name,
			"in":          p.In,
			"required":    p.Required,
			"description": p.Description,
			"schema": map[string]any{
				"type":   p.Schema.Type,
				"format": p.Schema.Format,
			},
		}
		out = append(out, entry)
	}
	return out
}

func exportOpenAPIRequestBody(body *MediaTypeSchema) map[string]any {
	ctype := body.ContentType
	if ctype == "" {
		ctype = "application/json"
	}
	content := map[string]any{
		ctype: map[string]any{
			"schema": body.Schema,
		},
	}
	if len(body.Examples) > 0 {
		content[ctype].(map[string]any)["examples"] = body.Examples
	}
	return map[string]any{
		"required": true,
		"content":  content,
	}
}

func exportOpenAPIResponses(responses map[string]Result) map[string]any {
	keys := make([]string, 0, len(responses))
	for code := range responses {
		keys = append(keys, code)
	}
	sort.Strings(keys)

	out := make(map[string]any, len(responses))
	for _, code := range keys {
		resp := responses[code]
		entry := map[string]any{"description": resp.Description}
		if resp.Body != nil {
			ctype := resp.Body.ContentType
			if ctype == "" {
				ctype = "application/json"
			}
			contentEntry := map[string]any{
				"schema": resp.Body.Schema,
			}
			if len(resp.Body.Examples) > 0 {
				contentEntry["examples"] = resp.Body.Examples
			}
			entry["content"] = map[string]any{ctype: contentEntry}
		}
		out[code] = entry
	}
	return out
}

func exportOpenAPISecurity(auth []AuthRequirement, path string, addDiag func(string, string)) ([]map[string][]string, map[string]any) {
	security := make([]map[string][]string, 0, len(auth))
	schemes := make(map[string]any, len(auth))
	for i, req := range auth {
		if req.Type == "none" {
			security = append(security, map[string][]string{})
			continue
		}
		name := fmt.Sprintf("apixAuth%d", i+1)
		switch req.Type {
		case "apiKey":
			schemes[name] = map[string]any{"type": "apiKey", "name": "X-API-Key", "in": "header"}
		case "http":
			scheme := req.Scheme
			if scheme == "" {
				scheme = "bearer"
			}
			schemes[name] = map[string]any{"type": "http", "scheme": scheme}
		case "oauth2":
			schemes[name] = map[string]any{
				"type": "oauth2",
				//nolint:gosec // clientCredentials is an OpenAPI field name, not a credential
				"flows": map[string]any{"clientCredentials": map[string]any{"tokenUrl": "https://example.com/oauth/token", "scopes": map[string]any{}}},
			}
		case "mtls":
			schemes[name] = map[string]any{"type": "mutualTLS"}
		default:
			addDiag(fmt.Sprintf("%s[%d].type", path, i), fmt.Sprintf("unsupported auth type %q; dropped", req.Type))
			continue
		}
		security = append(security, map[string][]string{name: req.Scopes})
	}
	return security, schemes
}

func importOpenAPIOperation(raw any, path, method string, securitySchemes map[string]any) (Operation, []Diagnostic) {
	diags := make([]Diagnostic, 0)
	addDiag := func(p, msg string) {
		diags = append(diags, Diagnostic{Path: p, Message: msg})
	}
	opMap, ok := raw.(map[string]any)
	if !ok {
		addDiag(fmt.Sprintf("paths.%s.%s", path, strings.ToLower(method)), "must be an object")
		return Operation{}, diags
	}

	op := Operation{
		Summary:     asString(opMap["summary"]),
		Description: asString(opMap["description"]),
		Responses:   map[string]Result{},
	}
	if params, ok := opMap["parameters"].([]any); ok {
		for i, p := range params {
			pm, ok := p.(map[string]any)
			if !ok {
				addDiag(fmt.Sprintf("paths.%s.%s.parameters[%d]", path, strings.ToLower(method), i), "must be an object")
				continue
			}
			schemaMap, _ := pm["schema"].(map[string]any)
			op.Parameters = append(op.Parameters, Parameter{
				Name:        asString(pm["name"]),
				In:          asString(pm["in"]),
				Required:    asBool(pm["required"]),
				Description: asString(pm["description"]),
				Schema: JSONType{
					Type:   asString(schemaMap["type"]),
					Format: asString(schemaMap["format"]),
				},
			})
			if oneOf, ok := schemaMap["oneOf"].([]any); ok && len(oneOf) > 0 {
				addDiag(fmt.Sprintf("paths.%s.%s.parameters[%d].schema.oneOf", path, strings.ToLower(method), i), "oneOf is not mapped and is ignored")
			}
		}
	}
	if requestBody, ok := opMap["requestBody"].(map[string]any); ok {
		op.RequestBody = importMediaTypeSchema(requestBody, fmt.Sprintf("paths.%s.%s.requestBody", path, strings.ToLower(method)), addDiag)
	}
	if responses, ok := opMap["responses"].(map[string]any); ok {
		for code, r := range responses {
			respMap, ok := r.(map[string]any)
			if !ok {
				addDiag(fmt.Sprintf("paths.%s.%s.responses.%s", path, strings.ToLower(method), code), "must be an object")
				continue
			}
			result := Result{Description: asString(respMap["description"])}
			if content, ok := respMap["content"].(map[string]any); ok {
				result.Body = importMediaTypeSchema(map[string]any{"content": content}, fmt.Sprintf("paths.%s.%s.responses.%s.content", path, strings.ToLower(method), code), addDiag)
			}
			op.Responses[code] = result
		}
	}
	if sec, ok := opMap["security"].([]any); ok {
		op.Auth = importOpenAPISecurity(sec, securitySchemes, fmt.Sprintf("paths.%s.%s.security", path, strings.ToLower(method)), addDiag)
	}
	if examples, ok := opMap["x-apix-examples"].([]any); ok {
		for _, rawExample := range examples {
			em, ok := rawExample.(map[string]any)
			if !ok {
				continue
			}
			op.Examples = append(op.Examples, Example{
				Name:        asString(em["name"]),
				Description: asString(em["description"]),
				Request:     em["request"],
				Response:    em["response"],
			})
		}
	}
	if callbacks, ok := opMap["callbacks"].(map[string]any); ok && len(callbacks) > 0 {
		addDiag(fmt.Sprintf("paths.%s.%s.callbacks", path, strings.ToLower(method)), "callbacks are not mapped and are ignored")
	}
	sortDiagnostics(diags)
	return op, diags
}

func importMediaTypeSchema(raw map[string]any, path string, addDiag func(string, string)) *MediaTypeSchema {
	content, ok := raw["content"].(map[string]any)
	if !ok || len(content) == 0 {
		return nil
	}
	mediaTypes := make([]string, 0, len(content))
	for ctype := range content {
		mediaTypes = append(mediaTypes, ctype)
	}
	sort.Strings(mediaTypes)
	firstType := mediaTypes[0]
	firstRaw, _ := content[firstType].(map[string]any)
	body := &MediaTypeSchema{
		ContentType: firstType,
	}
	if schema, ok := firstRaw["schema"].(map[string]any); ok {
		body.Schema = schema
	}
	if examples, ok := firstRaw["examples"].(map[string]any); ok {
		body.Examples = make(map[string]ExampleData, len(examples))
		for name, rawExample := range examples {
			exMap, ok := rawExample.(map[string]any)
			if !ok {
				continue
			}
			body.Examples[name] = ExampleData{
				Summary: asString(exMap["summary"]),
				Value:   exMap["value"],
			}
		}
	}
	if len(mediaTypes) > 1 {
		addDiag(path+".content", fmt.Sprintf("multiple media types found; only %q is imported", firstType))
	}
	return body
}

func importOpenAPISecurity(raw []any, schemes map[string]any, path string, addDiag func(string, string)) []AuthRequirement {
	out := make([]AuthRequirement, 0, len(raw))
	for i, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			addDiag(fmt.Sprintf("%s[%d]", path, i), "must be an object")
			continue
		}
		if len(entry) == 0 {
			out = append(out, AuthRequirement{Type: "none"})
			continue
		}
		for name, rawScopes := range entry {
			scopeStrings := make([]string, 0)
			if scopes, ok := rawScopes.([]any); ok {
				for _, s := range scopes {
					scopeStrings = append(scopeStrings, asString(s))
				}
			}
			scheme, _ := schemes[name].(map[string]any)
			schemeType := asString(scheme["type"])
			switch schemeType {
			case "apiKey":
				out = append(out, AuthRequirement{Type: "apiKey", Scopes: scopeStrings})
			case "http":
				out = append(out, AuthRequirement{Type: "http", Scheme: asString(scheme["scheme"]), Scopes: scopeStrings})
			case "oauth2":
				out = append(out, AuthRequirement{Type: "oauth2", Scopes: scopeStrings})
			case "mutualTLS":
				out = append(out, AuthRequirement{Type: "mtls", Scopes: scopeStrings})
			default:
				addDiag(fmt.Sprintf("%s[%d].%s", path, i, name), fmt.Sprintf("security scheme type %q is not mapped", schemeType))
			}
		}
	}
	return out
}

func sortDiagnostics(diags []Diagnostic) {
	sort.Slice(diags, func(i, j int) bool {
		if diags[i].Path == diags[j].Path {
			return diags[i].Message < diags[j].Message
		}
		return diags[i].Path < diags[j].Path
	})
}

func sortedOperationMethods(ops map[string]Operation) []string {
	keys := make([]string, 0, len(ops))
	for m := range ops {
		keys = append(keys, m)
	}
	sort.Strings(keys)
	return keys
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
