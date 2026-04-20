package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
	"time"

	logging "github.com/mnafshin/apix/internal/logging"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

func (s *EngineServer) auditLog(ctx context.Context, action, targetID string, details map[string]any) {
	if s == nil || s.cfg == nil || !s.cfg.AuditLogEnabled {
		return
	}
	err := logging.EmitAuditLog(ctx, logging.AuditLogConfig{
		Enabled: s.cfg.AuditLogEnabled,
		Path:    s.cfg.AuditLogPath,
	}, logging.AuditLogEntry{
		Timestamp: time.Now().UTC(),
		Action:    action,
		Actor:     auditActorFromContext(ctx),
		TargetID:  targetID,
		Details:   details,
	})
	if err != nil {
		logging.Errorf(ctx, "audit log emit: %v", err)
	}
}

func auditActorFromContext(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if authVals := md.Get("authorization"); len(authVals) > 0 {
			raw := strings.TrimSpace(authVals[0])
			if raw != "" {
				sum := sha256.Sum256([]byte(raw))
				return "bearer_sha256:" + hex.EncodeToString(sum[:8])
			}
		}
	}
	if p, ok := peer.FromContext(ctx); ok && p != nil && p.Addr != nil {
		if host, _, err := net.SplitHostPort(p.Addr.String()); err == nil {
			return "peer:" + host
		}
		return "peer:" + p.Addr.String()
	}
	return "unknown"
}
