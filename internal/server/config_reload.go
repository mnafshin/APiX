package server

import (
	"context"
	"sync"

	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ConfigReloader func(ctx context.Context, path string) (*apix.ConfigReloadResponse, error)

var (
	configReloaderMu sync.RWMutex
	configReloader   ConfigReloader
)

func SetConfigReloader(fn ConfigReloader) {
	configReloaderMu.Lock()
	defer configReloaderMu.Unlock()
	configReloader = fn
}

func getConfigReloader() ConfigReloader {
	configReloaderMu.RLock()
	defer configReloaderMu.RUnlock()
	return configReloader
}

func (s *EngineServer) ReloadConfig(ctx context.Context, req *apix.ConfigReloadRequest) (*apix.ConfigReloadResponse, error) {
	reloader := getConfigReloader()
	if reloader == nil {
		return nil, status.Error(codes.Unimplemented, "config reload is not enabled")
	}
	path := ""
	if req != nil {
		path = req.GetPath()
	}
	return reloader(ctx, path)
}
