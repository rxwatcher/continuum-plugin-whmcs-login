# continuum-plugin-whmcs-login

Continuum plugin: OAuth/OIDC authentication via WHMCS. Implements the
`auth_provider.v1` capability (PKCE OAuth) plus an admin SPA at `/admin`
for configuring allowed products, admin API credentials, and role mapping.

- Spec: `/opt/worktrees/continuum-rh/docs/superpowers/specs/2026-05-12-whmcs-login-design.md`
- Plan: `/opt/worktrees/continuum-rh/docs/superpowers/plans/2026-05-12-whmcs-login.md`

## Build

```
make build
```

This installs SPA dependencies, builds `web/dist`, then compiles the Go
binary `continuum-plugin-whmcs-login`.

## Test

```
make test        # Go unit tests
make test-web    # Vitest (SPA)
```

## Capabilities

- `auth_provider.v1` (id=`whmcs`, modes=`oauth2`) — PKCE OAuth handshake
  against `<whmcs>/oauth/authorize.php` / `/oauth/token.php` /
  `/oauth/userinfo.php`, with optional product gating + Discord ID
  fetch via the WHMCS admin API (`/includes/api.php`).
- `http_routes.v1` (id=`spa`) — admin SPA at `/admin`, public `/assets/*`
  (logo + bundled SPA assets), admin-gated `/api/v1/admin/*`.

## Config

See `cmd/continuum-plugin-whmcs-login/manifest.json` for the global config
schema. Required: `whmcs_server_url`, `client_id`, `client_secret`.
Required if product gating or Discord ID fetch is enabled:
`whmcs_admin_api_id`, `whmcs_admin_api_secret`.
