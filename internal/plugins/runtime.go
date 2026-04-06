package plugins

import (
	"context"
	"fmt"
	"sync"
)

// Runtime manages the lifecycle of all registered plugins.
type Runtime struct {
	mu      sync.RWMutex
	plugins map[string]Plugin // name → plugin
	order   []string          // insertion order for deterministic chain
}

// NewRuntime creates an empty plugin runtime.
func NewRuntime() *Runtime {
	return &Runtime{
		plugins: make(map[string]Plugin),
	}
}

// Register adds a plugin to the runtime. Returns an error if a plugin with
// the same name is already registered.
func (r *Runtime) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.plugins[p.Name()]; exists {
		return fmt.Errorf("plugin %q already registered", p.Name())
	}
	r.plugins[p.Name()] = p
	r.order = append(r.order, p.Name())
	return nil
}

// Unregister removes a plugin by name.
func (r *Runtime) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.plugins[name]; !exists {
		return fmt.Errorf("plugin %q not found", name)
	}
	delete(r.plugins, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return nil
}

// List returns metadata for all registered plugins.
func (r *Runtime) List() []PluginMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	metas := make([]PluginMeta, 0, len(r.order))
	for _, name := range r.order {
		p := r.plugins[name]
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
// Short-circuits on the first error.
func (r *Runtime) RunRequest(ctx context.Context, req *ProxyRequest) (*ProxyRequest, error) {
	r.mu.RLock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	plugins := make(map[string]Plugin, len(r.plugins))
	for k, v := range r.plugins {
		plugins[k] = v
	}
	r.mu.RUnlock()

	for _, name := range order {
		p := plugins[name]
		modified, err := p.OnRequest(ctx, req)
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
func (r *Runtime) RunResponse(ctx context.Context, req *ProxyRequest, resp *ProxyResponse) (*ProxyResponse, error) {
	r.mu.RLock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	plugins := make(map[string]Plugin, len(r.plugins))
	for k, v := range r.plugins {
		plugins[k] = v
	}
	r.mu.RUnlock()

	for _, name := range order {
		p := plugins[name]
		modified, err := p.OnResponse(ctx, req, resp)
		if err != nil {
			return nil, fmt.Errorf("plugin %q OnResponse: %w", name, err)
		}
		if modified != nil {
			resp = modified
		}
	}
	return resp, nil
}

// PluginMeta is a lightweight summary of a plugin's identity.
type PluginMeta struct {
	Name        string
	Version     string
	Description string
	Enabled     bool
}
