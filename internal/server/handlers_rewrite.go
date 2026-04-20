package server

import (
	"context"

	"github.com/google/uuid"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *EngineServer) AddRewriteRule(ctx context.Context, rule *apix.RewriteRule) (*apix.RewriteRule, error) {
	if rule.Id == "" {
		rule.Id = uuid.NewString()
	}
	if err := s.db.AddRewriteRule(rule); err != nil {
		return nil, status.Errorf(codes.Internal, "add rewrite rule: %v", err)
	}
	s.auditLog(ctx, "add_rewrite_rule", rule.Id, map[string]any{
		"name":     rule.Name,
		"action":   rule.Action.String(),
		"enabled":  rule.Enabled,
		"priority": rule.Priority,
	})
	return rule, nil
}

func (s *EngineServer) UpdateRewriteRule(ctx context.Context, rule *apix.RewriteRule) (*apix.RewriteRule, error) {
	if rule.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "rule_id is required")
	}
	if err := s.db.UpdateRewriteRule(rule); err != nil {
		return nil, status.Errorf(codes.Internal, "update rewrite rule: %v", err)
	}
	s.auditLog(ctx, "update_rewrite_rule", rule.Id, map[string]any{
		"name":     rule.Name,
		"action":   rule.Action.String(),
		"enabled":  rule.Enabled,
		"priority": rule.Priority,
	})
	return rule, nil
}

func (s *EngineServer) DeleteRewriteRule(ctx context.Context, req *apix.RewriteRuleRequest) (*apix.Empty, error) {
	if req.RuleId == "" {
		return nil, status.Error(codes.InvalidArgument, "rule_id is required")
	}
	if err := s.db.DeleteRewriteRule(req.RuleId); err != nil {
		return nil, status.Errorf(codes.Internal, "delete rewrite rule: %v", err)
	}
	s.auditLog(ctx, "delete_rewrite_rule", req.RuleId, nil)
	return &apix.Empty{}, nil
}

func (s *EngineServer) ListRewriteRules(ctx context.Context, _ *apix.Empty) (*apix.RewriteRuleList, error) {
	rules, err := s.db.ListRewriteRules()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list rewrite rules: %v", err)
	}
	return &apix.RewriteRuleList{Rules: rules}, nil
}

func (s *EngineServer) ToggleRewriteRule(ctx context.Context, req *apix.RewriteRuleRequest) (*apix.RewriteRule, error) {
	if req.RuleId == "" {
		return nil, status.Error(codes.InvalidArgument, "rule_id is required")
	}
	rule, err := s.db.GetRewriteRule(req.RuleId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get rewrite rule: %v", err)
	}
	if rule == nil {
		return nil, status.Errorf(codes.NotFound, "rule %q not found", req.RuleId)
	}
	rule.Enabled = !rule.Enabled
	if err := s.db.UpdateRewriteRule(rule); err != nil {
		return nil, status.Errorf(codes.Internal, "toggle rewrite rule: %v", err)
	}
	s.auditLog(ctx, "toggle_rewrite_rule", rule.Id, map[string]any{"enabled": rule.Enabled})
	return rule, nil
}
