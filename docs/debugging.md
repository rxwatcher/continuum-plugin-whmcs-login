# Debugging runbook

Concrete failure modes operators hit, plus the fastest way to discriminate
between them. Use this with `internal/auth/server.go` and the SPA's
"Simulate login" tool open.

## First-pass checks

1. **Is the install enabled?** Continuum admin -> Plugins. A disabled
   install does not register routes, so `/admin/` 404s.
2. **Health probe.** `curl https://<host>/api/v1/health` should return
   `{"ok":true}`. If it doesn't, the plugin process isn't serving HTTP at
   all — check the host's reverse proxy and the plugin process log.
3. **Whoami.** Browse to `/api/v1/admin/whoami` while logged in. The
   response shows `user_id`, `role`, and `theme`. If `role` is not `admin`,
   you'll hit 403s on every other admin endpoint — the SPA renders a
   friendly "admin required" notice rather than a hard 403 page (see the
   `/whoami` carve-out in `internal/admin/server.go`).
4. **Config summary.** `GET /api/v1/admin/config-summary` returns a
   redacted view (`has_client_secret`, `has_whmcs_admin_api_secret`, the
   list of `allowed_product_ids`, etc.). If a field you set in the form
   doesn't show up here, the Configure step failed — see the plugin
   process log.

## "Login redirects me but lands on an error page"

| Symptom | Most likely cause | Fix |
| --- | --- | --- |
| WHMCS shows `invalid_redirect_uri` before consent | Redirect URI in WHMCS doesn't match Continuum exactly | Re-check scheme, host, port, install ID. The install ID changes if you reinstall the plugin. |
| WHMCS consent screen completes, Continuum shows generic login error | Token exchange failed | Look in plugin log for `token exchange:` — WHMCS returns the reason verbatim. Common: wrong client secret, wrong allowed grant types, PKCE disabled on the client. |
| Plugin log shows `state mismatch` | The `state` parameter didn't round-trip. Browser dropped a cookie, multiple tabs raced, or the install ID changed mid-flow | Have the user try again in a clean window. |
| Plugin log shows `missing pkce_verifier in provider_state` | Continuum host did not preserve `provider_state` across the redirect | Check Continuum host version / configuration. The plugin places the verifier in `provider_state`; the host must round-trip it to the callback. |
| Plugin log shows `userinfo response missing subject` | WHMCS returned a userinfo payload without `sub` or `id` | OAuth Identity Provider misconfigured on WHMCS — confirm scopes include `openid`. |

## "Valid customers are denied"

The plugin emits `PermissionDenied` from `ExchangeCode` for three distinct
reasons. The log message tells you which.

| Log message | Meaning |
| --- | --- |
| `no WHMCS client found for this email` | `GetClientByEmail` returned no match for the OAuth userinfo email. The user OAuth'd in fine but has no matching billing record. Common when the user has multiple WHMCS users and the OAuth screen returned a non-primary email. |
| `your WHMCS account doesn't have an allowed active product` | The intersection of `allowed_product_ids` and the user's active products is empty. |
| (none — generic `Internal`) | Some upstream WHMCS call failed. See `fetch client by email:` / `fetch client products:` prefixes for which call. |

For the "no allowed active product" case, the fastest diagnostic is the
admin SPA's "Simulate login":

1. Enter the affected user's email.
2. Look at the returned `products` table.
3. `Active = true` and `Allowed = true` on any row -> the user **should** be
   passing. If they're not, something else changed between the simulator
   call and the real login (configuration race? They have multiple WHMCS
   accounts under the same email?).
4. `Active = false` on the rows you expected to be active -> the product is
   not in WHMCS status `Active`. Check the user's billing record.
5. `Allowed = false` everywhere -> the product IDs the user owns aren't in
   `allowed_product_ids`. Either add them or you've identified that this
   user genuinely shouldn't be allowed.

## "Roles are wrong"

The simulator's `role_hit` column shows, per product, which mapping (if any)
fired. Re-read [product-gating-and-roles.md](product-gating-and-roles.md);
the rule is "admin wins" — any single owned active product with role
`admin` elevates the user, regardless of order.

If `role` in the simulator response is `user` but you expected `admin`:

- The product ID you mapped to `admin` is not in the user's
  `owned_active` list — they don't own it, or it's suspended/cancelled/etc.
- Your mapping's `product_id` is a string but doesn't match the integer
  WHMCS PID exactly. The plugin normalises both sides through `strconv`
  but you can still have a trailing space or non-numeric value. Re-save
  through the SPA.

If `role` is `admin` but you expected `user`:

- Some other product the user owns is mapped to `admin`. Use the simulator
  to find which one (`role_hit` column).

## "Discord ID is missing"

See [discord-id-enrichment.md](discord-id-enrichment.md). The lookup is
non-fatal so the login completes either way; debug it through the simulator
(set `fetch_discord_id` true in the request body).

## "Admin SPA shows no products"

In order:

1. Is `configured` false in the products response? Admin API credentials
   aren't set — see [admin-api-setup.md](admin-api-setup.md).
2. Is `last_error` populated? Read the WHMCS message verbatim. Common ones:
   `Authentication Failed` (wrong credentials), `IP Address ... is not on
   the IP Allow List` (egress IP not allowlisted in WHMCS), `Invalid
   Action` (role missing `GetProducts` permission).
3. Is `last_error` empty and `products` empty? WHMCS genuinely returned no
   products. Confirm against the WHMCS UI.

## Cache and freshness

The product cache (5 min TTL) is in-process. If you've just changed the
admin API user, the cache is rebuilt automatically because `applyConfig`
constructs a fresh `whmcs.ProductCache` on every Configure call.

If WHMCS-side product list changed and you want it reflected immediately,
hit the refresh button in the SPA — it calls
`POST /api/v1/admin/products/refresh`, which bypasses the TTL and rewrites
`cached_at`.

If you suspect the cache is stuck for any reason: restart the plugin
process. The cache is in-memory only.

## Database / schema issues

The plugin owns the `whmcs_login` schema with a single table
`app_config`. Common failures:

- Database connection fails -> `Configure` returns an error, the plugin
  has no HTTP handler wired (the `httproutes` server starts with a 503
  fallback). The SPA at `/admin/` will be unreachable. Check
  `database_url`, the Postgres role, and `search_path`.
- The role can connect but can't `CREATE TABLE` -> `store.Migrate` errors
  out at startup. Grant `CREATE` on the schema to the plugin's role.
- DSN points at the wrong schema (or `public`) -> migrations succeed but
  the table ends up where you don't want it. Always set
  `search_path=whmcs_login` in the DSN, and make sure the role's
  search_path doesn't override.

## Where to look in the code

| Behaviour | File |
| --- | --- |
| OAuth client (authorize URL, token exchange, userinfo) | `internal/whmcs/oauth.go` |
| WHMCS admin API client + decoders | `internal/whmcs/api.go` |
| Product cache | `internal/whmcs/cache.go` |
| Gating + role mapping (`ExchangeCode`) | `internal/auth/server.go` |
| Admin SPA HTTP handlers | `internal/admin/server.go` |
| Config parsing + validation | `internal/runtime/runtime.go` |
| Persistence (`app_config` JSONB) | `internal/store/store.go` |
| Process wiring (Configure -> apply -> HTTP handler) | `cmd/continuum-plugin-whmcs-login/main.go` |
| Chi router / route registration | `internal/server/server.go` |
</content>
</invoke>