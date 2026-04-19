package proxy

import (
	"context"
	"fmt"
	"io"

	logging "github.com/mnafshin/apix/internal/logging"
	"github.com/mnafshin/apix/pkg/plugins"
)

// runPluginRequest executes the RunRequest hook via type assertion.
// Returns the (possibly modified) request, or an error if the hook failed.
// Callers may pass nil or a value that does not implement RequestPlugin —
// in both cases the original req is returned unchanged.
func runPluginRequest(ctx context.Context, chain any, req *plugins.ProxyRequest, logTag string) (*plugins.ProxyRequest, error) {
	rp, ok := chain.(RequestPlugin)
	if !ok || rp == nil {
		return req, nil
	}
	var runErr error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logging.Errorf(ctx, "%s: panic in plugin OnRequest (recovered): %v", logTag, rec)
				runErr = fmt.Errorf("plugin panic")
			}
		}()
		modified, err := rp.RunRequest(ctx, req)
		if err != nil {
			logging.Errorf(ctx, "%s: plugin OnRequest error: %v", logTag, err)
			runErr = err
			return
		}
		req = modified
	}()
	if runErr != nil {
		return nil, runErr
	}
	return req, nil
}

// runPluginResponse executes the RunResponse hook via type assertion.
// Returns the (possibly modified) response, or the original resp on failure.
// Callers may pass nil or a value that does not implement ResponsePlugin —
// in both cases the original resp is returned unchanged.
func runPluginResponse(ctx context.Context, chain any, req *plugins.ProxyRequest, resp *plugins.ProxyResponse, logTag string) *plugins.ProxyResponse {
	rp, ok := chain.(ResponsePlugin)
	if !ok || rp == nil {
		return resp
	}
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logging.Errorf(ctx, "%s: panic in plugin OnResponse (recovered): %v", logTag, rec)
			}
		}()
		modResp, err := rp.RunResponse(ctx, req, resp)
		if err != nil {
			logging.Errorf(ctx, "%s: plugin OnResponse error: %v", logTag, err)
			return
		}
		if modResp != nil {
			resp = modResp
		}
	}()
	return resp
}

// drainPluginResponseBody reads the response body from a plugin-modified response.
// Returns updated body bytes (or original body on read error).
func drainPluginResponseBody(ctx context.Context, resp *plugins.ProxyResponse, maxBytes int64, logTag string) []byte {
	if resp.Body == nil {
		return nil
	}
	limited := io.LimitReader(resp.Body, maxBytes)
	b, err := io.ReadAll(limited)
	if err != nil {
		logging.Errorf(ctx, "%s: read plugin response body: %v", logTag, err)
		return nil
	}
	return b
}
