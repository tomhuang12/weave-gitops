package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
)

const (
	// StateCookieName is the name of the cookie that holds state during auth flow.
	StateCookieName = "state"
	// IDTokenCookieName is the name of the cookie that holds the ID Token once
	// the user has authenticated successfully with the OIDC Provider.
	IDTokenCookieName = "id_token"
	// RefreshTokenCookieName is the name of the cookie that holds the refresh
	// token.
	RefreshTokenCookieName = "refresh_token"
	// ScopeProfile is the "profile" scope
	scopeProfile = "profile"
	// ScopeEmail is the "email" scope
	scopeEmail = "email"
	// ScopeGroups is the "groups" scope
	scopeGroups = "groups"
)

// RegisterAuthServer registers the /callback route under a specified prefix.
// This route is called by the OIDC Provider in order to pass back state after
// the authentication flow completes.
func RegisterAuthServer(mux *http.ServeMux, prefix string, srv *AuthServer) {
	mux.Handle(prefix, srv.OAuth2Flow())
	mux.Handle(prefix+"/callback", srv.Callback())
	mux.Handle(prefix+"/sign_in", srv.SignIn())
	mux.Handle(prefix+"/userinfo", srv.UserInfo())
	mux.Handle(prefix+"/logout", srv.Logout())
}

type principalCtxKey struct{}

// Principal gets the principal from the context.
func Principal(ctx context.Context) *UserPrincipal {
	principal, ok := ctx.Value(principalCtxKey{}).(*UserPrincipal)
	if ok {
		return principal
	}

	return nil
}

// UserPrincipal is a simple model for the user, including their ID and Groups.
type UserPrincipal struct {
	ID     string   `json:"id"`
	Groups []string `json:"groups"`
}

// WithPrincipal sets the principal into the context.
func WithPrincipal(ctx context.Context, p *UserPrincipal) context.Context {
	return context.WithValue(ctx, principalCtxKey{}, p)
}

// WithAPIAuth middleware adds auth validation to API handlers.
//
// Unauthorized requests will be denied with a 401 status code.
func WithAPIAuth(next http.Handler, srv *AuthServer, publicRoutes []string) http.Handler {
	adminAuth := NewJWTAdminCookiePrincipalGetter(srv.logger, srv.tokenSignerVerifier, IDTokenCookieName)
	multi := MultiAuthPrincipal{adminAuth}

	if srv.oidcEnabled() {
		headerAuth := NewJWTAuthorizationHeaderPrincipalGetter(srv.logger, srv.verifier())
		cookieAuth := NewJWTCookiePrincipalGetter(srv.logger, srv.verifier(), IDTokenCookieName)
		multi = append(multi, headerAuth, cookieAuth)
	}

	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if IsPublicRoute(r.URL, publicRoutes) {
			next.ServeHTTP(rw, r)
			return
		}

		principal, err := multi.Principal(r)
		if err != nil {
			srv.logger.Error(err, "failed to get principal")
		}

		if principal == nil || err != nil {
			http.Error(rw, "Authentication required", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(rw, r.Clone(WithPrincipal(r.Context(), principal)))
	})
}

func generateNonce() (string, error) {
	b := make([]byte, 32)

	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

func IsPublicRoute(u *url.URL, publicRoutes []string) bool {
	for _, pr := range publicRoutes {
		if u.Path == pr {
			return true
		}
	}

	return false
}
