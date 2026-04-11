package builtins

import (
	"bytes"
	"context"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/mnafshin/apix/pkg/plugins"
)

var envPattern = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// secretPatterns lists environment variable name substrings that indicate secret data.
// We check if the name contains these patterns (case-insensitive) to block substitution.
// This prevents leaking credentials via request bodies, while allowing test variables.
var secretPatterns = []string{
	"API_KEY", "API_SECRET", "ACCESS_TOKEN", "REFRESH_TOKEN",
	"PRIVATE_KEY", "SECRET_KEY", "SIGNING_KEY",
	"AWS_", "AZURE_", "GCP_", "GITHUB_", "GITLAB_",
}

// isSecretVar checks if an environment variable name looks like it contains secrets
func isSecretVar(name string) bool {
	upper := strings.ToUpper(name)
	for _, pattern := range secretPatterns {
		if strings.Contains(upper, pattern) {
			return true
		}
	}
	return false
}

// EnvSubst replaces {{ENV_VAR}} placeholders in request headers and body with
// the values of the corresponding environment variables, but blocks access to
// secret-like environment variables to prevent credential leakage.
type EnvSubst struct{}

func (p *EnvSubst) Name() string    { return "env-subst" }
func (p *EnvSubst) Version() string { return "1.0.0" }
func (p *EnvSubst) Description() string {
	return "Replace {{ENV_VAR}} placeholders with env values (blocking secret vars)."
}

func (p *EnvSubst) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	// Read body bytes.
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	// Substitute in body.
	newBody := envPattern.ReplaceAllFunc(bodyBytes, func(match []byte) []byte {
		varName := string(match[2 : len(match)-2]) // strip {{ }}
		// Block secret-like variable names to prevent credential leakage
		if isSecretVar(varName) {
			return match
		}
		if val, ok := os.LookupEnv(varName); ok {
			return []byte(val)
		}
		return match
	})

	clone := req.Clone(io.NopCloser(bytes.NewReader(newBody)))

	// Substitute in header values.
	for k, vals := range clone.Headers {
		for i, v := range vals {
			vals[i] = envPattern.ReplaceAllStringFunc(v, func(match string) string {
				varName := match[2 : len(match)-2]
				// Block secret-like variable names to prevent credential leakage
				if isSecretVar(varName) {
					return match
				}
				if val, ok := os.LookupEnv(varName); ok {
					return val
				}
				return match
			})
		}
		clone.Headers[k] = vals
	}
	return clone, nil
}

func (p *EnvSubst) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	return nil, nil
}
