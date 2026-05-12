// Package whmcs implements the typed HTTP clients the plugin uses against a
// WHMCS upstream: the OAuth endpoints (/oauth/authorize.php, /oauth/token.php,
// /oauth/userinfo.php) and the admin API (/includes/api.php).
package whmcs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpTimeout is the per-request timeout for upstream WHMCS calls.
const httpTimeout = 30 * time.Second

// GeneratePKCE returns a 64-char URL-safe verifier + its S256 challenge.
// The verifier conforms to RFC 7636 §4.1 (43..128 chars); the challenge is
// the base64url(SHA256(verifier)).
func GeneratePKCE() (verifier, challenge string) {
	b := make([]byte, 48)
	_, _ = rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// AuthorizeParams describes the inputs for the authorization-code request URL.
type AuthorizeParams struct {
	ServerURL           string
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string // defaults to "S256"
	Scope               string // defaults to "openid profile email"
}

// BuildAuthorizeURL returns the URL the host redirects the user to.
func BuildAuthorizeURL(p AuthorizeParams) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", p.RedirectURI)
	scope := p.Scope
	if scope == "" {
		scope = "openid profile email"
	}
	q.Set("scope", scope)
	q.Set("state", p.State)
	if p.CodeChallenge != "" {
		q.Set("code_challenge", p.CodeChallenge)
		method := p.CodeChallengeMethod
		if method == "" {
			method = "S256"
		}
		q.Set("code_challenge_method", method)
	}
	return strings.TrimRight(p.ServerURL, "/") + "/oauth/authorize.php?" + q.Encode()
}

// TokenResponse is the JSON shape returned by WHMCS's /oauth/token.php.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// ExchangeParams describes the inputs for an authorization_code → token swap.
type ExchangeParams struct {
	ServerURL    string
	ClientID     string
	ClientSecret string
	Code         string
	RedirectURI  string
	CodeVerifier string
}

// ExchangeCode POSTs to /oauth/token.php and decodes the JSON response.
func ExchangeCode(ctx context.Context, p ExchangeParams) (TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", p.Code)
	form.Set("redirect_uri", p.RedirectURI)
	form.Set("code_verifier", p.CodeVerifier)
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)

	endpoint := strings.TrimRight(p.ServerURL, "/") + "/oauth/token.php"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	hc := &http.Client{Timeout: httpTimeout}
	resp, err := hc.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return TokenResponse{}, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, string(body))
	}
	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return TokenResponse{}, fmt.Errorf("decode token response: %w", err)
	}
	return tok, nil
}

// UserInfo is the JSON shape returned by WHMCS's /oauth/userinfo.php.
type UserInfo struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Picture    string `json:"picture"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
}

// FetchUserInfo GETs /oauth/userinfo.php with the access token in the
// Authorization header.
func FetchUserInfo(ctx context.Context, serverURL, accessToken string) (UserInfo, error) {
	endpoint := strings.TrimRight(serverURL, "/") + "/oauth/userinfo.php"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return UserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	hc := &http.Client{Timeout: httpTimeout}
	resp, err := hc.Do(req)
	if err != nil {
		return UserInfo{}, fmt.Errorf("userinfo endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return UserInfo{}, fmt.Errorf("userinfo endpoint %d: %s", resp.StatusCode, string(body))
	}
	var ui UserInfo
	if err := json.Unmarshal(body, &ui); err != nil {
		return UserInfo{}, fmt.Errorf("decode userinfo: %w", err)
	}
	return ui, nil
}

// DecodeIDToken splits the JWT and base64-decodes the body. The signature is
// NOT verified — v1 trusts the TLS connection to the token endpoint, which is
// parity with the librarymanagerre reference implementation. Returns the
// claims map.
func DecodeIDToken(idToken string) (map[string]any, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed id_token: expected at least 2 segments")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Tolerate standard padding too — some issuers add it.
		body, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("decode id_token body: %w", err)
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal id_token claims: %w", err)
	}
	return claims, nil
}
