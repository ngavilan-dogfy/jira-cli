package jira

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	AuthorizeURL = "https://auth.atlassian.com/authorize"
	TokenURL     = "https://auth.atlassian.com/oauth/token"
	ResourcesURL = "https://api.atlassian.com/oauth/token/accessible-resources"
	CallbackPort = 19876
	Scopes       = "read:jira-work write:jira-work read:jira-user offline_access read:me"
)

type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

type CloudSite struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Scopes    []string `json:"scopes"`
	AvatarURL string   `json:"avatarUrl"`
}

type OAuthResult struct {
	Tokens    OAuthTokens
	Sites     []CloudSite
	ExpiresAt time.Time
}

func CallbackURL() string {
	return fmt.Sprintf("http://localhost:%d/callback", CallbackPort)
}

func pkceVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// OAuthLogin starts the OAuth 2.0 (3LO) flow with PKCE.
// Returns the browser URL and channels for the result.
func OAuthLogin(clientID, clientSecret string) (string, <-chan OAuthResult, <-chan error, error) {
	verifier, err := pkceVerifier()
	if err != nil {
		return "", nil, nil, err
	}
	challenge := pkceChallenge(verifier)

	state, err := randomState()
	if err != nil {
		return "", nil, nil, err
	}

	resultCh := make(chan OAuthResult, 1)
	errCh := make(chan error, 1)

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", CallbackPort))
	if err != nil {
		return "", nil, nil, fmt.Errorf("port %d busy — close whatever uses it and retry", CallbackPort)
	}

	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			go func() {
				time.Sleep(300 * time.Millisecond)
				server.Shutdown(context.Background())
			}()
		}()

		if e := r.URL.Query().Get("error"); e != "" {
			desc := r.URL.Query().Get("error_description")
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page("Auth failed", e+": "+desc))
			errCh <- fmt.Errorf("%s: %s", e, desc)
			return
		}

		code := r.URL.Query().Get("code")
		if r.URL.Query().Get("state") != state {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page("Error", "State mismatch — try again"))
			errCh <- fmt.Errorf("state mismatch")
			return
		}

		tokens, err := exchange(clientID, clientSecret, code, verifier)
		if err != nil {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page("Error", err.Error()))
			errCh <- err
			return
		}

		sites, err := fetchSites(tokens.AccessToken)
		if err != nil {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page("Error", err.Error()))
			errCh <- err
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page("Authenticated!", "You can close this tab."))

		resultCh <- OAuthResult{
			Tokens:    *tokens,
			Sites:     sites,
			ExpiresAt: time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
		}
	})

	go server.Serve(listener)

	params := url.Values{}
	params.Set("audience", "api.atlassian.com")
	params.Set("client_id", clientID)
	params.Set("scope", Scopes)
	params.Set("redirect_uri", CallbackURL())
	params.Set("state", state)
	params.Set("response_type", "code")
	params.Set("prompt", "consent")
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")

	return AuthorizeURL + "?" + params.Encode(), resultCh, errCh, nil
}

var authHTTPClient = &http.Client{Timeout: 15 * time.Second}

func exchange(clientID, clientSecret, code, verifier string) (*OAuthTokens, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", clientID)
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}
	data.Set("code", code)
	data.Set("redirect_uri", CallbackURL())
	data.Set("code_verifier", verifier)

	resp, err := authHTTPClient.Post(refreshURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokens OAuthTokens
	return &tokens, json.Unmarshal(body, &tokens)
}

func fetchSites(accessToken string) ([]CloudSite, error) {
	req, _ := http.NewRequest("GET", ResourcesURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := authHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get sites (%d): %s", resp.StatusCode, string(body))
	}

	var sites []CloudSite
	return sites, json.Unmarshal(body, &sites)
}

// refreshURL is TokenURL, swappable in tests.
var refreshURL = TokenURL

// RefreshToken exchanges a refresh token for new tokens.
func RefreshToken(clientID, clientSecret, refreshToken string) (*OAuthTokens, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientID)
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}
	data.Set("refresh_token", refreshToken)

	resp, err := authHTTPClient.Post(refreshURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token refresh failed (check network): %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokens OAuthTokens
	return &tokens, json.Unmarshal(body, &tokens)
}

func page(title, msg string) string {
	return fmt.Sprintf(`<!DOCTYPE html><html><head><style>
body{font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#0d1117;color:#e6edf3}
.card{text-align:center;padding:2rem;border:1px solid #30363d;border-radius:12px;background:#161b22}
h2{color:#7c3aed}p{color:#8b949e}
</style></head><body><div class="card"><h2>%s</h2><p>%s</p></div></body></html>`, title, msg)
}
