package contracts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate_ValidContract(t *testing.T) {
	t.Parallel()
	c := NewTemplate("Payments API", "1.0.0")
	c.Endpoints = []Endpoint{
		{
			Path: "/users/{id}",
			Operations: map[string]Operation{
				"GET": {
					Parameters: []Parameter{
						{Name: "id", In: "path", Required: true, Schema: JSONType{Type: "string"}},
					},
					Responses: map[string]Result{
						"200": {Description: "OK"},
					},
				},
			},
		},
	}
	if got := Validate(c); got.HasErrors() {
		t.Fatalf("expected valid contract, got diagnostics: %+v", got.Diagnostics)
	}
}

func TestValidate_InvalidContractDeterministicDiagnostics(t *testing.T) {
	t.Parallel()
	c := &Contract{
		SchemaVersion: "v0",
		Info:          Info{},
		Endpoints: []Endpoint{
			{
				Path: "users/{id}",
				Operations: map[string]Operation{
					"FETCH": {
						Parameters: []Parameter{
							{Name: "id", In: "path", Required: false, Schema: JSONType{Type: "id"}},
						},
						Responses: map[string]Result{
							"two-hundred": {},
						},
						Auth: []AuthRequirement{
							{Type: "saml"},
						},
					},
				},
			},
		},
	}
	got := Validate(c)
	if !got.HasErrors() {
		t.Fatal("expected validation diagnostics")
	}
	serialized := got.Error()
	expectedContains := []string{
		"schema_version: must be \"apix.contract/v1\"",
		"info.title: is required",
		"info.version: is required",
		"endpoints[0].path: must start with '/'",
		"unsupported HTTP method",
		"status code must be 3 digits",
		"must be true for path parameters",
		"must be one of: string, integer, number, boolean, object, array",
		"must be one of: none, apiKey, http, oauth2, mtls",
	}
	for _, expected := range expectedContains {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("expected diagnostic %q in %q", expected, serialized)
		}
	}
}

func TestValidateFile_LoadsAndValidatesYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "contract.yaml")
	if err := SaveYAML(path, NewTemplate("Orders API", "0.3.0")); err != nil {
		t.Fatalf("save yaml: %v", err)
	}
	_, validation, err := ValidateFile(path)
	if err != nil {
		t.Fatalf("validate file error: %v", err)
	}
	if validation != nil {
		t.Fatalf("expected valid file, got diagnostics: %+v", validation.Diagnostics)
	}
}

func TestLoadFile_JSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "contract.json")
	payload := `{"schema_version":"apix.contract/v1","info":{"title":"X","version":"1.0.0"},"endpoints":[{"path":"/health","operations":{"GET":{"responses":{"200":{"description":"ok"}}}}}]}`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	c, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load json: %v", err)
	}
	if c.Info.Title != "X" {
		t.Fatalf("unexpected title: %q", c.Info.Title)
	}
}
