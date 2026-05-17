# WHMCS Login for Continuum

`continuum.whmcs-login` adds OAuth sign-in to Continuum using a WHMCS billing
system. It can optionally allow only clients with specific active WHMCS
products, fetch a Discord ID custom field, and map WHMCS product ownership to
Continuum roles.

Use this plugin when WHMCS is your source of customer identity and entitlement.
Use `continuum.oidc-login` when your identity provider supports standard OIDC.

## Features

- OAuth2/PKCE login against WHMCS OAuth endpoints.
- Per-install login-button display name.
- Optional product gating.
- Optional role mapping from WHMCS product IDs to Continuum roles.
- Optional Discord ID lookup through a WHMCS client custom field.
- Admin SPA for configuring products, role mapping, and diagnostics.
- Stateless runtime; no plugin-owned Postgres schema is required.

## Configuration

| Key | Required | Description |
|---|---|---|
| `whmcs_server_url` | yes | WHMCS base URL, no trailing slash. |
| `client_id` | yes | WHMCS OAuth client ID. |
| `client_secret` | yes | WHMCS OAuth client secret. |
| `display_name` | no | Login-button label. |
| `allowed_product_ids` | no | Comma-separated WHMCS product IDs. Empty allows all WHMCS accounts. |
| `whmcs_admin_api_id` | conditional | Required for product gating and Discord ID lookup. |
| `whmcs_admin_api_secret` | conditional | Required for product gating and Discord ID lookup. |
| `fetch_discord_id` | no | Include the configured Discord ID custom field as a claim. |
| `discord_id_custom_field` | no | WHMCS custom-field name. Defaults to `Discord ID`. |
| `claim_role_mapping` | no | JSON array of `{product_id, role}` entries. |

Redirect URI to register in WHMCS:

```text
https://<continuum-host>/api/v1/auth/oauth/<install-id>/callback
```

## Role Mapping

`claim_role_mapping` is a JSON array. `role` must be `user` or `admin`.

```json
[
  {"product_id": "5", "role": "user"},
  {"product_id": "12", "role": "admin"}
]
```

Role assignment is applied at login. If a client's WHMCS products change during
an active session, the new role applies after the next sign-in.

## Setup

1. Register an OAuth client in WHMCS.
2. Install this plugin and note its installation ID.
3. Configure the WHMCS redirect URI using that install ID.
4. Open the plugin admin page and enter server URL, OAuth credentials, product
   gating, and role mapping.
5. Test with a non-admin client account before enabling product gates broadly.

## Build And Test

```bash
make build
make test
make test-web
```
