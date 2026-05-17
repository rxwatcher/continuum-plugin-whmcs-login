# WHMCS Login Setup, Debugging, And Flows

Plugin ID: `continuum.whmcs-login`
Version documented: `0.1.0`

## Purpose

Continuum auth provider backed by WHMCS OAuth/OIDC and optional WHMCS admin API checks.

## Runtime Dependencies

- Continuum plugin host
- WHMCS instance with OAuth/OIDC client
- Optional WHMCS admin API credentials for product/Discord enrichment

## Setup Checklist

1. Create/register the WHMCS OAuth client.
2. Configure whmcs_server_url, client_id, client_secret, and display_name.
3. Set allowed_product_ids if only customers with specific products may log in.
4. Configure admin API credentials if product checks or Discord custom field lookup are enabled.
5. Test login and verify claims/roles in Continuum.

## Configuration Reference

- `whmcs_server_url`
- `client_id`
- `client_secret`
- `display_name`
- `allowed_product_ids`
- `whmcs_admin_api_id`
- `whmcs_admin_api_secret`
- `fetch_discord_id`
- `discord_id_custom_field`
- `claim_role_mapping`

Use the plugin manifest/admin form as the source of truth for field validation and defaults. Keep database credentials scoped to the plugin schema unless a plugin explicitly needs read access to Continuum core tables.

## Exposed Routes

- `GET /assets/* [public]`
- `GET /admin/* [admin]`
- `* /api/v1/admin/* [admin]`
- `GET /api/v1/health [public]`

## Capabilities

- `auth_provider.v1 (whmcs) - OAuth/OIDC auth against a WHMCS billing system.`
- `http_routes.v1 (spa) - Admin SPA for configuring allowed products.`

## Operational Flows

### Login

1. User chooses WHMCS login.
2. The plugin redirects to WHMCS authorization.
3. WHMCS returns an auth response to the callback.
4. The plugin validates identity, optionally checks product ownership/admin API fields, maps roles/Discord ID, and returns auth data to Continuum.
5. Continuum establishes the user session.

## How This Plugin Communicates

- Implements auth_provider.v1 for Continuum.
- Talks outward to WHMCS OAuth/OIDC and optional admin API.
- Does not manage media/request flows directly.

## Debugging Runbook

- If callback fails, verify WHMCS URL and registered redirect URI.
- If valid customers are denied, check allowed_product_ids and admin API permissions.
- If Discord ID is missing, verify fetch_discord_id and discord_id_custom_field.
- If roles are wrong, inspect claim_role_mapping JSON.
- Use /api/v1/health for a route-level check.

## Log And Health Checks

- Start with Continuum Admin -> Plugins and confirm the installation is enabled.
- Check the plugin process logs around startup for manifest loading, migration, and route registration.
- Check scheduled task logs when a workflow depends on polling or reconciliation.
- Confirm the plugin routes are reachable through Continuum using the access level shown above.
- For database-backed plugins, verify the configured role can connect, create/migrate tables in its schema, and read/write expected rows.

## Common Failure Patterns

- Wrong installation ID selected in a portal or router setting after reinstalling a plugin.
- Plugin database URL points at the public schema instead of the dedicated plugin schema.
- Reverse proxy forwards the SPA route but not `/api/*`, `/api/v1/*`, `/assets/*`, or provider-specific public routes.
- Network checks are run from the operator laptop instead of from the Continuum/plugin runtime network.
- Secrets are regenerated during restart, invalidating signed URLs, encrypted fields, or login state.

## Verification After Changes

1. Restart or reload the plugin installation.
2. Open the plugin route or admin page in Continuum.
3. Exercise the smallest workflow that crosses a plugin boundary.
4. Confirm both the source plugin and destination plugin record the same request/session/login identifier.
5. Leave the scheduled reconciler enough time to run, then confirm terminal state or a useful error.
