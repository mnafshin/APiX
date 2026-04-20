package pluginrt

import (
	"context"
	"fmt"
	"testing"
)

func benchRuntimeWithNoOpPlugins(b *testing.B, n int) *Runtime {
	b.Helper()
	rt := NewRuntime()
	for i := 0; i < n; i++ {
		if err := rt.Register(&mockPlugin{name: fmt.Sprintf("bench-plugin-%d", i)}); err != nil {
			b.Fatalf("Register: %v", err)
		}
	}
	return rt
}

func benchmarkRunRequestWithPlugins(b *testing.B, pluginCount int) {
	rt := benchRuntimeWithNoOpPlugins(b, pluginCount)
	req := newProxyRequest("GET", "https://example.com/bench")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.RunRequest(ctx, req); err != nil {
			b.Fatalf("RunRequest: %v", err)
		}
	}
}

func BenchmarkPluginRuntime_RunRequest_0Plugins(b *testing.B) {
	benchmarkRunRequestWithPlugins(b, 0)
}

func BenchmarkPluginRuntime_RunRequest_1Plugin(b *testing.B) {
	benchmarkRunRequestWithPlugins(b, 1)
}

func BenchmarkPluginRuntime_RunRequest_5Plugins(b *testing.B) {
	benchmarkRunRequestWithPlugins(b, 5)
}

func BenchmarkPluginRuntime_RunRequest_14Plugins(b *testing.B) {
	benchmarkRunRequestWithPlugins(b, 14)
}
