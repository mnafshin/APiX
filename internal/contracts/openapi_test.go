package contracts

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExportOpenAPI31_ValidContract(t *testing.T) {
	t.Parallel()
	c := &Contract{
		SchemaVersion: CurrentSchemaVersion,
		Info: Info{
			Title:       "Test API",
			Version:     "1.0.0",
			Description: "A test API",
		},
		Servers: []Server{
			{URL: "https://api.example.com", Description: "Production"},
		},
		Endpoints: []Endpoint{
			{
				Path: "/users/{id}",
				Operations: map[string]Operation{
					"GET": {
						Summary: "Get user",
						Parameters: []Parameter{
							{Name: "id", In: "path", Required: true, Schema: JSONType{Type: "string"}},
						},
						Responses: map[string]Result{
							"200": {Description: "OK"},
							"404": {Description: "Not found"},
						},
					},
				},
			},
		},
	}
	doc, diags := ExportOpenAPI31(c)
	if len(diags) > 0 {
		t.Fatalf("expected no diagnostics, got %d: %+v", len(diags), diags)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("expected openapi 3.1.0, got %v", doc["openapi"])
	}
	info, ok := doc["info"].(map[string]any)
	if !ok || info["title"] != "Test API" {
		t.Fatalf("unexpected info: %+v", info)
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("unexpected paths: %+v", paths)
	}
}

func TestImportOpenAPI31_ValidDocument(t *testing.T) {
	t.Parallel()
	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Imported API",
			"version":     "1.0.0",
			"description": "An imported API",
		},
		"servers": []any{
			map[string]any{"url": "https://api.example.com", "description": "Production"},
		},
		"paths": map[string]any{
			"/items/{id}": map[string]any{
				"get": map[string]any{
					"summary": "Get item",
					"parameters": []any{
						map[string]any{
							"name":     "id",
							"in":       "path",
							"required": true,
							"schema":   map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "OK"},
					},
				},
			},
		},
	}
	c, diags, err := ImportOpenAPI31(doc)
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	if len(diags) > 0 {
		t.Logf("import diagnostics: %+v", diags)
	}
	if c.Info.Title != "Imported API" {
		t.Fatalf("unexpected title: %q", c.Info.Title)
	}
	if len(c.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(c.Endpoints))
	}
	if len(c.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(c.Servers))
	}
}

func TestRoundTrip_ContractToOpenAPIAndBack(t *testing.T) {
	t.Parallel()
	original := &Contract{
		SchemaVersion: CurrentSchemaVersion,
		Info: Info{
			Title:       "Roundtrip API",
			Version:     "2.0.0",
			Description: "Test roundtrip",
		},
		Endpoints: []Endpoint{
			{
				Path: "/health",
				Operations: map[string]Operation{
					"GET": {
						Summary: "Health check",
						Responses: map[string]Result{
							"200": {Description: "Healthy"},
						},
					},
				},
			},
		},
	}

	doc, exportDiags := ExportOpenAPI31(original)
	if len(exportDiags) > 0 {
		t.Fatalf("export diagnostics: %+v", exportDiags)
	}

	reimported, importDiags, err := ImportOpenAPI31(doc)
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	if len(importDiags) > 0 {
		t.Logf("import diagnostics: %+v", importDiags)
	}

	if reimported.Info.Title != original.Info.Title {
		t.Fatalf("title mismatch: %q vs %q", reimported.Info.Title, original.Info.Title)
	}
	if reimported.Info.Version != original.Info.Version {
		t.Fatalf("version mismatch: %q vs %q", reimported.Info.Version, original.Info.Version)
	}
	if len(reimported.Endpoints) != len(original.Endpoints) {
		t.Fatalf("endpoint count mismatch: %d vs %d", len(reimported.Endpoints), len(original.Endpoints))
	}
}

func TestExportOpenAPI31_JSON(t *testing.T) {
	t.Parallel()
	c := NewTemplate("Export Test", "1.0.0")
	doc, _ := ExportOpenAPI31(c)
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatalf("empty json output")
	}
	if !strings.Contains(string(encoded), "3.1.0") {
		t.Fatalf("missing openapi version in json: %s", string(encoded))
	}
}

func TestExportOpenAPI31_YAML(t *testing.T) {
	t.Parallel()
	c := NewTemplate("Export YAML Test", "1.0.0")
	doc, _ := ExportOpenAPI31(c)
	encoded, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatalf("empty yaml output")
	}
	if !strings.Contains(string(encoded), "3.1.0") {
		t.Fatalf("missing openapi version in yaml: %s", string(encoded))
	}
}

func TestImportOpenAPI31_DiagnosticsForUnsupportedFeatures(t *testing.T) {
	t.Parallel()
	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Test",
			"version": "1.0.0",
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"User": map[string]any{"type": "object"},
			},
			"links": map[string]any{
				"link1": map[string]any{},
			},
		},
		"paths": map[string]any{
			"/test": map[string]any{
				"get": map[string]any{
					"responses": map[string]any{
						"200": map[string]any{"description": "OK"},
					},
				},
			},
		},
	}
	_, diags, err := ImportOpenAPI31(doc)
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	if len(diags) < 2 {
		t.Fatalf("expected diagnostics for unsupported features, got %d", len(diags))
	}
	hasSchemaDiag := false
	hasLinkDiag := false
	for _, d := range diags {
		if strings.Contains(d.Path, "schemas") {
			hasSchemaDiag = true
		}
		if strings.Contains(d.Path, "links") {
			hasLinkDiag = true
		}
	}
	if !hasSchemaDiag || !hasLinkDiag {
		t.Fatalf("missing expected diagnostics in: %+v", diags)
	}
}
