package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

type storedCredentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"client_id"`
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".broker", "credentials.json")
}

func loadCredentials() (*storedCredentials, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		return nil, err
	}
	var creds storedCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

func saveCredentials(creds *storedCredentials) error {
	path := credentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func deleteCredentials() error {
	return os.Remove(credentialsPath())
}

func brokerAccessToken() string {
	creds, err := loadCredentials()
	if err != nil {
		return ""
	}
	if creds.AccessToken == "" {
		return ""
	}
	if !creds.Expiry.IsZero() && time.Now().After(creds.Expiry) {
		if creds.RefreshToken == "" {
			return ""
		}
		refreshed, err := refreshToken(creds)
		if err != nil {
			return ""
		}
		return refreshed.AccessToken
	}
	return creds.AccessToken
}

type oidcDiscovery struct {
	AuthEndpoint  string `json:"authorization_endpoint"`
	TokenEndpoint string `json:"token_endpoint"`
}

func discoverOIDC(issuer string) (*oidcDiscovery, error) {
	wellKnown := issuer + "/.well-known/openid-configuration"
	resp, err := http.Get(wellKnown) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer resp.Body.Close()

	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("parse oidc discovery: %w", err)
	}
	return &d, nil
}

func refreshToken(creds *storedCredentials) (*storedCredentials, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	discovery, err := discoverOIDC(creds.Issuer)
	if err != nil {
		return nil, err
	}

	oauth2Cfg := &oauth2.Config{
		ClientID: creds.ClientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: discovery.TokenEndpoint,
		},
	}

	token := &oauth2.Token{
		RefreshToken: creds.RefreshToken,
	}

	newToken, err := oauth2Cfg.TokenSource(ctx, token).Token()
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	creds.AccessToken = newToken.AccessToken
	creds.Expiry = newToken.Expiry
	if newToken.RefreshToken != "" {
		creds.RefreshToken = newToken.RefreshToken
	}

	if rawID, ok := newToken.Extra("id_token").(string); ok {
		creds.IDToken = rawID
	}

	if err := saveCredentials(creds); err != nil {
		return nil, fmt.Errorf("save credentials: %w", err)
	}

	return creds, nil
}

func loginCmd() *cobra.Command {
	var (
		issuer   string
		clientID string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with an OIDC provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			if issuer == "" || clientID == "" {
				return fmt.Errorf("--issuer and --client-id are required")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			discovery, err := discoverOIDC(issuer)
			if err != nil {
				return fmt.Errorf("oidc discovery failed: %w", err)
			}

			codeVerifier, err := generateCodeVerifier()
			if err != nil {
				return fmt.Errorf("generate code verifier: %w", err)
			}
			codeChallenge := generateCodeChallenge(codeVerifier)

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return fmt.Errorf("start callback server: %w", err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

			resultCh := make(chan *oauth2.Token, 1)
			errCh := make(chan error, 1)

			oauth2Cfg := &oauth2.Config{
				ClientID: clientID,
				Endpoint: oauth2.Endpoint{
					AuthURL:  discovery.AuthEndpoint,
					TokenURL: discovery.TokenEndpoint,
				},
				RedirectURL: redirectURI,
				Scopes:      []string{"openid", "profile", "email", "offline_access"},
			}

			stateBytes := make([]byte, 16)
			if _, err := rand.Read(stateBytes); err != nil {
				return fmt.Errorf("generate state: %w", err)
			}
			state := base64.RawURLEncoding.EncodeToString(stateBytes)

			mux := http.NewServeMux()
			srv := &http.Server{Handler: mux}

			mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("state") != state {
					errCh <- fmt.Errorf("invalid state parameter")
					fmt.Fprintf(w, "<html><body><h1>Authentication failed</h1><p>Invalid state parameter.</p></body></html>")
					return
				}

				if errParam := r.URL.Query().Get("error"); errParam != "" {
					desc := r.URL.Query().Get("error_description")
					errCh <- fmt.Errorf("auth error: %s - %s", errParam, desc)
					fmt.Fprintf(w, "<html><body><h1>Authentication failed</h1><p>%s: %s</p></body></html>", errParam, desc)
					return
				}

				code := r.URL.Query().Get("code")
				token, err := oauth2Cfg.Exchange(ctx, code,
					oauth2.SetAuthURLParam("code_verifier", codeVerifier),
				)
				if err != nil {
					errCh <- fmt.Errorf("token exchange: %w", err)
					fmt.Fprintf(w, "<html><body><h1>Authentication failed</h1><p>%s</p></body></html>", err.Error())
					return
				}

				resultCh <- token
				fmt.Fprintf(w, "<html><body><h1>Authentication successful</h1><p>You can close this window.</p></body></html>")
			})

			go srv.Serve(listener) //nolint:errcheck

			authURL := oauth2Cfg.AuthCodeURL(state,
				oauth2.SetAuthURLParam("code_challenge", codeChallenge),
				oauth2.SetAuthURLParam("code_challenge_method", "S256"),
			)

			fmt.Printf("Opening browser for authentication...\n")
			fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", authURL)
			openBrowser(authURL)

			select {
			case token := <-resultCh:
				srv.Shutdown(ctx) //nolint:errcheck

				rawIDToken, _ := token.Extra("id_token").(string)

				creds := &storedCredentials{
					AccessToken:  token.AccessToken,
					RefreshToken: token.RefreshToken,
					IDToken:      rawIDToken,
					TokenType:    token.TokenType,
					Expiry:       token.Expiry,
					Issuer:       issuer,
					ClientID:     clientID,
				}

				if err := saveCredentials(creds); err != nil {
					return fmt.Errorf("save credentials: %w", err)
				}

				fmt.Println("Login successful. Credentials saved.")
				return nil

			case err := <-errCh:
				srv.Shutdown(ctx) //nolint:errcheck
				return err

			case <-ctx.Done():
				srv.Shutdown(context.Background()) //nolint:errcheck
				return fmt.Errorf("login timed out")
			}
		},
	}

	cmd.Flags().StringVar(&issuer, "issuer", os.Getenv("BROKER_OIDC_ISSUER"), "OIDC issuer URL (e.g. https://dev-123456.okta.com)")
	cmd.Flags().StringVar(&clientID, "client-id", os.Getenv("BROKER_OIDC_CLIENT_ID"), "OIDC client ID")

	return cmd
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored authentication credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := deleteCredentials(); err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No credentials to remove.")
					return nil
				}
				return fmt.Errorf("failed to remove credentials: %w", err)
			}
			fmt.Println("Logged out. Credentials removed.")
			return nil
		},
	}
}

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	}
	if cmd != nil {
		cmd.Start() //nolint:errcheck
	}
}
