package server

import (
	"context"

	apix "github.com/mnafshin/apix/pkg/api/generated"
)

func (s *EngineServer) ListPlugins(ctx context.Context, _ *apix.PluginListRequest) (*apix.PluginListResponse, error) {
	metas := s.engine.PluginRuntime().List()
	infos := make([]*apix.PluginInfo, 0, len(metas))
	for _, m := range metas {
		infos = append(infos, &apix.PluginInfo{
			Name:        m.Name,
			Version:     m.Version,
			Description: m.Description,
			Enabled:     m.Enabled,
		})
	}
	return &apix.PluginListResponse{Plugins: infos}, nil
}
