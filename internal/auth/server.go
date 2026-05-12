// Package auth implements the auth_provider.v1 gRPC service: InitAuthorize
// (kicks off PKCE OAuth against WHMCS) and ExchangeCode (token exchange +
// userinfo + optional product gating / Discord ID fetch). Authenticate and
// RefreshSession return Unimplemented per spec Layer 4.6/4.7.
package auth

import (
	"context"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"

	pluginrt "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
)

// ConfigFn returns the live in-process Config. It is invoked on every RPC so
// that Configure-driven reconfiguration takes effect without restarting the
// gRPC server.
type ConfigFn func() pluginrt.Config

// Server implements pluginv1.AuthProviderServer.
type Server struct {
	pluginv1.UnimplementedAuthProviderServer
	cfgFn ConfigFn
}

func NewServer(cfgFn ConfigFn) *Server {
	return &Server{cfgFn: cfgFn}
}

// Authenticate (password) is not supported — manifest declares oauth2 only.
func (s *Server) Authenticate(_ context.Context, _ *pluginv1.AuthenticateRequest) (*pluginv1.AuthenticateResponse, error) {
	return nil, status.Error(codes.Unimplemented,
		"WHMCS plugin is OAuth-only; use InitAuthorize / ExchangeCode")
}

// RefreshSession is not supported in v1 — users re-authenticate via the full
// OAuth flow when their continuum session expires.
func (s *Server) RefreshSession(_ context.Context, _ *pluginv1.RefreshSessionRequest) (*pluginv1.AuthenticateResponse, error) {
	return nil, status.Error(codes.Unimplemented, "refresh not supported in v1")
}

// InitAuthorize generates a PKCE verifier+challenge, builds the upstream
// authorize URL, and stashes the verifier in provider_state for the host to
// round-trip on the callback.
func (s *Server) InitAuthorize(_ context.Context, req *pluginv1.InitAuthorizeRequest) (*pluginv1.InitAuthorizeResponse, error) {
	cfg := s.cfgFn()
	if cfg.WHMCSServerURL == "" || cfg.ClientID == "" {
		return nil, status.Error(codes.FailedPrecondition, "plugin not configured")
	}
	verifier, challenge := whmcs.GeneratePKCE()
	authorizeURL := whmcs.BuildAuthorizeURL(whmcs.AuthorizeParams{
		ServerURL:           cfg.WHMCSServerURL,
		ClientID:            cfg.ClientID,
		RedirectURI:         req.GetRedirectUri(),
		State:               req.GetState(),
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	pState, err := structpb.NewStruct(map[string]any{"pkce_verifier": verifier})
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
	if verifier == "" {
		return nil, status.Error(codes.InvalidArgument, "missing pkce_verifier in provider_state")
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

	claims := map[string]any{
		"raw_userinfo": map[string]any{
			"id":          ui.ID,
			"email":       ui.Email,
			"name":        ui.Name,
			"picture":     ui.Picture,
			"given_name":  ui.GivenName,
			"family_name": ui.FamilyName,
		},
	}

	if len(cfg.AllowedProductIDs) > 0 || cfg.FetchDiscordID {
		if cfg.WHMCSAdminAPIID == "" || cfg.WHMCSAdminAPISecret == "" {
			return nil, status.Error(codes.FailedPrecondition,
				"admin API credentials required for product gating / Discord ID fetch")
		}
		api := whmcs.NewAPIClient(cfg.WHMCSServerURL, cfg.WHMCSAdminAPIID, cfg.WHMCSAdminAPISecret)

		if len(cfg.AllowedProductIDs) > 0 {
			prods, err := api.GetClientsProducts(ctx, ui.ID)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "fetch client products: %v", err)
			}
			ownedActive := activeProductIDs(prods)
			if !anyMatch(cfg.AllowedProductIDs, ownedActive) {
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
			cd, err := api.GetClientsDetails(ctx, ui.ID)
			if err == nil {
				if id, ok := cd.CustomFields[cfg.DiscordIDCustomField]; ok && id != "" {
					claims["discord_id"] = id
				}
			}
			// Discord ID failure is non-fatal — login succeeds, claim is absent.
		}
	}

	cs, err := structpb.NewStruct(claims)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode claims: %v", err)
	}
	return &pluginv1.AuthenticateResponse{
		ExternalSubject: ui.ID,
		DisplayName:     ui.Name,
		Email:           ui.Email,
		Claims:          cs,
	}, nil
}

// activeProductIDs returns the PIDs (as decimal strings) of products the
// client owns with status "Active" (or unset, which some WHMCS versions
// return for client-products).
func activeProductIDs(prods []whmcs.ClientProduct) []string {
	out := make([]string, 0, len(prods))
	for _, p := range prods {
		if p.Status == "" || p.Status == "Active" {
			out = append(out, strconv.Itoa(p.PID))
		}
	}
	return out
}

func anyMatch(want, have []string) bool {
	for _, w := range want {
		for _, h := range have {
			if w == h {
				return true
			}
		}
	}
	return false
}
