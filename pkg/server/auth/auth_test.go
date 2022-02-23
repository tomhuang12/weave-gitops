package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-logr/logr"
	"github.com/oauth2-proxy/mockoidc"
	"github.com/stretchr/testify/assert"
	"github.com/weaveworks/weave-gitops/pkg/server/auth"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWithAPIAuthReturns401ForUnauthenticatedRequests(t *testing.T) {
	ctx := context.Background()

	m, err := mockoidc.Run()
	if err != nil {
		t.Error("failed to create fake OIDC server")
	}

	defer func() {
		_ = m.Shutdown()
	}()

	fake := m.Config()
	mux := http.NewServeMux()
	fakeKubernetesClient := ctrlclient.NewClientBuilder().Build()

	tokenSignerVerifier, err := auth.NewHMACTokenSignerVerifier(5 * time.Minute)
	if err != nil {
		t.Errorf("failed to create HMAC signer: %v", err)
	}

	srv, err := auth.NewAuthServer(ctx, logr.Discard(), http.DefaultClient,
		auth.AuthConfig{
			auth.OIDCConfig{
				IssuerURL:     fake.Issuer,
				ClientID:      fake.ClientID,
				ClientSecret:  fake.ClientSecret,
				RedirectURL:   "",
				TokenDuration: 20 * time.Minute,
			},
		}, fakeKubernetesClient, tokenSignerVerifier)
	if err != nil {
		t.Error("failed to create auth config")
	}

	auth.RegisterAuthServer(mux, "/oauth2", srv)

	s := httptest.NewServer(mux)
	defer s.Close()

	// Set the correct redirect URL now that we have a server running
	srv.SetRedirectURL(s.URL + "/oauth2/callback")

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, s.URL, nil)
	auth.WithAPIAuth(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}), srv, nil).ServeHTTP(res, req)

	if res.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status of %d but got %d", http.StatusUnauthorized, res.Result().StatusCode)
	}

	// Test out the publicRoutes
	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, s.URL+"/v1/featureflags", nil)
	auth.WithAPIAuth(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}), srv, []string{"/v1/featureflags"}).ServeHTTP(res, req)

	if res.Result().StatusCode != http.StatusOK {
		t.Errorf("expected status of %d but got %d", http.StatusUnauthorized, res.Result().StatusCode)
	}
}

func TestOauth2FlowRedirectsToOIDCIssuerForUnauthenticatedRequests(t *testing.T) {
	ctx := context.Background()

	m, err := mockoidc.Run()
	if err != nil {
		t.Error("failed to create fake OIDC server")
	}

	defer func() {
		_ = m.Shutdown()
	}()

	fake := m.Config()
	mux := http.NewServeMux()
	fakeKubernetesClient := ctrlclient.NewClientBuilder().Build()

	tokenSignerVerifier, err := auth.NewHMACTokenSignerVerifier(5 * time.Minute)
	if err != nil {
		t.Errorf("failed to create HMAC signer: %v", err)
	}

	srv, err := auth.NewAuthServer(ctx, logr.Discard(), http.DefaultClient,
		auth.AuthConfig{
			auth.OIDCConfig{
				IssuerURL:     fake.Issuer,
				ClientID:      fake.ClientID,
				ClientSecret:  fake.ClientSecret,
				RedirectURL:   "",
				TokenDuration: 20 * time.Minute,
			},
		}, fakeKubernetesClient, tokenSignerVerifier)
	if err != nil {
		t.Error("failed to create auth config")
	}

	auth.RegisterAuthServer(mux, "/oauth2", srv)

	s := httptest.NewServer(mux)
	defer s.Close()

	// Set the correct redirect URL now that we have a server running
	redirectURL := s.URL + "/oauth2/callback"
	srv.SetRedirectURL(redirectURL)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, s.URL, nil)
	srv.OAuth2Flow().ServeHTTP(res, req)

	if res.Result().StatusCode != http.StatusSeeOther {
		t.Errorf("expected status of %d but got %d", http.StatusSeeOther, res.Result().StatusCode)
	}

	authCodeURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s", m.AuthorizationEndpoint(), fake.ClientID, url.QueryEscape(redirectURL), strings.Join([]string{"profile", oidc.ScopeOpenID, oidc.ScopeOfflineAccess, "email"}, "+"))
	if !strings.HasPrefix(res.Result().Header.Get("Location"), authCodeURL) {
		t.Errorf("expected Location header URL to include scopes %s but does not: %s", authCodeURL, res.Result().Header.Get("Location"))
	}
}

func TestIsPublicRoute(t *testing.T) {
	assert.True(t, auth.IsPublicRoute(&url.URL{Path: "/foo"}, []string{"/foo"}))
	assert.False(t, auth.IsPublicRoute(&url.URL{Path: "foo"}, []string{"/foo"}))
	assert.False(t, auth.IsPublicRoute(&url.URL{Path: "/foob"}, []string{"/foo"}))
}
