package proxy

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"time"

	logging "github.com/mnafshin/apix/internal/logging"
	metrics "github.com/mnafshin/apix/internal/metrics"
	"github.com/mnafshin/apix/pkg/plugins"
)

// runPluginRequest executes the RunRequest hook via type assertion with optional timeout.
// Returns the (possibly modified) request, or an error if the hook failed.
// Callers may pass nil or a value that does not implement RequestPlugin —
// in both cases the original req is returned unchanged.
// timeoutSec: plugin execution timeout in seconds (0 = no timeout).
func runPluginRequest(ctx context.Context, chain any, req *plugins.ProxyRequest, logTag string, timeoutSec int) (*plugins.ProxyRequest, error) {
	rp, ok := chain.(RequestPlugin)
	if !ok || rp == nil {
		return req, nil
	}
	pluginName := pluginMetricName(chain)
	start := time.Now()
	var runErr error

	// Track memory before and after plugin execution
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	func() {
		defer func() {
			recoverProxyPanic(ctx, fmt.Sprintf("%s: panic in plugin OnRequest", logTag), func() {
				runErr = fmt.Errorf("plugin panic")
			})
			// Track memory after plugin execution
			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)
			memDeltaMB := float64((m2.Alloc - m1.Alloc)) / 1024 / 1024
			if memDeltaMB > 10 {
				logging.Warnf(ctx, "%s: plugin %q allocated %.2f MB during OnRequest", logTag, pluginName, memDeltaMB)
			}
		}()

		// Apply plugin execution timeout if configured
		execCtx := ctx
		if timeoutSec > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
			defer cancel()
		}

		modified, err := rp.RunRequest(execCtx, req)
		if err != nil {
			if execCtx.Err() == context.DeadlineExceeded {
				logging.Errorf(ctx, "%s: plugin %q OnRequest timeout exceeded (%d sec)", logTag, pluginName, timeoutSec)
				runErr = fmt.Errorf("plugin timeout")
			} else {
				logging.Errorf(ctx, "%s: plugin %q OnRequest error: %v", logTag, pluginName, err)
				runErr = err
			}
			return
		}
		req = modified
	}()
	metrics.ObservePluginExecution(pluginName, "request", time.Since(start).Seconds(), runErr != nil)
	if runErr != nil {
		return nil, runErr
	}
	return req, nil
}

// runPluginResponse executes the RunResponse hook via type assertion with optional timeout.
// Returns the (possibly modified) response, or an error when the plugin fails.
// Callers may pass nil or a value that does not implement ResponsePlugin —
// in both cases the original resp is returned unchanged.
// timeoutSec: plugin execution timeout in seconds (0 = no timeout).
func runPluginResponse(ctx context.Context, chain any, req *plugins.ProxyRequest, resp *plugins.ProxyResponse, logTag string, timeoutSec int) (*plugins.ProxyResponse, error) {
	rp, ok := chain.(ResponsePlugin)
	if !ok || rp == nil {
		return resp, nil
	}
	pluginName := pluginMetricName(chain)
	start := time.Now()
	var runErr error

	// Track memory before and after plugin execution
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	func() {
		defer func() {
			recoverProxyPanic(ctx, fmt.Sprintf("%s: panic in plugin OnResponse", logTag), func() {
				runErr = fmt.Errorf("plugin panic")
			})
			// Track memory after plugin execution
			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)
			memDeltaMB := float64((m2.Alloc - m1.Alloc)) / 1024 / 1024
			if memDeltaMB > 10 {
				logging.Warnf(ctx, "%s: plugin %q allocated %.2f MB during OnResponse", logTag, pluginName, memDeltaMB)
			}
		}()

		// Apply plugin execution timeout if configured
		execCtx := ctx
		if timeoutSec > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
			defer cancel()
		}

		modResp, err := rp.RunResponse(execCtx, req, resp)
		if err != nil {
			if execCtx.Err() == context.DeadlineExceeded {
				logging.Errorf(ctx, "%s: plugin %q OnResponse timeout exceeded (%d sec)", logTag, pluginName, timeoutSec)
				runErr = fmt.Errorf("plugin timeout")
			} else {
				logging.Errorf(ctx, "%s: plugin %q OnResponse error: %v", logTag, pluginName, err)
				runErr = err
			}
			return
		}
		if modResp != nil {
			resp = modResp
		}
	}()
	metrics.ObservePluginExecution(pluginName, "response", time.Since(start).Seconds(), runErr != nil)
	if runErr != nil {
		return nil, runErr
	}
	return resp, nil
}

func pluginMetricName(chain any) string {
	if chain == nil {
		return "unknown"
	}
	t := reflect.TypeOf(chain)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if name := t.Name(); name != "" {
		return name
	}
	return "unknown"
}

// drainPluginResponseBody reads the response body from a plugin-modified response.
// Returns updated body bytes and an explicit read error when body extraction fails.
func drainPluginResponseBody(ctx context.Context, resp *plugins.ProxyResponse, maxBytes int64, logTag string) ([]byte, error) {
	if resp.Body == nil {
		return nil, nil
	}
	limited := io.LimitReader(resp.Body, maxBytes)
	b, err := io.ReadAll(limited)
	if err != nil {
		logging.Errorf(ctx, "%s: read plugin response body: %v", logTag, err)
		return nil, err
	}
	return b, nil
}
