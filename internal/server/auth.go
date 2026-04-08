package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"strings"

	"broker/internal/auth"
)

type contextKey string

const claimsKey contextKey = "auth_claims"

func ClaimsFromContext(ctx context.Context) *auth.Claims {
	c, _ := ctx.Value(claimsKey).(*auth.Claims)
	return c
}

func authMiddleware(verifier *auth.Verifier, next http.Handler) http.Handler {
	token := os.Getenv("BROKER_TOKEN")

	if token == "" && verifier == nil {
		return next
	}

	var expectedBasic string
	if token != "" {
		expectedBasic = base64.StdEncoding.EncodeToString([]byte("broker:" + token))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Endpoints that must be accessible without authentication:
		// - Health/readiness probes (load balancers, orchestrators)
		// - Agent binary download (EC2 bootstrap before agent has token context)
		// - Auth flow endpoints (initiate and complete OIDC login)
		// - Dashboard static assets (the SPA must load before it can authenticate)
		switch {
		case path == "/healthz",
			path == "/readyz",
			path == "/agent/v1/binary",
			path == "/auth/login",
			path == "/auth/callback":
			next.ServeHTTP(w, r)
			return
		}

		// The dashboard is an SPA served from "/". Its HTML, JS, and CSS must
		// be loadable without auth so the browser can render the login page.
		// API routes (/api/, /broker.v1., /agent/) are always authenticated.
		if !strings.HasPrefix(path, "/api/") &&
			!strings.HasPrefix(path, "/broker.v1.") &&
			!strings.HasPrefix(path, "/agent/") &&
			!strings.HasPrefix(path, "/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		authorization := r.Header.Get("Authorization")

		// SSE (EventSource) cannot send custom headers. The dashboard passes
		// the token as a query parameter for the /api/v1/events endpoint.
		if authorization == "" && r.URL.Query().Get("token") != "" {
			qToken := r.URL.Query().Get("token")
			authorization = "Basic " + base64.StdEncoding.EncodeToString([]byte("broker:"+qToken))
		}

		if strings.HasPrefix(authorization, "Bearer ") {
			rawToken := strings.TrimPrefix(authorization, "Bearer ")
			if verifier == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			claims, err := verifier.VerifyToken(r.Context(), rawToken)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		if strings.HasPrefix(authorization, "Basic ") {
			provided := strings.TrimPrefix(authorization, "Basic ")
			if expectedBasic == "" || provided != expectedBasic {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
