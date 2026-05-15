# continuum-plugin-whmcs-login

OAuth/OIDC authentication for Continuum via [WHMCS](https://www.whmcs.com/). PKCE OAuth handshake against the WHMCS server's OAuth endpoints, with **optional product gating** (only users who own a specified WHMCS product can sign in) and **role mapping** from WHMCS product IDs to Continuum roles.

Companion plugin to the generic [`continuum.oidc-login`](../continuum-plugin-oidc-login/). Pick this one when your user accounts already live in WHMCS billing.

## Capabilities

| Capability | Notes |
|---|---|
| `auth_provider.v1` (`whmcs`, modes `oauth2`) | PKCE OAuth handshake against `<whmcs>/oauth/{authorize,token,userinfo}.php`, with optional product gating + Discord ID fetch via the WHMCS admin API (`/includes/api.php`). |
| `http_routes.v1` (`spa`) | Admin SPA at `/admin`, `/assets/*` (logo + bundled SPA), `/api/v1/admin/*` (admin-gated). |

## Configuration

| Key | Required | Description |
|---|---|---|
| `whmcs_server_url` | yes | WHMCS instance base URL. |
| `client_id` | yes | OAuth client ID from WHMCS. |
| `client_secret` | yes | OAuth client secret. |
| `allowed_product_ids` | no | Comma-separated WHMCS product IDs. Users without an active matching product are rejected. |
| `whmcs_admin_api_id` | conditional | Required for product gating or Discord ID fetch. |
| `whmcs_admin_api_secret` | conditional | Same. |
| `fetch_discord_id` | no | Pull a Discord ID custom field from WHMCS and stamp it onto the user's identity. |
| `discord_id_custom_field` | no | WHMCS custom-field name holding the Discord ID. |
| `claim_role_mapping` | no | JSON array of `{product_id, role}` objects. |

See [`cmd/continuum-plugin-whmcs-login/manifest.json`](cmd/continuum-plugin-whmcs-login/manifest.json) for the full schema.

## Claim role mapping

The `claim_role_mapping` config is a JSON array binding WHMCS product IDs to Continuum roles. After every successful login the host inspects the user's active products (carried through in the `products` claim) and applies the highest-matched role from this mapping.

Shape — an array of `{product_id, role}` objects, where `role` is `user` or `admin`:

```json
[
  {"product_id": "5",  "role": "user"},
  {"product_id": "12", "role": "admin"}
]
```

Examples:

- A client who owns active product `5` is granted the `user` role.
- A client who owns both `5` and `12` is granted `admin` (host picks the highest match).
- A client who owns neither receives no mapped role; access still depends on `allowed_product_ids` if product gating is enabled.

Note: role assignment is applied at login. If a user's WHMCS products change mid-session, the new role takes effect only after they re-authenticate.

## Dependencies

- No Postgres schema (stateless).
- Reachable WHMCS instance.
- Optionally: WHMCS admin API credentials (for product gating + Discord lookup).

## Install

1. `make build`, upload via `POST /api/v1/admin/plugins/uploads`.
2. Register the OAuth client in WHMCS; redirect URI is `https://<continuum-host>/api/v1/auth/oauth/<install-id>/callback`.
3. Open the plugin's `/admin` SPA, configure server URL, client credentials, product gating, role mapping.

## Build & test

```bash
make build         # installs SPA dependencies, builds web/dist, compiles Go binary
make test          # Go unit tests
make test-web      # Vitest (SPA)
```

## Status

v0.1.0. Functional.
