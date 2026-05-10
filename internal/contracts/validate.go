package contracts

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	validMethods = map[string]struct{}{
		"GET": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {}, "HEAD": {}, "OPTIONS": {},
	}
	validParamIn = map[string]struct{}{
		"path": {}, "query": {}, "header": {}, "cookie": {},
	}
	validJSONTypes = map[string]struct{}{
		"string": {}, "integer": {}, "number": {}, "boolean": {}, "object": {}, "array": {},
	}
	validAuthTypes = map[string]struct{}{
		"none": {}, "apiKey": {}, "http": {}, "oauth2": {}, "mtls": {},
	}
	pathParamPattern = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)
	statusCodeRegex  = regexp.MustCompile(`^[1-5][0-9][0-9]$`)
)

type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ValidationError struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (e *ValidationError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "contract validation failed"
	}
	lines := make([]string, 0, len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		lines = append(lines, fmt.Sprintf("%s: %s", d.Path, d.Message))
	}
	return "contract validation failed:\n  - " + strings.Join(lines, "\n  - ")
}

func (e *ValidationError) HasErrors() bool {
	return len(e.Diagnostics) > 0
}

func Validate(c *Contract) *ValidationError {
	diags := make([]Diagnostic, 0)
	add := func(path, message string) {
		diags = append(diags, Diagnostic{Path: path, Message: message})
	}

	if c == nil {
		return &ValidationError{Diagnostics: []Diagnostic{{Path: "contract", Message: "contract is required"}}}
	}
	if strings.TrimSpace(c.SchemaVersion) == "" {
		add("schema_version", "is required")
	} else if c.SchemaVersion != CurrentSchemaVersion {
		add("schema_version", fmt.Sprintf("must be %q", CurrentSchemaVersion))
	}
	if strings.TrimSpace(c.Info.Title) == "" {
		add("info.title", "is required")
	}
	if strings.TrimSpace(c.Info.Version) == "" {
		add("info.version", "is required")
	}
	if len(c.Endpoints) == 0 {
		add("endpoints", "at least one endpoint is required")
	}

	seenPaths := make(map[string]int, len(c.Endpoints))
	for i, ep := range c.Endpoints {
		base := fmt.Sprintf("endpoints[%d]", i)
		if strings.TrimSpace(ep.Path) == "" {
			add(base+".path", "is required")
			continue
		}
		if !strings.HasPrefix(ep.Path, "/") {
			add(base+".path", "must start with '/'")
		}
		if prev, ok := seenPaths[ep.Path]; ok {
			add(base+".path", fmt.Sprintf("duplicates endpoints[%d].path", prev))
		} else {
			seenPaths[ep.Path] = i
		}
		if len(ep.Operations) == 0 {
			add(base+".operations", "at least one operation is required")
			continue
		}
		validateOperations(add, base, ep.Path, ep.Operations)
	}

	sort.Slice(diags, func(i, j int) bool {
		if diags[i].Path == diags[j].Path {
			return diags[i].Message < diags[j].Message
		}
		return diags[i].Path < diags[j].Path
	})
	return &ValidationError{Diagnostics: diags}
}

func validateOperations(add func(string, string), basePath, endpointPath string, operations map[string]Operation) {
	pathParams := make(map[string]struct{})
	matches := pathParamPattern.FindAllStringSubmatch(endpointPath, -1)
	for _, m := range matches {
		if len(m) == 2 {
			pathParams[m[1]] = struct{}{}
		}
	}

	for method, op := range operations {
		opPath := fmt.Sprintf("%s.operations.%s", basePath, method)
		normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
		if normalizedMethod == "" {
			add(opPath, "method key is required")
		} else if _, ok := validMethods[normalizedMethod]; !ok {
			add(opPath, fmt.Sprintf("unsupported HTTP method %q", method))
		}

		if len(op.Responses) == 0 {
			add(opPath+".responses", "at least one response is required")
		}
		for code, resp := range op.Responses {
			respPath := fmt.Sprintf("%s.responses.%s", opPath, code)
			if code != "default" && !statusCodeRegex.MatchString(code) {
				add(respPath, "status code must be 3 digits (100-599) or 'default'")
			}
			if strings.TrimSpace(resp.Description) == "" {
				add(respPath+".description", "is required")
			}
		}

		paramKeys := make(map[string]struct{})
		pathParamDefinitions := make(map[string]struct{})
		for i, p := range op.Parameters {
			paramPath := fmt.Sprintf("%s.parameters[%d]", opPath, i)
			if strings.TrimSpace(p.Name) == "" {
				add(paramPath+".name", "is required")
			}
			if _, ok := validParamIn[p.In]; !ok {
				add(paramPath+".in", "must be one of: path, query, header, cookie")
			}
			if _, ok := validJSONTypes[p.Schema.Type]; !ok {
				add(paramPath+".schema.type", "must be one of: string, integer, number, boolean, object, array")
			}
			key := p.In + ":" + p.Name
			if _, ok := paramKeys[key]; ok {
				add(paramPath, fmt.Sprintf("duplicate parameter %q", key))
			} else {
				paramKeys[key] = struct{}{}
			}
			if p.In == "path" {
				if !p.Required {
					add(paramPath+".required", "must be true for path parameters")
				}
				pathParamDefinitions[p.Name] = struct{}{}
			}
		}
		for name := range pathParams {
			if _, ok := pathParamDefinitions[name]; !ok {
				add(opPath+".parameters", fmt.Sprintf("missing path parameter definition for {%s}", name))
			}
		}

		for i, a := range op.Auth {
			authPath := fmt.Sprintf("%s.auth[%d].type", opPath, i)
			if _, ok := validAuthTypes[a.Type]; !ok {
				add(authPath, "must be one of: none, apiKey, http, oauth2, mtls")
			}
		}
	}
}
