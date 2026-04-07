package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Audience     string
	Scopes       []string
	RedirectURL  string
}

func (c *OIDCConfig) Enabled() bool {
	return c.Issuer != "" && c.ClientID != ""
}

type Claims struct {
	Subject string   `json:"subject"`
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Groups  []string `json:"groups"`
}

type Verifier struct {
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config *oauth2.Config
	logger       *slog.Logger
}

func NewVerifier(ctx context.Context, cfg OIDCConfig, logger *slog.Logger) (*Verifier, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	verifierConfig := &oidc.Config{
		ClientID: cfg.ClientID,
	}
	if cfg.Audience != "" {
		verifierConfig.ClientID = cfg.Audience
	}

	scopes := []string{oidc.ScopeOpenID, "profile", "email"}
	if len(cfg.Scopes) > 0 {
		scopes = append(scopes, cfg.Scopes...)
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
		RedirectURL:  cfg.RedirectURL,
	}

	return &Verifier{
		provider:     provider,
		verifier:     provider.Verifier(verifierConfig),
		oauth2Config: oauth2Cfg,
		logger:       logger,
	}, nil
}

func (v *Verifier) VerifyToken(ctx context.Context, rawToken string) (*Claims, error) {
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}

	var claims struct {
		Email  string   `json:"email"`
		Name   string   `json:"name"`
		Groups []string `json:"groups"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	return &Claims{
		Subject: idToken.Subject,
		Email:   claims.Email,
		Name:    claims.Name,
		Groups:  claims.Groups,
	}, nil
}

func (v *Verifier) OAuth2Config() *oauth2.Config {
	return v.oauth2Config
}

func (v *Verifier) Provider() *oidc.Provider {
	return v.provider
}
