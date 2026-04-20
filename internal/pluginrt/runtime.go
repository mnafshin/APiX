package pluginrt

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/mnafshin/apix/pkg/plugins"
)

// Runtime manages the lifecycle of all registered plugins.
type Runtime struct {
	mu      sync.Mutex
	cond    *sync.Cond
	plugins map[string]*pluginEntry // name → plugin entry
	order   []string                // insertion order for deterministic chain
}

type pluginEntry struct {
	plugin   plugins.Plugin
	inFlight int
	draining bool
}

// NewRuntime creates an empty plugin runtime.
func NewRuntime() *Runtime {
	r := &Runtime{
		plugins: make(map[string]*pluginEntry),
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// Register adds a plugin to the runtime. Returns an error if a plugin with
// the same name is already registered.
func (r *Runtime) Register(p plugins.Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.plugins[p.Name()]; exists {
		return fmt.Errorf("plugin %q already registered", p.Name())
	}
	r.plugins[p.Name()] = &pluginEntry{plugin: p}
	r.order = append(r.order, p.Name())
	return nil
}

// Unregister removes a plugin by name.
func (r *Runtime) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, exists := r.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %q not found", name)
	}
	// Drain: stop routing new requests to this plugin, then wait for in-flight
	// executions to finish before removing it.
	entry.draining = true
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	for entry.inFlight > 0 {
		r.cond.Wait()
	}
	delete(r.plugins, name)
	return nil
}

// List returns metadata for all registered plugins.
func (r *Runtime) List() []PluginMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	metas := make([]PluginMeta, 0, len(r.order))
	for _, name := range r.order {
		p := r.plugins[name].plugin
		metas = append(metas, PluginMeta{
			Name:        p.Name(),
			Version:     p.Version(),
			Description: p.Description(),
			Enabled:     true,
		})
	}
	return metas
}

// RunRequest executes the OnRequest hook of every plugin in order.
// Plugins that do not implement RequestPlugin are silently skipped.
// Short-circuits on the first error. Panics inside plugin code are recovered
// and returned as errors so a misbehaving plugin cannot crash the engine.
func (r *Runtime) RunRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	r.mu.Lock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	r.mu.Unlock()

	for _, name := range order {
		p, done := r.beginPluginUse(name)
		if p == nil {
			continue
		}
		rp, ok := p.(plugins.RequestPlugin)
		if !ok {
			done()
			continue
		}
		modified, err := safeOnRequest(ctx, rp, req)
		done()
		if err != nil {
			return nil, fmt.Errorf("plugin %q OnRequest: %w", name, err)
		}
		if modified != nil {
			req = modified
		}
	}
	return req, nil
}

// RunResponse executes the OnResponse hook of every plugin in order.
// Plugins that do not implement ResponsePlugin are silently skipped.
// Panics inside plugin code are recovered and returned as errors.
func (r *Runtime) RunResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	r.mu.Lock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	r.mu.Unlock()

	for _, name := range order {
		p, done := r.beginPluginUse(name)
		if p == nil {
			continue
		}
		rp, ok := p.(plugins.ResponsePlugin)
		if !ok {
			done()
			continue
		}
		modified, err := safeOnResponse(ctx, rp, req, resp)
		done()
		if err != nil {
			return nil, fmt.Errorf("plugin %q OnResponse: %w", name, err)
		}
		if modified != nil {
			resp = modified
		}
	}
	return resp, nil
}

func (r *Runtime) beginPluginUse(name string) (plugins.Plugin, func()) {
	r.mu.Lock()
	entry, ok := r.plugins[name]
	if !ok || entry.draining {
		r.mu.Unlock()
		return nil, nil
	}
	entry.inFlight++
	p := entry.plugin
	r.mu.Unlock()

	return p, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		e, exists := r.plugins[name]
		if !exists {
			return
		}
		e.inFlight--
		if e.draining && e.inFlight == 0 {
			r.cond.Broadcast()
		}
	}
}

// safeOnRequest calls p.OnRequest and converts any panic into an error.
func safeOnRequest(ctx context.Context, p plugins.RequestPlugin, req *plugins.ProxyRequest) (modified *plugins.ProxyRequest, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return p.OnRequest(ctx, req)
}

// safeOnResponse calls p.OnResponse and converts any panic into an error.
func safeOnResponse(ctx context.Context, p plugins.ResponsePlugin, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (modified *plugins.ProxyResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return p.OnResponse(ctx, req, resp)
}

// PluginMeta is a lightweight summary of a plugin's identity.
type PluginMeta struct {
	Name        string
	Version     string
	Description string
	Enabled     bool
}
