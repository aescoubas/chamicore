package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"git.cscs.ch/openchami/chamicore-mcp/internal/policy"
)

var (
	// ErrSessionTokenMissing indicates no MCP session token was configured.
	ErrSessionTokenMissing = errors.New("mcp session token is not configured")
	// ErrBearerTokenMissing indicates Authorization header did not contain a bearer token.
	ErrBearerTokenMissing = errors.New("missing or malformed Authorization bearer token")
	// ErrBearerTokenInvalid indicates provided bearer token did not match configured session token.
	ErrBearerTokenInvalid = errors.New("invalid bearer token for MCP session")
)

// SessionPrincipal carries caller identity for tool policy checks.
type SessionPrincipal struct {
	Subject string
	Scopes  []string
}

// SessionAuthenticator authenticates HTTP and stdio MCP calls.
type SessionAuthenticator interface {
	AuthenticateHTTP(r *http.Request) (SessionPrincipal, error)
	AuthenticateStdio() (SessionPrincipal, error)
}

// TokenSessionAuthenticator validates incoming bearer tokens against a configured
// MCP session token and exposes resolved principal scopes.
type TokenSessionAuthenticator struct {
	token     string
	principal SessionPrincipal
}

// NewTokenSessionAuthenticator creates a new session authenticator.
//
// Scope derivation:
// - JWT-like tokens use parsed scope claims.
// - Opaque tokens default to broad V1 admin scope.
func NewTokenSessionAuthenticator(token string) *TokenSessionAuthenticator {
	trimmed := strings.TrimSpace(token)
	return &TokenSessionAuthenticator{
		token:     trimmed,
		principal: deriveSessionPrincipal(trimmed),
	}
}

// AuthenticateHTTP validates Authorization bearer token.
func (a *TokenSessionAuthenticator) AuthenticateHTTP(r *http.Request) (SessionPrincipal, error) {
	if strings.TrimSpace(a.token) == "" {
		return SessionPrincipal{}, fmt.Errorf("%w; set CHAMICORE_MCP_TOKEN or CHAMICORE_TOKEN", ErrSessionTokenMissing)
	}
	presented := parseBearerToken(r.Header.Get("Authorization"))
	if presented == "" {
		return SessionPrincipal{}, ErrBearerTokenMissing
	}
	if presented != a.token {
		return SessionPrincipal{}, ErrBearerTokenInvalid
	}
	return clonePrincipal(a.principal), nil
}

// AuthenticateStdio validates configured stdio session token presence.
func (a *TokenSessionAuthenticator) AuthenticateStdio() (SessionPrincipal, error) {
	if strings.TrimSpace(a.token) == "" {
		return SessionPrincipal{}, fmt.Errorf("%w; set CHAMICORE_MCP_TOKEN or CHAMICORE_TOKEN, or enable CHAMICORE_MCP_ALLOW_CLI_CONFIG_TOKEN", ErrSessionTokenMissing)
	}
	return clonePrincipal(a.principal), nil
}

func clonePrincipal(p SessionPrincipal) SessionPrincipal {
	clonedScopes := make([]string, len(p.Scopes))
	copy(clonedScopes, p.Scopes)
	return SessionPrincipal{
		Subject: p.Subject,
		Scopes:  clonedScopes,
	}
}

func deriveSessionPrincipal(token string) SessionPrincipal {
	subject := "mcp-session"
	scopes := []string{"admin"}

	if parsedSubject, parsedScopes, ok := parseJWTPrincipal(token); ok {
		if parsedSubject != "" {
			subject = parsedSubject
		}
		if len(parsedScopes) > 0 {
			scopes = parsedScopes
		} else {
			scopes = nil
		}
	}

	if len(scopes) == 0 {
		scopes = nil
	}

	return SessionPrincipal{
		Subject: subject,
		Scopes:  scopes,
	}
}

func parseBearerToken(header string) string {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func parseJWTPrincipal(token string) (string, []string, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return "", nil, false
	}

	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, false
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return "", nil, false
	}

	subject, _ := payload["sub"].(string)
	scopes := parseScopeClaims(payload["scope"])
	if len(scopes) == 0 {
		scopes = parseScopeClaims(payload["scopes"])
	}
	if len(scopes) == 0 {
		scopes = parseScopeClaims(payload["scp"])
	}
	for _, role := range parseScopeClaims(payload["roles"]) {
		if role == "admin" {
			scopes = append(scopes, "admin")
			break
		}
	}

	if len(scopes) > 0 {
		// De-duplicate while preserving order.
		normalized := make([]string, 0, len(scopes))
		seen := make(map[string]struct{}, len(scopes))
		for _, scope := range scopes {
			trimmed := strings.TrimSpace(scope)
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			normalized = append(normalized, trimmed)
		}
		scopes = normalized
	}

	return strings.TrimSpace(subject), scopes, true
}

func parseScopeClaims(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		parts := strings.Fields(typed)
		if len(parts) == 0 {
			return nil
		}
		return parts
	case []string:
		result := make([]string, 0, len(typed))
		for _, scope := range typed {
			trimmed := strings.TrimSpace(scope)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if asString, ok := item.(string); ok {
				trimmed := strings.TrimSpace(asString)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}
		return result
	default:
		return nil
	}
}

func requireToolScopes(tool ToolSpec, principal SessionPrincipal) error {
	return policy.RequireScopes(tool.Name, tool.RequiredScopes, principal.Scopes)
}
