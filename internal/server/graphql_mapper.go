package server

import (
	gql "github.com/mnafshin/apix/internal/graphql"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

func toProtoGraphQLMetadata(meta *gql.Metadata) *apix.GraphQLMetadata {
	if meta == nil {
		return nil
	}
	out := &apix.GraphQLMetadata{}
	if meta.Request != nil {
		out.Request = &apix.GraphQLRequestMetadata{
			OperationName:  meta.Request.OperationName,
			Query:          meta.Request.Query,
			VariablesJson:  meta.Request.VariablesJSON,
			IsBatch:        meta.Request.IsBatch,
			OperationCount: int32(meta.Request.OperationCount), //nolint:gosec // G115: operation count is bounded by payload size
		}
	}
	if meta.Response != nil {
		out.Response = &apix.GraphQLResponseMetadata{
			Errors: make([]*apix.GraphQLError, 0, len(meta.Response.Errors)),
		}
		for _, e := range meta.Response.Errors {
			out.Response.Errors = append(out.Response.Errors, &apix.GraphQLError{
				Message:        e.Message,
				PathJson:       e.PathJSON,
				LocationsJson:  e.LocationsJSON,
				ExtensionsJson: e.ExtensionsJSON,
				RawJson:        e.RawJSON,
			})
		}
	}
	if out.Request == nil && out.Response == nil {
		return nil
	}
	return out
}
