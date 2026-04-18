package proxy

import (
	"context"
	"fmt"
	"io"

	logging "github.com/mnafshin/apix/internal/logging"
	"github.com/mnafshin/apix/pkg/plugins"
)

// runPluginRequest executes the OnRequest chain of chain with panic recovery.
// Returns the (possibly modified) request, or an error if the chain failed.
// Callers may pass nil for chain — the original req is returned unchanged.
func runPluginRequest(ctx context.Context, chain PluginChain, req *plugins.ProxyRequest, logTag string) (*plugins.ProxyRequest, error) {
	if chain == nil {
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
		modified, err := chain.RunRequest(ctx, req)
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

// runPluginResponse executes the OnResponse chain of chain with panic recovery.
// Returns the (possibly modified) response, or the original resp on failure.
// Callers may pass nil for chain — the original resp is returned unchanged.
func runPluginResponse(ctx context.Context, chain PluginChain, req *plugins.ProxyRequest, resp *plugins.ProxyResponse, logTag string) *plugins.ProxyResponse {
	if chain == nil {
		return resp
	}
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logging.Errorf(ctx, "%s: panic in plugin OnResponse (recovered): %v", logTag, rec)
			}
		}()
		modResp, err := chain.RunResponse(ctx, req, resp)
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
