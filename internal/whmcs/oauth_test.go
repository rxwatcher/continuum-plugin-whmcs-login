package whmcs_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
)

func TestPKCE_GeneratesValidVerifierAndChallenge(t *testing.T) {
	v, c := whmcs.GeneratePKCE()
	if len(v) < 43 || len(v) > 128 {
		t.Errorf("verifier len = %d (expected 43..128)", len(v))
	}
	// Challenge is base64url(SHA256(verifier)) — 43 chars no padding.
	if len(c) != 43 {
		t.Errorf("challenge len = %d", len(c))
	}
	if strings.ContainsAny(c, "+/=") {
		t.Errorf("challenge must be url-safe base64: %q", c)
	}
}

func TestPKCE_Unique(t *testing.T) {
	v1, _ := whmcs.GeneratePKCE()
	v2, _ := whmcs.GeneratePKCE()
	if v1 == v2 {
		t.Errorf("two calls returned same verifier")
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	u := whmcs.BuildAuthorizeURL(whmcs.AuthorizeParams{
		ServerURL:           "https://billing.example",
		ClientID:            "client-1",
		RedirectURI:         "https://app.example/cb",
		State:               "state-1",
		CodeChallenge:       "chall",
		CodeChallengeMethod: "S256",
	})
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Path != "/oauth/authorize.php" {
		t.Errorf("path = %s", parsed.Path)
	}
	q := parsed.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("scope") != "openid profile email" {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Get("code_challenge") != "chall" {
		t.Errorf("code_challenge = %q", q.Get("code_challenge"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("state") != "state-1" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("redirect_uri") != "https://app.example/cb" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
}

func TestBuildAuthorizeURL_StripsTrailingSlash(t *testing.T) {
	u := whmcs.BuildAuthorizeURL(whmcs.AuthorizeParams{
		ServerURL:   "https://billing.example/",
		ClientID:    "c",
		RedirectURI: "https://app/cb",
		State:       "s",
	})
	if !strings.HasPrefix(u, "https://billing.example/oauth/authorize.php?") {
		t.Errorf("URL = %s", u)
	}
}

func TestExchangeCode_PostsCorrectFormAndDecodesTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token.php" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content-type = %s", ct)
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "auth-code" {
			t.Errorf("code = %q", r.Form.Get("code"))
		}
		if r.Form.Get("code_verifier") != "verifier-x" {
			t.Errorf("verifier = %q", r.Form.Get("code_verifier"))
		}
		if r.Form.Get("redirect_uri") != "https://app/cb" {
			t.Errorf("redirect_uri = %q", r.Form.Get("redirect_uri"))
		}
		if r.Form.Get("client_id") != "c" || r.Form.Get("client_secret") != "s" {
			t.Errorf("client creds = %v", r.Form)
		}
		_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	tok, err := whmcs.ExchangeCode(context.Background(), whmcs.ExchangeParams{
		ServerURL:    srv.URL,
		ClientID:     "c", ClientSecret: "s",
		Code:         "auth-code",
		RedirectURI:  "https://app/cb",
		CodeVerifier: "verifier-x",
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "AT" || tok.IDToken != "a.b.c" || tok.ExpiresIn != 3600 {
		t.Errorf("tok = %+v", tok)
	}
}

func TestExchangeCode_NonOKBubblesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	_, err := whmcs.ExchangeCode(context.Background(), whmcs.ExchangeParams{
		ServerURL: srv.URL, ClientID: "c", ClientSecret: "s", Code: "x", RedirectURI: "/cb", CodeVerifier: "v",
	})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 in error, got: %v", err)
	}
}

func TestFetchUserInfo_AttachesBearerAndDecodes(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/oauth/userinfo.php" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User","picture":"https://x/u.png","given_name":"U","family_name":"R"}`))
	}))
	defer srv.Close()

	ui, err := whmcs.FetchUserInfo(context.Background(), srv.URL, "AT")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if gotAuth != "Bearer AT" {
		t.Errorf("auth = %q", gotAuth)
	}
	if ui.ID != "42" || ui.Email != "u@x.com" || ui.Name != "User" || ui.GivenName != "U" || ui.FamilyName != "R" {
		t.Errorf("ui = %+v", ui)
	}
}

func TestDecodeIDToken_ExtractsBodyClaims(t *testing.T) {
	body := map[string]any{"sub": "42", "email": "u@x.com", "name": "User"}
	bodyJSON, _ := json.Marshal(body)
	parts := []string{
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)),
		base64.RawURLEncoding.EncodeToString(bodyJSON),
		"sig",
	}
	tok := strings.Join(parts, ".")
	claims, err := whmcs.DecodeIDToken(tok)
	if err != nil {
		t.Fatalf("DecodeIDToken: %v", err)
	}
	if claims["sub"] != "42" || claims["email"] != "u@x.com" {
		t.Errorf("claims = %v", claims)
	}
}

func TestDecodeIDToken_RejectsMalformed(t *testing.T) {
	if _, err := whmcs.DecodeIDToken("notajwt"); err == nil {
		t.Error("expected error for malformed token")
	}
}
