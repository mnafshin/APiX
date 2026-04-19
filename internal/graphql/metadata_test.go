package graphql

import "testing"

func TestExtract_ValidGraphQLRequestAndResponse(t *testing.T) {
	reqBody := []byte(`{"operationName":"GetUser","query":"query GetUser { user { id } }","variables":{"id":"123"}}`)
	respBody := []byte(`{"data":null,"errors":[{"message":"boom","path":["user"],"extensions":{"code":"BAD_USER_INPUT"}}]}`)

	got := Extract(
		map[string]string{"Content-Type": "application/json"},
		reqBody,
		map[string]string{"content-type": "application/json"},
		respBody,
	)
	if got == nil {
		t.Fatal("expected metadata, got nil")
	}
	if got.Request == nil {
		t.Fatal("expected request metadata")
	}
	if got.Request.OperationName != "GetUser" {
		t.Fatalf("operation name: got %q", got.Request.OperationName)
	}
	if got.Request.Query == "" {
		t.Fatal("expected query to be extracted")
	}
	if got.Request.VariablesJSON != `{"id":"123"}` {
		t.Fatalf("variables json: got %q", got.Request.VariablesJSON)
	}
	if got.Response == nil {
		t.Fatal("expected response metadata")
	}
	if len(got.Response.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(got.Response.Errors))
	}
	if got.Response.Errors[0].Message != "boom" {
		t.Fatalf("error message: got %q", got.Response.Errors[0].Message)
	}
}

func TestExtract_MalformedJSON(t *testing.T) {
	got := Extract(
		map[string]string{"Content-Type": "application/json"},
		[]byte(`{"query":`),
		map[string]string{"Content-Type": "application/json"},
		[]byte(`{"errors":[`),
	)
	if got != nil {
		t.Fatalf("expected nil metadata for malformed json, got %+v", got)
	}
}

func TestExtract_MissingFields(t *testing.T) {
	got := Extract(
		map[string]string{"Content-Type": "application/json"},
		[]byte(`{"variables":{"id":1}}`),
		map[string]string{"Content-Type": "application/json"},
		[]byte(`{"data":{"ok":true}}`),
	)
	if got == nil || got.Request == nil {
		t.Fatal("expected request metadata with variables")
	}
	if got.Request.OperationName != "" {
		t.Fatalf("expected empty operation name, got %q", got.Request.OperationName)
	}
	if got.Request.Query != "" {
		t.Fatalf("expected empty query, got %q", got.Request.Query)
	}
	if got.Response != nil {
		t.Fatal("did not expect response metadata without errors")
	}
}

func TestExtract_BatchedPayloads(t *testing.T) {
	reqBody := []byte(`[
		{"operationName":"First","query":"query First { a }","variables":{"a":1}},
		{"operationName":"Second","query":"query Second { b }","variables":{"b":2}}
	]`)
	respBody := []byte(`[
		{"data":{"a":1}},
		{"errors":[{"message":"second failed","path":["b"]}]}
	]`)

	got := Extract(
		map[string]string{"Content-Type": "application/json"},
		reqBody,
		map[string]string{"Content-Type": "application/json"},
		respBody,
	)
	if got == nil || got.Request == nil || got.Response == nil {
		t.Fatalf("expected both request and response metadata, got %+v", got)
	}
	if !got.Request.IsBatch {
		t.Fatal("expected batched request")
	}
	if got.Request.OperationCount != 2 {
		t.Fatalf("operation count: got %d", got.Request.OperationCount)
	}
	if len(got.Response.Errors) != 1 {
		t.Fatalf("expected 1 error from batched response, got %d", len(got.Response.Errors))
	}
}

func TestExtract_NoPanicOnArbitraryBodies(t *testing.T) {
	bodies := [][]byte{
		{},
		[]byte{0x00, 0xff, 0x01},
		[]byte(`not-json`),
		[]byte(`{"errors":"wrong-shape"}`),
		[]byte(`[]`),
	}
	for i, body := range bodies {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("case %d panicked: %v", i, r)
				}
			}()
			_ = Extract(
				map[string]string{"Content-Type": "application/json"},
				body,
				map[string]string{"Content-Type": "application/json"},
				body,
			)
		}()
	}
}
