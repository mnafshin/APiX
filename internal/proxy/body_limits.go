package proxy

import "github.com/mnafshin/apix/internal/config"

func maxBodyBytes(cfg *config.Config) int64 {
	return int64(cfg.MaxBodySizeMB) * 1024 * 1024
}
