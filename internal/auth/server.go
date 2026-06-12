// Package auth implements the auth_provider.v1 gRPC service: InitAuthorize
// (kicks off PKCE OAuth against WHMCS), ExchangeCode (token exchange + userinfo
// + optional product gating / Discord ID fetch), and RefreshSession (silent
// re-auth via a stored WHMCS refresh_token). Authenticate (password) returns
// Unimplemented — the manifest declares oauth2 only.
package auth

import (
	"context"
	"crypto/subtle"
	"strconv"
	"strings"

	"github.com/hashicorp/go-hclog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"

	pluginrt "github.com/RXWatcher/silo-plugin-whmcs-login/internal/runtime"
	"github.com/RXWatcher/silo-plugin-whmcs-login/internal/whmcs"
)

// ConfigFn returns the live in-process Config. It is invoked on every RPC so
// that Configure-driven reconfiguration takes effect without restarting the
// gRPC server.
type ConfigFn func() pluginrt.Config

// EntitlementAPI is the subset of the WHMCS admin API the auth flow needs for
// product gating + Discord ID resolution. The shared, coalescing +
// negative-caching whmcs.EntitlementResolver satisfies the lookup methods;
// GetClientsDetails passes through to the underlying client.
type EntitlementAPI interface {
	GetClientByEmail(ctx context.Context, email string) (*whmcs.Client, error)
	GetClientsProducts(ctx context.Context, clientID string) ([]whmcs.ClientProduct, error)
	GetClientsDetails(ctx context.Context, clientID string) (whmcs.ClientDetails, error)
}

// APIFn returns the shared entitlement API client for the live config. Returning
// nil signals that admin API credentials are not configured. Supplied by
// main.go so the auth flow reuses one coalescing resolver across logins; when
// omitted (tests), the server falls back to a fresh per-call whmcs.APIClient.
type APIFn func(cfg pluginrt.Config) EntitlementAPI

// Server implements pluginv1.AuthProviderServer.
type Server struct {
	pluginv1.UnimplementedAuthProviderServer
	cfgFn ConfigFn
	apiFn APIFn
	log   hclog.Logger
}

// NewServer builds the auth provider server. An optional logger may be passed;
// when omitted, a no-op logger is used so tests can construct the server
// without wiring logging.
func NewServer(cfgFn ConfigFn, log ...hclog.Logger) *Server {
	l := hclog.NewNullLogger()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Server{cfgFn: cfgFn, log: l}
}

// SetAPIFn installs the shared entitlement-API accessor. main.go calls this so
// the auth flow reuses the process-wide coalescing/negative-caching resolver.
func (s *Server) SetAPIFn(fn APIFn) { s.apiFn = fn }

// entitlementAPI returns the entitlement API client for cfg, preferring the
// shared resolver from apiFn and falling back to a freshly-built APIClient.
func (s *Server) entitlementAPI(cfg pluginrt.Config) EntitlementAPI {
	if s.apiFn != nil {
		if api := s.apiFn(cfg); api != nil {
			return api
		}
	}
	if cfg.WHMCSAdminAPIID == "" || cfg.WHMCSAdminAPISecret == "" {
		return nil
	}
	return whmcs.NewAPIClient(cfg.WHMCSServerURL, cfg.WHMCSAdminAPIID, cfg.WHMCSAdminAPISecret)
}

// Authenticate (password) is not supported — manifest declares oauth2 only.
func (s *Server) Authenticate(_ context.Context, _ *pluginv1.AuthenticateRequest) (*pluginv1.AuthenticateResponse, error) {
	return nil, status.Error(codes.Unimplemented,
		"WHMCS plugin is OAuth-only; use InitAuthorize / ExchangeCode")
}

// refreshTokenClaim is the claim key under which ExchangeCode stashes the WHMCS
// refresh_token. The host persists AuthenticateResponse claims and round-trips
// them into RefreshSessionRequest.refresh_state, giving us a place to recover
// the refresh_token without a fresh OAuth round-trip.
const refreshTokenClaim = "whmcs_refresh_token"

// RefreshSession refreshes a previously-issued session using the WHMCS
// refresh_token the host round-tripped via refresh_state. It performs a
// grant_type=refresh_token swap, re-fetches userinfo, re-runs the same product
// gating / role mapping / Discord resolution as ExchangeCode, and returns the
// refreshed identity. When no refresh_token is present (older sessions, or a
// WHMCS that didn't issue one) it returns Unimplemented so the host falls back
// to a full re-authentication.
func (s *Server) RefreshSession(ctx context.Context, req *pluginv1.RefreshSessionRequest) (*pluginv1.AuthenticateResponse, error) {
	cfg := s.cfgFn()
	if cfg.WHMCSServerURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, status.Error(codes.FailedPrecondition, "plugin not configured")
	}

	refreshToken := ""
	if st := req.GetRefreshState(); st != nil {
		refreshToken, _ = st.AsMap()[refreshTokenClaim].(string)
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		// No stored refresh_token: tell the host to re-run the full OAuth flow.
		return nil, status.Error(codes.Unimplemented,
			"no WHMCS refresh_token available; full re-authentication required")
	}

	tok, err := whmcs.RefreshAccessToken(ctx, whmcs.RefreshParams{
		ServerURL:    cfg.WHMCSServerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RefreshToken: refreshToken,
	})
	if err != nil {
		s.audit("deny", "", req.GetExternalSubject(), "refresh-failed", err.Error())
		return nil, status.Errorf(codes.Unauthenticated, "refresh token: %v", err)
	}
	// WHMCS may rotate the refresh_token; carry the freshest one forward so the
	// next refresh uses it. Fall back to the prior token when none is returned.
	nextRefresh := tok.RefreshToken
	if nextRefresh == "" {
		nextRefresh = refreshToken
	}

	ui, err := whmcs.FetchUserInfo(ctx, cfg.WHMCSServerURL, tok.AccessToken)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "userinfo: %v", err)
	}
	return s.buildResponse(ctx, cfg, ui, nextRefresh)
}

// InitAuthorize generates a PKCE verifier+challenge, builds the upstream
// authorize URL, and stashes the verifier in provider_state for the host to
// round-trip on the callback.
func (s *Server) InitAuthorize(_ context.Context, req *pluginv1.InitAuthorizeRequest) (*pluginv1.InitAuthorizeResponse, error) {
	cfg := s.cfgFn()
	if cfg.WHMCSServerURL == "" || cfg.ClientID == "" {
		return nil, status.Error(codes.FailedPrecondition, "plugin not configured")
	}
	verifier, challenge, err := whmcs.GeneratePKCE()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "entropy: %v", err)
	}
	authorizeURL := whmcs.BuildAuthorizeURL(whmcs.AuthorizeParams{
		ServerURL:           cfg.WHMCSServerURL,
		ClientID:            cfg.ClientID,
		RedirectURI:         req.GetRedirectUri(),
		State:               req.GetState(),
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	pState, err := structpb.NewStruct(map[string]any{
		"pkce_verifier": verifier,
		"state":         req.GetState(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode provider_state: %v", err)
	}
	return &pluginv1.InitAuthorizeResponse{
		AuthorizeUrl:  authorizeURL,
		ProviderState: pState,
	}, nil
}

// ExchangeCode exchanges the authorization code for tokens, calls
// /oauth/userinfo.php, optionally fetches product list + Discord ID, and
// returns an AuthenticateResponse the host pipes through its provisioning
// logic.
func (s *Server) ExchangeCode(ctx context.Context, req *pluginv1.ExchangeCodeRequest) (*pluginv1.AuthenticateResponse, error) {
	cfg := s.cfgFn()
	if cfg.WHMCSServerURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, status.Error(codes.FailedPrecondition, "plugin not configured")
	}

	pState := req.GetProviderState().AsMap()
	verifier, _ := pState["pkce_verifier"].(string)
	expectedState, _ := pState["state"].(string)
	if verifier == "" {
		return nil, status.Error(codes.InvalidArgument, "missing pkce_verifier in provider_state")
	}
	// Fail closed: the state round-tripped via provider_state is the CSRF bind
	// for the OAuth callback. A missing expected state (provider_state lost the
	// value) or a mismatch must reject — never skip the comparison.
	if expectedState == "" {
		return nil, status.Error(codes.Unauthenticated, "missing state in provider_state")
	}
	if subtle.ConstantTimeCompare([]byte(req.GetState()), []byte(expectedState)) != 1 {
		return nil, status.Error(codes.Unauthenticated, "state mismatch")
	}

	tok, err := whmcs.ExchangeCode(ctx, whmcs.ExchangeParams{
		ServerURL:    cfg.WHMCSServerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Code:         req.GetCode(),
		RedirectURI:  req.GetRedirectUri(),
		CodeVerifier: verifier,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "token exchange: %v", err)
	}

	ui, err := whmcs.FetchUserInfo(ctx, cfg.WHMCSServerURL, tok.AccessToken)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "userinfo: %v", err)
	}

	return s.buildResponse(ctx, cfg, ui, tok.RefreshToken)
}

// buildResponse runs the entitlement evaluation (client resolution, product
// gating, role mapping, Discord ID) shared by ExchangeCode and RefreshSession,
// audits the allow/deny decision, and assembles the AuthenticateResponse. When
// refreshToken is non-empty it is carried back in the claims so the host can
// round-trip it into a future RefreshSession.
func (s *Server) buildResponse(ctx context.Context, cfg pluginrt.Config, ui whmcs.UserInfo, refreshToken string) (*pluginv1.AuthenticateResponse, error) {
	claims := map[string]any{
		"raw_userinfo": map[string]any{
			"sub":         ui.Sub,
			"id":          ui.ID,
			"email":       ui.Email,
			"name":        ui.Name,
			"picture":     ui.Picture,
			"given_name":  ui.GivenName,
			"family_name": ui.FamilyName,
		},
	}
	// Carry the WHMCS refresh_token (if any) so the host can persist it and
	// round-trip it into RefreshSession, enabling silent session refresh.
	if rt := strings.TrimSpace(refreshToken); rt != "" {
		claims[refreshTokenClaim] = rt
	}
	// Account linking by (unverified) email is an account-takeover vector, so it
	// is gated behind an explicit admin opt-in (default off).
	if cfg.LinkByEmail {
		claims["silo_link_by_email"] = true
	}

	if len(cfg.AllowedProductIDs) > 0 || len(cfg.ClaimRoleMapping) > 0 || cfg.FetchDiscordID {
		api := s.entitlementAPI(cfg)
		if api == nil {
			return nil, status.Error(codes.FailedPrecondition,
				"admin API credentials required for product gating / Discord ID fetch")
		}
		// Prefer the authenticated WHMCS client id from userinfo. Only fall back
		// to an email lookup when userinfo did not carry an id — resolving the
		// client by the unverified email is weaker and is the last resort.
		clientID := ui.ID
		if clientID == "" {
			if ui.Email == "" {
				s.audit("deny", ui.Email, ui.Sub, "no-client", "userinfo carried neither client id nor email")
				return nil, status.Error(codes.PermissionDenied,
					"WHMCS userinfo carried neither client id nor email")
			}
			client, err := api.GetClientByEmail(ctx, ui.Email)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "fetch client by email: %v", err)
			}
			if client == nil || client.ID == "" {
				s.audit("deny", ui.Email, ui.Sub, "no-client", "no WHMCS client found for email")
				return nil, status.Error(codes.PermissionDenied, "no WHMCS client found for this email")
			}
			clientID = client.ID
		}

		var ownedActive []string
		if len(cfg.AllowedProductIDs) > 0 || len(cfg.ClaimRoleMapping) > 0 {
			prods, err := api.GetClientsProducts(ctx, clientID)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "fetch client products: %v", err)
			}
			ownedActive = ActiveProductIDs(prods)
			if len(cfg.AllowedProductIDs) > 0 && !AnyMatch(cfg.AllowedProductIDs, ownedActive) {
				s.audit("deny", ui.Email, ui.Sub, "product-gate-failed",
					"no allowed active product (client "+clientID+")")
				return nil, status.Error(codes.PermissionDenied,
					"your WHMCS account doesn't have an allowed active product")
			}
			// Carry the user's product list through to the host so role
			// mapping can match against it.
			anyVals := make([]any, len(ownedActive))
			for i, v := range ownedActive {
				anyVals[i] = v
			}
			claims["products"] = anyVals
		}

		if cfg.FetchDiscordID {
			cd, err := api.GetClientsDetails(ctx, clientID)
			if err == nil {
				if id, ok := cd.CustomFields[cfg.DiscordIDCustomField]; ok && id != "" {
					claims["discord_id"] = id
				}
			} else {
				// Discord ID failure is non-fatal — login succeeds, claim is
				// absent — but log it so a misconfigured custom field or upstream
				// outage is diagnosable instead of silently swallowed.
				s.log.Debug("discord id fetch failed", "client_id", clientID, "error", err)
			}
		}

		if len(ownedActive) > 0 {
			claims["silo_role"] = RoleFromProducts(ownedActive, cfg.ClaimRoleMapping)
		}
	}

	cs, err := structpb.NewStruct(claims)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode claims: %v", err)
	}
	s.audit("allow", ui.Email, ui.Sub, "success", "")
	return &pluginv1.AuthenticateResponse{
		ExternalSubject: ui.Sub,
		DisplayName:     ui.Name,
		Email:           ui.Email,
		Claims:          cs,
	}, nil
}

// audit emits a structured auth decision log line. decision is "allow" or
// "deny"; reason is a short machine-friendly tag (success / no-client /
// product-gate-failed / refresh-failed). The email + subject identify the
// actor. No tokens or secrets are ever logged here.
func (s *Server) audit(decision, email, subject, reason, detail string) {
	args := []any{
		"decision", decision,
		"reason", reason,
		"subject", subject,
		"email", email,
	}
	if detail != "" {
		args = append(args, "detail", detail)
	}
	s.log.Info("auth decision", args...)
}

// ActiveProductIDs returns the PIDs (as decimal strings) of products the
// client owns with an Active-equivalent status.
//
// Treated as active:
//   - Status == nil (WHMCS omitted the field — legacy compat).
//   - Status != nil && strings.TrimSpace(*Status) case-insensitively equals
//     "Active".
//
// Treated as inactive (and excluded from gating):
//   - Status != nil && *Status == ""  — WHMCS explicitly returned empty,
//     which is conservatively interpreted as not-active. If a future
//     deployment hits a WHMCS variant that returns "" for genuinely active
//     products, this allowlist needs to be extended.
//   - Status != nil && *Status == "Suspended" | "Terminated" | "Cancelled" |
//     "Fraud" | "Pending" (or anything else).
func ActiveProductIDs(prods []whmcs.ClientProduct) []string {
	out := make([]string, 0, len(prods))
	for _, p := range prods {
		if p.Status == nil || strings.EqualFold(strings.TrimSpace(*p.Status), "Active") {
			out = append(out, strconv.Itoa(p.PID))
		}
	}
	return out
}

func AnyMatch(want, have []string) bool {
	for _, w := range want {
		for _, h := range have {
			if w == h {
				return true
			}
		}
	}
	return false
}

// RoleFromProducts returns "admin" if any owned product maps to an admin role,
// otherwise "user". Non-admin mappings ("user") and malformed roles never
// elevate, so they need no separate handling.
func RoleFromProducts(products []string, mappings []pluginrt.ClaimRoleMap) string {
	for _, m := range mappings {
		if m.Role != "admin" {
			continue
		}
		for _, p := range products {
			if p == m.ProductID {
				return "admin"
			}
		}
	}
	return "user"
}
