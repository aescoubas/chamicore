//go:build system

package system

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	authclient "git.cscs.ch/openchami/chamicore-auth/pkg/client"
	authtypes "git.cscs.ch/openchami/chamicore-auth/pkg/types"
	baseclient "git.cscs.ch/openchami/chamicore-lib/httputil/client"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
)

func TestAuthFlow_IssueUseAndRejectBadToken(t *testing.T) {
	endpoints := systemEndpoints()
	waitForReadiness(t, "auth", endpoints.auth)
	waitForReadiness(t, "smd", endpoints.smd)

	authSDK := authclient.New(authclient.Config{
		BaseURL: endpoints.auth,
	})

	ctx, cancel := systemContext(t)
	defer cancel()

	issuedToken, err := issueTestToken(ctx, authSDK, endpoints.auth)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if issuedToken == "" {
		t.Fatalf("issued token is empty")
	}

	smd := smdclient.New(smdclient.Config{
		BaseURL: endpoints.smd,
		Token:   issuedToken,
	})
	if _, err := smd.ListComponents(ctx, smdclient.ComponentListOptions{Limit: 1}); err != nil {
		t.Fatalf("list components with issued token: %v", err)
	}

	_, err = authSDK.ExchangeToken(ctx, authtypes.TokenRequest{
		GrantType:    "urn:ietf:params:oauth:grant-type:token-exchange",
		SubjectToken: "not.a.jwt",
	})
	if err == nil {
		t.Fatalf("expected invalid subject_token to be rejected")
	}
	requireAPIErrorStatus(t, err, http.StatusUnauthorized)
}

func issueTestToken(ctx context.Context, authSDK *authclient.Client, authURL string) (string, error) {
	resp, err := authSDK.ExchangeToken(ctx, authtypes.TokenRequest{GrantType: "dev_mode"})
	if err == nil {
		return resp.AccessToken, nil
	}

	var apiErr *baseclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return "", err
	}

	adminClient := authclient.New(authclient.Config{
		BaseURL: authURL,
		Token:   authToken(),
	})
	serviceAccountName := uniqueID("system-sa")
	created, createErr := adminClient.CreateServiceAccount(ctx, authtypes.CreateServiceAccountRequest{
		Name:   serviceAccountName,
		Scopes: []string{"read:components"},
	})
	if createErr != nil {
		return "", createErr
	}

	tokResp, exchangeErr := authSDK.ExchangeToken(ctx, authtypes.TokenRequest{
		GrantType:    "client_credentials",
		ClientID:     created.Spec.ClientID,
		ClientSecret: created.Spec.ClientSecret,
	})
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cleanupCancel()
	_ = adminClient.DeleteServiceAccount(cleanupCtx, created.Metadata.ID)
	if exchangeErr != nil {
		return "", exchangeErr
	}
	return tokResp.AccessToken, nil
}
