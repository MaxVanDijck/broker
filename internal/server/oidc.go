package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidcVerifier == nil {
		http.Error(w, "oidc not configured", http.StatusNotImplemented)
		return
	}

	state := generateRandomState()
	http.SetCookie(w, &http.Cookie{
		Name:     "broker_oauth_state",
		Value:    state,
		Path:     "/auth/callback",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	url := s.oidcVerifier.OAuth2Config().AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidcVerifier == nil {
		http.Error(w, "oidc not configured", http.StatusNotImplemented)
		return
	}

	stateCookie, err := r.Cookie("broker_oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "broker_oauth_state",
		Value:  "",
		Path:   "/auth/callback",
		MaxAge: -1,
	})

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		http.Error(w, fmt.Sprintf("authentication failed: %s - %s", errParam, desc), http.StatusUnauthorized)
		return
	}

	code := r.URL.Query().Get("code")
	token, err := s.oidcVerifier.OAuth2Config().Exchange(r.Context(), code)
	if err != nil {
		s.logger.Error("failed to exchange auth code", "error", err)
		http.Error(w, "failed to exchange authorization code", http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in response", http.StatusInternalServerError)
		return
	}

	claims, err := s.oidcVerifier.VerifyToken(r.Context(), rawIDToken)
	if err != nil {
		s.logger.Error("id token verification failed", "error", err)
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}

	s.logger.Info("user authenticated via oidc", "email", claims.Email, "subject", claims.Subject)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token":  token.AccessToken,
		"id_token":      rawIDToken,
		"refresh_token": token.RefreshToken,
		"token_type":    token.TokenType,
		"expiry":        token.Expiry,
		"email":         claims.Email,
		"name":          claims.Name,
	})
}

func (s *Server) handleAuthUserinfo(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(claims)
}

func generateRandomState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
