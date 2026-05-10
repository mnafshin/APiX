package learn

import "testing"

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	path, params := NormalizePath("https://api.example.com/users/123/orders/550e8400-e29b-41d4-a716-446655440000")
	if path != "/users/{id}/orders/{id2}" {
		t.Fatalf("path=%q", path)
	}
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(params))
	}
	if params[0].Name != "id" || params[0].Type != "integer" {
		t.Fatalf("unexpected first param: %+v", params[0])
	}
	if params[1].Name != "id2" || params[1].Format != "uuid" {
		t.Fatalf("unexpected second param: %+v", params[1])
	}
}
