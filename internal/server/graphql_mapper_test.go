package server

import (
	"testing"

	gql "github.com/mnafshin/apix/internal/graphql"
)

func TestToProtoGraphQLMetadata(t *testing.T) {
	in := &gql.Metadata{
		Request: &gql.RequestMetadata{
			OperationName:  "GetUser",
			Query:          "query GetUser { user { id } }",
			VariablesJSON:  `{"id":"1"}`,
			IsBatch:        true,
			OperationCount: 2,
		},
		Response: &gql.ResponseMetadata{
			Errors: []gql.ErrorMetadata{
				{
					Message:        "boom",
					PathJSON:       `["user"]`,
					LocationsJSON:  `[{"line":1,"column":2}]`,
					ExtensionsJSON: `{"code":"BAD_USER_INPUT"}`,
					RawJSON:        `{"message":"boom"}`,
				},
			},
		},
	}

	got := toProtoGraphQLMetadata(in)
	if got == nil {
		t.Fatal("expected metadata, got nil")
	}
	if got.Request == nil || got.Request.OperationName != "GetUser" {
		t.Fatalf("unexpected request metadata: %+v", got.Request)
	}
	if got.Request.OperationCount != 2 {
		t.Fatalf("operation count: got %d", got.Request.OperationCount)
	}
	if got.Response == nil || len(got.Response.Errors) != 1 {
		t.Fatalf("unexpected response metadata: %+v", got.Response)
	}
	if got.Response.Errors[0].Message != "boom" {
		t.Fatalf("error message: got %q", got.Response.Errors[0].Message)
	}
}
