# WHMCS Login for Silo

`silo.whmcs-login` is a Silo auth provider that signs users in through a WHMCS billing system's OAuth/OIDC endpoints and gates access by active WHMCS product ownership.

## Category

Lives under **Auth**.

Use this plugin when WHMCS is your source of customer identity and entitlement. For a generic IdP, use [`silo.oidc-login`](https://github.com/RXWatcher/silo-plugin-oidc-login).

## Capabilities

| Type | ID | Purpose |
| --- | --- | --- |
| `auth_provider.v1` | `whmcs` | OAuth2/PKCE sign-in against a WHMCS instance, with optional product-based entitlement and role mapping. |
| `http_routes.v1` | `spa` | Admin SPA for configuring OAuth credentials, allowed products, role mapping, and diagnostics. |

HTTP routes registered by the plugin:

- `GET /assets/*` (public) — static assets (logo, etc.)
- `GET /admin/*` (admin) — admin SPA, navigable as "WHMCS Login"
- `* /api/v1/admin/*` (admin) — admin API consumed by the SPA
- `GET /api/v1/health` (public) — liveness probe

## Dependencies

Standalone Silo auth provider. Requires:

- A reachable WHMCS instance with an OAuth/OIDC client configured.
- A Postgres database reachable from the plugin runtime, with a dedicated `whmcs_login` schema for the plugin's owned tables (config storage and migrations).
- Optional: WHMCS admin API credentials, needed only when you want product gating or Discord ID enrichment.

Host: [`Silo-Server/silo-server`](https://github.com/Silo-Server/silo-server). SDK: [`Silo-Server/silo-plugin-sdk`](https://github.com/Silo-Server/silo-plugin-sdk).

## External services

- **WHMCS OAuth/OIDC** — authorization, token exchange, and userinfo.
- **WHMCS admin API** — optional, used to verify active product ownership and to look up the configured Discord ID custom field.

## Auth flow

1. The user picks "Sign in with WHMCS" on the Silo login page.
2. The plugin starts an OAuth2/PKCE authorization request against `whmcs_server_url`.
3. WHMCS redirects back to `https://<silo-host>/api/v1/auth/oauth/<install-id>/callback`.
4. The plugin exchanges the code for tokens and fetches the WHMCS user.
5. If `allowed_product_ids` is set, the plugin calls the WHMCS admin API to confirm the client has at least one matching active product; otherwise access is denied.
6. If `fetch_discord_id` is on, the plugin reads the configured WHMCS custom field and exposes it as a claim.
7. `claim_role_mapping` maps owned product IDs to Silo roles (`user` or `admin`) for this login.
8. The plugin returns identity, claims, and role to the Silo host, which establishes the session.

The redirect URI to register in WHMCS is per-install:

```text
https://<silo-host>/api/v1/auth/oauth/<install-id>/callback
```

## Allowed products

Product gating is configured per install through the admin SPA at `/admin/`. Operators pick WHMCS product IDs from a list fetched (and cached for five minutes) from the WHMCS admin API; the SPA includes a refresh action that invalidates the cache on demand.

Behavior:

- Empty list — any WHMCS account can sign in.
- Non-empty list — only clients with at least one active subscription matching one of those product IDs can sign in. All other accounts are rejected at the callback.

Role mapping uses the same set of product IDs: each entry in `claim_role_mapping` pins a product ID to either the `user` or `admin` role, evaluated at every login.

## Configuration

Most of the plugin global config is persisted by the plugin itself (in its own schema) and edited from the admin SPA. Secrets are host-managed: `database_url`, `client_secret`, and `whmcs_admin_api_secret` are declared in the manifest `global_config_schema` with `secret: true`, stored encrypted by the host, and injected at runtime via `Configure`. They are never written to the plugin's own database and are configured from the host's plugin settings, not the plugin SPA.

| Key | Required | Managed by | Description |
| --- | --- | --- | --- |
| `database_url` | yes | host (secret) | Postgres DSN for the dedicated `whmcs_login` schema. |
| `whmcs_server_url` | yes | plugin | WHMCS base URL, no trailing slash. HTTPS required except for localhost; must not resolve to an internal/private address. |
| `client_id` | yes | plugin | WHMCS OAuth client ID. |
| `client_secret` | yes | host (secret) | WHMCS OAuth client secret. |
| `display_name` | no | plugin | Label shown on the Silo login button. |
| `allowed_product_ids` | no | plugin | Comma-separated WHMCS product IDs. Empty allows any WHMCS account. |
| `whmcs_admin_api_id` | conditional | plugin | Required for product gating and Discord ID lookup. |
| `whmcs_admin_api_secret` | conditional | host (secret) | Required for product gating and Discord ID lookup. |
| `fetch_discord_id` | no | plugin | If true, includes the configured custom field as a claim. |
| `discord_id_custom_field` | no | plugin | WHMCS custom-field name. Defaults to `Discord ID`. |
| `link_by_email` | no | plugin | Off by default. When true, sets the `silo_link_by_email` claim so the host may link a sign-in to an existing account sharing the (unverified) email. Account-takeover risk — opt in only if you trust WHMCS email verification. |
| `claim_role_mapping` | no | plugin | JSON array of `{product_id, role}` entries; `role` must be `user` or `admin`. |

Example `claim_role_mapping`:

```json
[
  {"product_id": "5", "role": "user"},
  {"product_id": "12", "role": "admin"}
]
```

Role assignment is applied at login; product changes during an active session take effect on next sign-in.

## Detailed docs

Operator deep-dives live under [`docs/`](docs/):

- [WHMCS OAuth/OIDC client setup](docs/whmcs-oauth-setup.md)
- [WHMCS admin API setup](docs/admin-api-setup.md)
- [Product gating and role mapping](docs/product-gating-and-roles.md)
- [Discord ID enrichment](docs/discord-id-enrichment.md)
- [Debugging runbook](docs/debugging.md)
- [WHMCS quirks and edge cases](docs/whmcs-quirks.md)

## Build and release

```bash
make build      # builds web SPA and the Go binary
make test       # go test ./... and the web tests
```

CI builds linux-amd64 binaries on push to main via the reusable workflow in [RXWatcher/silo-plugin-repository](https://github.com/RXWatcher/silo-plugin-repository) and publishes them to the catalog at [`./binaries/`](https://github.com/RXWatcher/silo-plugin-repository/tree/main/binaries).
