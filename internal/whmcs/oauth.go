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

	"github.com/hashicorp/go-hclog"
)

// httpTimeout is the per-request timeout for upstream WHMCS calls.
const httpTimeout = 30 * time.Second

// maxResponseBytes caps the body the WHMCS HTTP clients (oauth.go + api.go)
// will read from upstream. Token, userinfo, and admin-API JSON payloads are
// well under this in normal operation; the cap defends against memory
// exhaustion if a misbehaving or hostile WHMCS instance streams a runaway
// response.
const maxResponseBytes = 10 << 20 // 10 MiB

// sharedClient is the single http.Client used for all upstream WHMCS calls
// (token, userinfo, admin API). Reusing one client lets the transport pool and
// reuse TCP/TLS connections instead of allocating a fresh pool per login.
var sharedClient = &http.Client{Timeout: httpTimeout}

// logger receives server-side diagnostics for upstream failures. Raw upstream
// response bodies are logged here (never returned in errors, which surface to
// admin-facing JSON). Defaults to a null logger; main.go may override it.
var logger hclog.Logger = hclog.NewNullLogger()

// SetLogger installs the package logger used for upstream-failure diagnostics.
func SetLogger(l hclog.Logger) {
	if l != nil {
		logger = l
	}
}

// doForm issues a request to endpoint with the given method. When form is
// non-nil it is sent url-encoded as the body (POST-style); headers are applied
// last. The response body is read (bounded) and returned together with the
// status code. Transport errors are wrapped with opName for context; HTTP
// status interpretation is left to the caller.
func doForm(ctx context.Context, method, endpoint, opName string, form url.Values, headers map[string]string) (status int, body []byte, err error) {
	var bodyReader io.Reader
	if form != nil {
		bodyReader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := sharedClient.Do(req)
	if err != nil {
		// Transport failures (DNS, connection refused, timeout) are transient;
		// mark them retryable so idempotent admin GETs wrapped in retry() get a
		// second chance instead of denying an entitled login on a single blip.
		return 0, nil, markRetryable(fmt.Errorf("%s: %w", opName, err))
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return resp.StatusCode, nil, markRetryable(fmt.Errorf("%s read body: %w", opName, err))
	}
	return resp.StatusCode, b, nil
}

// GeneratePKCE returns a 64-char URL-safe verifier + its S256 challenge.
// The verifier conforms to RFC 7636 §4.1 (43..128 chars); the challenge is
// the base64url(SHA256(verifier)).
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
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
	statusCode, body, err := doForm(ctx, http.MethodPost, endpoint, "token endpoint", form, nil)
	if err != nil {
		return TokenResponse{}, err
	}
	if statusCode >= 400 {
		// Log the raw upstream body server-side for debugging, but return a
		// generic error so the response body never leaks into admin-facing JSON.
		logger.Debug("token endpoint error", "status", statusCode, "body", string(body))
		return TokenResponse{}, fmt.Errorf("token endpoint returned status %d", statusCode)
	}
	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return TokenResponse{}, fmt.Errorf("decode token response: %w", err)
	}
	tok.AccessToken = strings.TrimSpace(tok.AccessToken)
	tok.RefreshToken = strings.TrimSpace(tok.RefreshToken)
	tok.TokenType = strings.TrimSpace(tok.TokenType)
	if tok.AccessToken == "" {
		return TokenResponse{}, fmt.Errorf("token response missing access_token")
	}
	if tok.TokenType != "" && !strings.EqualFold(tok.TokenType, "Bearer") {
		return TokenResponse{}, fmt.Errorf("unsupported token_type %q", tok.TokenType)
	}
	return tok, nil
}

// RefreshParams describes the inputs for a refresh_token → token swap against
// /oauth/token.php.
type RefreshParams struct {
	ServerURL    string
	ClientID     string
	ClientSecret string
	RefreshToken string
}

// RefreshAccessToken POSTs grant_type=refresh_token to /oauth/token.php and
// decodes the JSON response. WHMCS may (rotation) or may not return a new
// refresh_token; when omitted the caller should keep reusing the previous one.
func RefreshAccessToken(ctx context.Context, p RefreshParams) (TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", p.RefreshToken)
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)

	endpoint := strings.TrimRight(p.ServerURL, "/") + "/oauth/token.php"
	statusCode, body, err := doForm(ctx, http.MethodPost, endpoint, "refresh token endpoint", form, nil)
	if err != nil {
		return TokenResponse{}, err
	}
	if statusCode >= 400 {
		logger.Debug("refresh token endpoint error", "status", statusCode, "body", string(body))
		return TokenResponse{}, fmt.Errorf("refresh token endpoint returned status %d", statusCode)
	}
	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return TokenResponse{}, fmt.Errorf("decode refresh token response: %w", err)
	}
	tok.AccessToken = strings.TrimSpace(tok.AccessToken)
	tok.RefreshToken = strings.TrimSpace(tok.RefreshToken)
	tok.TokenType = strings.TrimSpace(tok.TokenType)
	if tok.AccessToken == "" {
		return TokenResponse{}, fmt.Errorf("refresh token response missing access_token")
	}
	if tok.TokenType != "" && !strings.EqualFold(tok.TokenType, "Bearer") {
		return TokenResponse{}, fmt.Errorf("unsupported token_type %q", tok.TokenType)
	}
	return tok, nil
}

// UserInfo is the JSON shape returned by WHMCS's /oauth/userinfo.php.
type UserInfo struct {
	Sub        string `json:"sub"`
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
	statusCode, body, err := doForm(ctx, http.MethodGet, endpoint, "userinfo endpoint", nil,
		map[string]string{"Authorization": "Bearer " + accessToken})
	if err != nil {
		return UserInfo{}, err
	}
	if statusCode >= 400 {
		// Log raw upstream body server-side; return a generic error so the body
		// never reaches admin-facing JSON.
		logger.Debug("userinfo endpoint error", "status", statusCode, "body", string(body))
		return UserInfo{}, fmt.Errorf("userinfo endpoint returned status %d", statusCode)
	}
	var ui UserInfo
	if err := json.Unmarshal(body, &ui); err != nil {
		return UserInfo{}, fmt.Errorf("decode userinfo: %w", err)
	}
	ui.Sub = strings.TrimSpace(ui.Sub)
	ui.ID = strings.TrimSpace(ui.ID)
	ui.Email = strings.TrimSpace(ui.Email)
	if ui.Sub == "" {
		ui.Sub = ui.ID
	}
	if ui.Sub == "" {
		return UserInfo{}, fmt.Errorf("userinfo response missing subject")
	}
	return ui, nil
}
