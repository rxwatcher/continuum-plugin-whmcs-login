package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"

	"github.com/RXWatcher/silo-plugin-whmcs-login/internal/auth"
	pluginrt "github.com/RXWatcher/silo-plugin-whmcs-login/internal/runtime"
)

func newAuthServer(cfg pluginrt.Config) *auth.Server {
	return auth.NewServer(func() pluginrt.Config { return cfg })
}

func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	return s
}

func TestInitAuthorize_BuildsURLWithPKCE(t *testing.T) {
	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: "https://billing.example",
		ClientID:       "c1",
	})
	resp, err := s.InitAuthorize(context.Background(), &pluginv1.InitAuthorizeRequest{
		RedirectUri: "https://app.example/cb",
		State:       "state-1",
	})
	if err != nil {
		t.Fatalf("InitAuthorize: %v", err)
	}
	if !strings.HasPrefix(resp.GetAuthorizeUrl(), "https://billing.example/oauth/authorize.php?") {
		t.Errorf("URL = %s", resp.GetAuthorizeUrl())
	}
	parsed, _ := url.Parse(resp.GetAuthorizeUrl())
	q := parsed.Query()
	if q.Get("code_challenge") == "" {
		t.Errorf("expected PKCE challenge in URL: %s", resp.GetAuthorizeUrl())
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("state") != "state-1" {
		t.Errorf("state = %q", q.Get("state"))
	}
	ps := resp.GetProviderState().AsMap()
	verifier, _ := ps["pkce_verifier"].(string)
	if verifier == "" {
		t.Errorf("provider_state missing pkce_verifier: %v", ps)
	}
	if ps["state"] != "state-1" {
		t.Errorf("provider_state state = %v", ps["state"])
	}
}

func TestInitAuthorize_RejectsUnconfigured(t *testing.T) {
	s := newAuthServer(pluginrt.Config{})
	_, err := s.InitAuthorize(context.Background(), &pluginv1.InitAuthorizeRequest{
		RedirectUri: "/cb", State: "s",
	})
	if err == nil {
		t.Fatal("expected error when unconfigured")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestExchangeCode_RejectsStateMismatch(t *testing.T) {
	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: "https://x", ClientID: "c", ClientSecret: "s",
	})
	_, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code:        "x",
		State:       "wrong",
		RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{
			"pkce_verifier": "v",
			"state":         "expected",
		}),
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated; err = %v", status.Code(err), err)
	}
}

func TestExchangeCode_RejectsMissingState(t *testing.T) {
	// Fail closed: provider_state without a state must be rejected even though a
	// pkce_verifier is present. An empty expected state must never be treated as
	// "skip the check".
	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: "https://x", ClientID: "c", ClientSecret: "s",
	})
	_, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code:        "x",
		State:       "anything",
		RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{
			"pkce_verifier": "v",
			// state intentionally omitted
		}),
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated; err = %v", status.Code(err), err)
	}
}

func TestExchangeCode_LinkByEmail_DefaultOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	// Default config: LinkByEmail is false.
	s := newAuthServer(pluginrt.Config{WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s"})
	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if _, present := resp.GetClaims().AsMap()["silo_link_by_email"]; present {
		t.Error("silo_link_by_email must be absent when LinkByEmail is off")
	}
}

func TestExchangeCode_LinkByEmail_OptIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
		LinkByEmail: true,
	})
	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if resp.GetClaims().AsMap()["silo_link_by_email"] != true {
		t.Error("silo_link_by_email must be true when LinkByEmail is on")
	}
}

func TestExchangeCode_HappyPath_ReturnsAuthenticateResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_ = r.ParseForm()
			if r.Form.Get("code_verifier") != "verifier-x" {
				t.Errorf("verifier = %q", r.Form.Get("code_verifier"))
			}
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c","token_type":"Bearer","expires_in":3600}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL,
		ClientID:       "c1", ClientSecret: "s",
	}
	s := newAuthServer(cfg)

	pStateMap := map[string]any{"pkce_verifier": "verifier-x", "state": "state-1"}
	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code:          "auth-code",
		State:         "state-1",
		RedirectUri:   "https://app.example/cb",
		ProviderState: mustStruct(t, pStateMap),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if resp.GetExternalSubject() != "42" {
		t.Errorf("external_subject = %q", resp.GetExternalSubject())
	}
	if resp.GetEmail() != "u@x.com" || resp.GetDisplayName() != "User" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestExchangeCode_MissingPKCEVerifier(t *testing.T) {
	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: "https://x", ClientID: "c", ClientSecret: "s",
	})
	_, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code:          "x",
		State:         "s",
		RedirectUri:   "/cb",
		ProviderState: mustStruct(t, map[string]any{}),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("err = %v (want InvalidArgument)", err)
	}
}

func TestExchangeCode_ProductGating_RejectsWhenNoMatchingProduct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		case "/includes/api.php":
			_ = r.ParseForm()
			switch r.Form.Get("action") {
			case "GetClients":
				_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":"42","email":"u@x.com"}]}}`))
			case "GetClientsProducts":
				_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":99,"name":"Other","status":"Active"}]}}`))
			default:
				_, _ = w.Write([]byte(`{"result":"error","message":"unexpected action"}`))
			}
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL,
		ClientID:       "c", ClientSecret: "s",
		AllowedProductIDs: []string{"5"},
		WHMCSAdminAPIID:   "id", WHMCSAdminAPISecret: "sec",
	}
	s := newAuthServer(cfg)

	_, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err == nil {
		t.Fatal("expected PermissionDenied")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("code = %v, want PermissionDenied; err = %v", status.Code(err), err)
	}
}

func TestExchangeCode_ProductGating_AcceptsWhenMatching(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		case "/includes/api.php":
			_ = r.ParseForm()
			switch r.Form.Get("action") {
			case "GetClients":
				_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":"42","email":"u@x.com"}]}}`))
			case "GetClientsProducts":
				_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":5,"name":"Pro","status":"Active"},{"pid":99,"name":"Other","status":"Active"}]}}`))
			default:
				_, _ = w.Write([]byte(`{"result":"error","message":"unexpected action"}`))
			}
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
		AllowedProductIDs: []string{"5"},
		WHMCSAdminAPIID:   "id", WHMCSAdminAPISecret: "sec",
	}
	s := newAuthServer(cfg)

	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	claims := resp.GetClaims().AsMap()
	prods, _ := claims["products"].([]any)
	if len(prods) != 2 {
		t.Errorf("claims.products = %v", prods)
	}
}

func TestExchangeCode_DiscordIDClaim_FetchedFromCustomField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		case "/includes/api.php":
			_ = r.ParseForm()
			if r.Form.Get("action") == "GetClients" {
				_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":"42","email":"u@x.com"}]}}`))
				return
			}
			if r.Form.Get("action") == "GetClientsDetails" {
				_, _ = w.Write([]byte(`{"result":"success","client":{"id":"42","email":"u@x.com","customfields":[{"value":"183","fieldname":"Discord ID"}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"result":"error","message":"unexpected"}`))
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
		FetchDiscordID:       true,
		DiscordIDCustomField: "Discord ID",
		WHMCSAdminAPIID:      "id", WHMCSAdminAPISecret: "sec",
	}
	s := newAuthServer(cfg)

	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if resp.GetClaims().AsMap()["discord_id"] != "183" {
		t.Errorf("discord_id claim = %v", resp.GetClaims().AsMap()["discord_id"])
	}
}

func TestExchangeCode_DiscordIDFailureIsNonFatal(t *testing.T) {
	// GetClientsDetails returns a WHMCS error envelope while everything else
	// (token, userinfo, GetClients, GetClientsProducts) succeeds. The plugin
	// must let the login through with no discord_id claim rather than
	// failing the whole sign-in over a missing custom field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		case "/includes/api.php":
			_ = r.ParseForm()
			switch r.Form.Get("action") {
			case "GetClients":
				_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":"42","email":"u@x.com"}]}}`))
			case "GetClientsProducts":
				_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":5,"status":"Active"}]}}`))
			case "GetClientsDetails":
				_, _ = w.Write([]byte(`{"result":"error","message":"upstream blew up"}`))
			default:
				_, _ = w.Write([]byte(`{"result":"error","message":"unexpected"}`))
			}
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
		AllowedProductIDs:    []string{"5"},
		FetchDiscordID:       true,
		DiscordIDCustomField: "Discord ID",
		WHMCSAdminAPIID:      "id", WHMCSAdminAPISecret: "sec",
	}
	s := newAuthServer(cfg)

	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode should have succeeded despite details outage: %v", err)
	}
	if _, present := resp.GetClaims().AsMap()["discord_id"]; present {
		t.Errorf("discord_id should be absent when fetch failed")
	}
}

func TestExchangeCode_EmailHasNoMatchingClient_Rejects(t *testing.T) {
	// userinfo carries no client id (only sub + email), so the plugin falls
	// back to resolving the WHMCS client by email. GetClients returns an empty
	// clients envelope, which translates to a nil Client. The plugin must reject
	// this with PermissionDenied rather than auto-provisioning an unknown
	// identity.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"sub":"sub-42","id":"","email":"unknown@x.com","name":"User"}`))
		case "/includes/api.php":
			_ = r.ParseForm()
			if r.Form.Get("action") == "GetClients" {
				_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[]}}`))
				return
			}
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
		AllowedProductIDs: []string{"5"},
		WHMCSAdminAPIID:   "id", WHMCSAdminAPISecret: "sec",
	}
	s := newAuthServer(cfg)

	_, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("code = %v, want PermissionDenied; err = %v", status.Code(err), err)
	}
}

func TestExchangeCode_ProductsLookupError_FailsLogin(t *testing.T) {
	// GetClientsProducts returns a WHMCS error. This is the entitlement
	// source of truth — failing closed is correct: we must NOT let the user
	// through with an empty product list.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com"}`))
		case "/includes/api.php":
			_ = r.ParseForm()
			switch r.Form.Get("action") {
			case "GetClients":
				_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":"42","email":"u@x.com"}]}}`))
			case "GetClientsProducts":
				_, _ = w.Write([]byte(`{"result":"error","message":"db locked"}`))
			}
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
		AllowedProductIDs: []string{"5"},
		WHMCSAdminAPIID:   "id", WHMCSAdminAPISecret: "sec",
	}
	s := newAuthServer(cfg)

	_, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err == nil {
		t.Fatal("expected ExchangeCode to fail when products lookup errors")
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("code = %v, want Internal", status.Code(err))
	}
}

func TestExchangeCode_RoleMapping_AdminAppliedFromOwnedProduct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		case "/includes/api.php":
			_ = r.ParseForm()
			switch r.Form.Get("action") {
			case "GetClients":
				_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":"42","email":"u@x.com"}]}}`))
			case "GetClientsProducts":
				_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":12,"status":"Active"}]}}`))
			}
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
		ClaimRoleMapping: []pluginrt.ClaimRoleMap{
			{ProductID: "12", Role: "admin"},
		},
		WHMCSAdminAPIID: "id", WHMCSAdminAPISecret: "sec",
	}
	s := newAuthServer(cfg)

	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if got := resp.GetClaims().AsMap()["silo_role"]; got != "admin" {
		t.Errorf("silo_role = %v, want admin", got)
	}
}

func TestExchangeCode_RoleMapping_IgnoresMalformedRoleEntries(t *testing.T) {
	// runtime/validate normally rejects invalid roles at Configure time, but
	// belt-and-braces: if a malformed entry sneaks in (e.g. via direct DB
	// edit, or a future migration that misses a default), it must NOT crash
	// or elevate the user. The auth path silently skips it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c"}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com"}`))
		case "/includes/api.php":
			_ = r.ParseForm()
			switch r.Form.Get("action") {
			case "GetClients":
				_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":"42","email":"u@x.com"}]}}`))
			case "GetClientsProducts":
				_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":12,"status":"Active"}]}}`))
			}
		}
	}))
	defer srv.Close()

	cfg := pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
		ClaimRoleMapping: []pluginrt.ClaimRoleMap{
			{ProductID: "12", Role: "superadmin"}, // malformed
			{ProductID: "99", Role: "admin"},      // doesn't own this product
		},
		WHMCSAdminAPIID: "id", WHMCSAdminAPISecret: "sec",
	}
	s := newAuthServer(cfg)

	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code: "x", State: "s", RedirectUri: "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "s"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if got := resp.GetClaims().AsMap()["silo_role"]; got != "user" {
		t.Errorf("silo_role = %v, want user (malformed entry must not elevate)", got)
	}
}

// PKCE is this plugin's CSRF defense for the OAuth callback: the verifier
// generated in InitAuthorize is round-tripped via provider_state and bound to
// the upstream code_challenge. A missing or mismatched verifier MUST cause
// ExchangeCode to reject — these tests pin that contract.

func TestExchangeCode_CSRF_RejectsNilProviderState(t *testing.T) {
	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: "https://x", ClientID: "c", ClientSecret: "s",
	})
	_, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code:        "auth-code",
		State:       "state-1",
		RedirectUri: "/cb",
		// ProviderState intentionally nil — simulates a callback whose
		// state round-trip was lost or never set.
	})
	if err == nil {
		t.Fatal("expected rejection when provider_state is missing")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument; err = %v", status.Code(err), err)
	}
}

func TestExchangeCode_CSRF_RejectsMismatchedPKCEVerifier(t *testing.T) {
	// Upstream WHMCS hashes the verifier and compares to the challenge it
	// recorded at /oauth/authorize.php — a mismatched verifier yields 400.
	// The plugin must surface that as an error, NOT return a successful
	// AuthenticateResponse.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token.php" {
			t.Errorf("unexpected upstream call: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("code_verifier") != "wrong-verifier" {
			t.Errorf("verifier = %q, want wrong-verifier", r.Form.Get("code_verifier"))
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code_verifier mismatch"}`))
	}))
	defer srv.Close()

	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
	})
	_, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code:          "auth-code",
		State:         "state-1",
		RedirectUri:   "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "wrong-verifier", "state": "state-1"}),
	})
	if err == nil {
		t.Fatal("expected rejection when upstream rejects code_verifier")
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("code = %v, want Internal; err = %v", status.Code(err), err)
	}
}

func TestExchangeCode_CSRF_AcceptsValidPKCEVerifier(t *testing.T) {
	// Valid state path: provider_state carries the verifier minted in
	// InitAuthorize, the upstream accepts it, and ExchangeCode returns
	// an AuthenticateResponse with the userinfo populated.
	const verifier = "correct-verifier"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_ = r.ParseForm()
			if r.Form.Get("code_verifier") != verifier {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"AT","id_token":"a.b.c","token_type":"Bearer","expires_in":3600}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c", ClientSecret: "s",
	})
	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code:          "auth-code",
		State:         "state-1",
		RedirectUri:   "/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": verifier, "state": "state-1"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if resp.GetExternalSubject() != "42" {
		t.Errorf("external_subject = %q, want 42", resp.GetExternalSubject())
	}
}

func TestAuthenticate_ReturnsUnimplemented(t *testing.T) {
	s := newAuthServer(pluginrt.Config{})
	_, err := s.Authenticate(context.Background(), &pluginv1.AuthenticateRequest{Username: "u", Password: "p"})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("err = %v", err)
	}
}

func TestRefreshSession_UnconfiguredFailsPrecondition(t *testing.T) {
	s := newAuthServer(pluginrt.Config{})
	_, err := s.RefreshSession(context.Background(), &pluginv1.RefreshSessionRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition; err = %v", status.Code(err), err)
	}
}

func TestRefreshSession_NoRefreshTokenReturnsUnimplemented(t *testing.T) {
	// A configured plugin with no stored refresh_token must tell the host to
	// fall back to a full OAuth round-trip (Unimplemented), not error.
	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: "https://x", ClientID: "c", ClientSecret: "s",
	})
	_, err := s.RefreshSession(context.Background(), &pluginv1.RefreshSessionRequest{
		ExternalSubject: "42",
		RefreshState:    mustStruct(t, map[string]any{}),
	})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("code = %v, want Unimplemented; err = %v", status.Code(err), err)
	}
}

func TestRefreshSession_HappyPath_UsesRefreshTokenAndReturnsIdentity(t *testing.T) {
	var sawGrant, sawRefreshToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_ = r.ParseForm()
			sawGrant = r.Form.Get("grant_type")
			sawRefreshToken = r.Form.Get("refresh_token")
			// Rotate the refresh token to exercise the carry-forward path.
			_, _ = w.Write([]byte(`{"access_token":"AT2","refresh_token":"RT2","token_type":"Bearer","expires_in":3600}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c1", ClientSecret: "s",
	})
	resp, err := s.RefreshSession(context.Background(), &pluginv1.RefreshSessionRequest{
		ExternalSubject: "42",
		RefreshState:    mustStruct(t, map[string]any{"whmcs_refresh_token": "RT1"}),
	})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if sawGrant != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", sawGrant)
	}
	if sawRefreshToken != "RT1" {
		t.Errorf("refresh_token sent = %q, want RT1", sawRefreshToken)
	}
	if resp.GetExternalSubject() != "42" || resp.GetEmail() != "u@x.com" {
		t.Errorf("resp = %+v", resp)
	}
	// The rotated refresh_token should be carried back in the claims.
	if got, _ := resp.GetClaims().AsMap()["whmcs_refresh_token"].(string); got != "RT2" {
		t.Errorf("carried refresh_token = %q, want RT2", got)
	}
}

func TestRefreshSession_UpstreamErrorIsUnauthenticated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token.php" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c1", ClientSecret: "s",
	})
	_, err := s.RefreshSession(context.Background(), &pluginv1.RefreshSessionRequest{
		ExternalSubject: "42",
		RefreshState:    mustStruct(t, map[string]any{"whmcs_refresh_token": "RT1"}),
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated; err = %v", status.Code(err), err)
	}
}

func TestExchangeCode_CarriesRefreshTokenClaim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token.php":
			_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT1","token_type":"Bearer","expires_in":3600}`))
		case "/oauth/userinfo.php":
			_, _ = w.Write([]byte(`{"id":"42","email":"u@x.com","name":"User"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	s := newAuthServer(pluginrt.Config{
		WHMCSServerURL: srv.URL, ClientID: "c1", ClientSecret: "s",
	})
	resp, err := s.ExchangeCode(context.Background(), &pluginv1.ExchangeCodeRequest{
		Code:          "auth-code",
		State:         "state-1",
		RedirectUri:   "https://app.example/cb",
		ProviderState: mustStruct(t, map[string]any{"pkce_verifier": "v", "state": "state-1"}),
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if got, _ := resp.GetClaims().AsMap()["whmcs_refresh_token"].(string); got != "RT1" {
		t.Errorf("refresh_token claim = %q, want RT1", got)
	}
}
