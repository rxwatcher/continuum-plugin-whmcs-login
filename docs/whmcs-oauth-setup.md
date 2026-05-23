# WHMCS OAuth/OIDC client setup

This is the WHMCS-side configuration the plugin needs to authenticate users.
Do this once per Silo install that connects to a given WHMCS instance.

## Prerequisites

- A WHMCS deployment that has the OAuth Identity Provider module installed
  and enabled. WHMCS ships OAuth Server (an OIDC-compliant authorization
  server) in the core; on older 7.x deployments it may need to be enabled
  under Setup -> Apps & Integrations -> OpenID Connect.
- Admin login on that WHMCS instance.
- The Silo host's externally reachable HTTPS URL.
- The install ID of this plugin install in Silo. Find it in the
  Silo admin UI on the plugin's install detail page; the URL contains
  it, and the install page exposes the per-install callback path.

## 1. Compose the redirect URI

The redirect URI is fixed by Silo and is per-install:

```
https://<silo-host>/api/v1/auth/oauth/<install-id>/callback
```

Notes:

- Scheme must be `https` in production. The plugin allows `http` only when
  the WHMCS server URL itself is on `localhost` / a loopback address; the
  redirect URI restrictions are enforced by WHMCS, not by this plugin.
- The path is literal — no trailing slash, no query string, no fragment.
- If you reinstall the plugin in Silo, the install ID changes. You must
  update the redirect URI in WHMCS or logins will fail with a redirect_uri
  mismatch.

## 2. Create the OAuth client in WHMCS

In WHMCS admin:

1. Setup -> Apps & Integrations -> OpenID Connect (or "Identity Provider", or
   under System Settings on newer builds).
2. Create a new Identity Client / OAuth client. Settings:
   - **Name**: anything operator-friendly, e.g. `Silo`.
   - **Identifier / Client ID**: WHMCS will generate this; keep it.
   - **Secret**: WHMCS will generate this; copy it immediately, it will only
     be shown once on most builds.
   - **Redirect URI(s)**: paste the URI from step 1, exactly.
   - **Allowed grant types**: `authorization_code` is required. Include
     `refresh_token` only if you intend to add refresh support later — the
     plugin's `RefreshSession` currently returns Unimplemented (see
     `internal/auth/server.go`), so refresh tokens are unused.
   - **Scopes**: at minimum `openid profile email`. The plugin requests
     `openid profile email` by default (see `BuildAuthorizeURL` in
     `internal/whmcs/oauth.go`). If your WHMCS install requires explicit
     scope allowlisting, enable those three.
   - **PKCE**: enable / require. The plugin always uses S256 PKCE; no
     `client_secret`-only flow is supported.

## 3. Configure the plugin

In Silo admin -> Plugins -> WHMCS Login -> Install -> admin SPA at
`/admin/`:

| Field | Value |
| --- | --- |
| WHMCS server URL | The WHMCS base URL, no trailing slash, e.g. `https://billing.example.com`. The plugin trims trailing slashes; it appends `/oauth/authorize.php`, `/oauth/token.php`, `/oauth/userinfo.php`, and `/includes/api.php` itself. |
| Client ID | From WHMCS. |
| Client secret | From WHMCS. Write-only via the admin form — the SPA never displays it. |
| Display name | The label shown on the Silo login button. Optional. |

The connection string (`database_url`) is set by the Silo host, not via
the SPA. Point it at a Postgres role with create-table rights inside the
`whmcs_login` schema only.

## 4. Smoke test

1. Sign out of Silo (or use an incognito window).
2. Click the WHMCS sign-in button.
3. You should be sent to `https://<whmcs>/oauth/authorize.php?...`. Confirm
   the consent screen renders.
4. After consenting, you should land back on the Silo host and be
   signed in.

If step 3 errors out with `invalid_redirect_uri`, the URI in WHMCS does not
match exactly. Re-check scheme, host, and install ID.

If step 4 errors out, see [debugging.md](debugging.md).

## Endpoint paths the plugin uses

The plugin assumes a vanilla WHMCS path layout:

| Purpose | URL |
| --- | --- |
| Authorize | `<server_url>/oauth/authorize.php` |
| Token | `<server_url>/oauth/token.php` |
| Userinfo | `<server_url>/oauth/userinfo.php` |
| Admin API | `<server_url>/includes/api.php` |

These are hardcoded in `internal/whmcs/oauth.go` and `internal/whmcs/api.go`.
If your WHMCS install rewrites these paths (custom mod_rewrite, reverse
proxy, etc.), the plugin will not work without code changes — fix the upstream
routing instead.

## Response shape requirements

- Token response: JSON. Must include `access_token`. `token_type` must be
  empty or `Bearer`; anything else is rejected.
- Userinfo response: JSON. Must include `sub` (the plugin will fall back to
  `id` if `sub` is missing, but at least one is required). The plugin reads
  `email`, `name`, `picture`, `given_name`, `family_name` opportunistically
  and surfaces them as `raw_userinfo` claims.
</content>
</invoke>