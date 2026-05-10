package learn

import "testing"

func TestBuildOpenAPISpec(t *testing.T) {
	t.Parallel()

	spec := BuildOpenAPISpec([]Observation{
		{
			Method:         "GET",
			URL:            "https://api.example.com/users/123?include=posts",
			RequestHeaders: map[string]string{"Authorization": "Bearer token"},
			ResponseStatus: 200,
			ResponseBody:   []byte(`{"id":123,"name":"A"}`),
		},
		{
			Method:         "GET",
			URL:            "https://api.example.com/users/999?include=comments",
			RequestHeaders: map[string]string{"Authorization": "Bearer token"},
			ResponseStatus: 200,
			ResponseBody:   []byte(`{"id":999,"name":"B","active":true}`),
		},
		{
			Method:         "POST",
			URL:            "https://api.example.com/users",
			RequestBody:    []byte(`{"name":"C","email":"c@example.com"}`),
			ResponseStatus: 201,
			ResponseBody:   []byte(`{"id":1000}`),
		},
	}, "Test API", "0.1.0")

	paths := spec["paths"].(map[string]any)
	if _, ok := paths["/users/{id}"]; !ok {
		t.Fatalf("expected inferred /users/{id} path, got %+v", paths)
	}
	if _, ok := paths["/users"]; !ok {
		t.Fatalf("expected inferred /users path, got %+v", paths)
	}
	components := spec["components"].(map[string]any)
	securitySchemes := components["securitySchemes"].(map[string]any)
	if _, ok := securitySchemes["bearerAuth"]; !ok {
		t.Fatalf("expected bearerAuth security scheme")
	}
}
