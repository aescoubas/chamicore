package server

import (
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenSessionAuthenticator_HTTPMissingConfiguredToken(t *testing.T) {
	authn := NewTokenSessionAuthenticator("")
	req := httptest.NewRequest("POST", "/mcp/v1/tools/call", nil)
	req.Header.Set("Authorization", "Bearer token")

	_, err := authn.AuthenticateHTTP(req)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionTokenMissing)
}

func TestTokenSessionAuthenticator_HTTPMissingBearerHeader(t *testing.T) {
	authn := NewTokenSessionAuthenticator("session-token")
	req := httptest.NewRequest("POST", "/mcp/v1/tools/call", nil)

	_, err := authn.AuthenticateHTTP(req)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBearerTokenMissing)
}

func TestTokenSessionAuthenticator_HTTPInvalidBearerToken(t *testing.T) {
	authn := NewTokenSessionAuthenticator("session-token")
	req := httptest.NewRequest("POST", "/mcp/v1/tools/call", nil)
	req.Header.Set("Authorization", "Bearer other-token")

	_, err := authn.AuthenticateHTTP(req)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBearerTokenInvalid)
}

func TestTokenSessionAuthenticator_HTTPJWTScopes(t *testing.T) {
	token := testJWTToken(t, "agent", []string{"read:groups"})
	authn := NewTokenSessionAuthenticator(token)
	req := httptest.NewRequest("POST", "/mcp/v1/tools/call", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	principal, err := authn.AuthenticateHTTP(req)
	require.NoError(t, err)
	require.Equal(t, "agent", principal.Subject)
	require.Equal(t, []string{"read:groups"}, principal.Scopes)
}

func TestTokenSessionAuthenticator_HTTPOpaqueTokenFallsBackToAdmin(t *testing.T) {
	authn := NewTokenSessionAuthenticator("opaque-session-token")
	req := httptest.NewRequest("POST", "/mcp/v1/tools/call", nil)
	req.Header.Set("Authorization", "Bearer opaque-session-token")

	principal, err := authn.AuthenticateHTTP(req)
	require.NoError(t, err)
	require.Equal(t, "mcp-session", principal.Subject)
	require.Equal(t, []string{"admin"}, principal.Scopes)
}

func TestTokenSessionAuthenticator_StdioRequiresToken(t *testing.T) {
	authn := NewTokenSessionAuthenticator("")

	_, err := authn.AuthenticateStdio()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionTokenMissing)
}

func TestTokenSessionAuthenticator_StdioJWTScopes(t *testing.T) {
	token := testJWTToken(t, "cli-agent", []string{"read:components"})
	authn := NewTokenSessionAuthenticator(token)

	principal, err := authn.AuthenticateStdio()
	require.NoError(t, err)
	require.Equal(t, "cli-agent", principal.Subject)
	require.Equal(t, []string{"read:components"}, principal.Scopes)
}

func testJWTToken(t *testing.T, subject string, scopes []string) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := fmt.Sprintf(`{"sub":%q,"scope":[%q]}`, subject, scopes[0])
	if len(scopes) > 1 {
		encodedScopes := ""
		for idx, scope := range scopes {
			if idx > 0 {
				encodedScopes += ","
			}
			encodedScopes += fmt.Sprintf("%q", scope)
		}
		payload = fmt.Sprintf(`{"sub":%q,"scope":[%s]}`, subject, encodedScopes)
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString([]byte(payload))

	return fmt.Sprintf("%s.%s.", header, payloadEncoded)
}
