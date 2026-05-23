# Product gating and role mapping

This page covers exactly how `allowed_product_ids` and `claim_role_mapping`
are evaluated at login time, what counts as an "active" product, and how the
admin SPA's product cache behaves.

## When the admin API is consulted at all

`ExchangeCode` in `internal/auth/server.go` only calls the WHMCS admin API
when at least one of these conditions holds:

- `len(cfg.AllowedProductIDs) > 0` (gating active)
- `len(cfg.ClaimRoleMapping) > 0` (role mapping active)
- `cfg.FetchDiscordID` (Discord enrichment active)

If none of those is true, the plugin authenticates the user purely from the
OAuth userinfo response and never touches `/includes/api.php`. The plugin
returns the user's role as whatever the Silo host's default is — see
the host's role-on-create policy.

## Resolving the WHMCS client

Once any admin-API path is active, the plugin needs a WHMCS `clientid`. It
resolves it like this:

1. Start with `userinfo.id` from `/oauth/userinfo.php`.
2. If `userinfo.email` is non-empty, **override** with the result of
   `GetClientByEmail(email)`. This is intentional: WHMCS's OAuth userinfo
   `id` is the OAuth user record's ID, which on some builds is distinct from
   the client (billing) record's ID. Looking up by email is more reliable.
3. If `GetClientByEmail` returns nil (no match), the login is rejected with
   `PermissionDenied: no WHMCS client found for this email`.

Email comparison is case-insensitive and whitespace-trimmed
(`GetClientByEmail` in `internal/whmcs/api.go`). If WHMCS's `GetClients`
returns multiple matches the plugin picks the first that exactly matches the
authenticated email.

## What counts as an "active" product

`ActiveProductIDs` in `internal/auth/server.go` decides which entries from
`GetClientsProducts` are included in the gating decision and the role
mapping:

| `Status` value as returned by WHMCS | Treated as |
| --- | --- |
| Field omitted entirely (legacy WHMCS) | **Active** — included |
| `"Active"` (case-insensitive, trimmed) | **Active** — included |
| `""` (empty string explicit) | Inactive — excluded |
| `"Suspended"` / `"Terminated"` / `"Cancelled"` / `"Fraud"` / `"Pending"` / anything else | Inactive — excluded |

The empty-string case is the subtle one: the JSON has `"status":""`, not the
field missing. The plugin defensively treats that as inactive. If you ever
deploy against a WHMCS build that returns `""` for genuinely active
products, you must extend the allowlist in `ActiveProductIDs` — the
gating decision will reject everyone otherwise.

## How `allowed_product_ids` filters

The admin SPA writes `allowed_product_ids` as a JSON array of integers; the
plugin stores them as decimal strings inside `runtime.Config.AllowedProductIDs`.

Decision at login:

- Empty list -> any WHMCS account that successfully OAuth'd in is allowed.
- Non-empty list -> the user's `ActiveProductIDs(...)` must intersect with
  the allowed list (`AnyMatch` — set-intersection, not a subset check).
- Failure to intersect -> `PermissionDenied: your WHMCS account doesn't
  have an allowed active product`.

The user-facing error message in Silo may be generic — the actual
reason is in the plugin's logs.

## How `claim_role_mapping` resolves the role

`RoleFromProducts` in `internal/auth/server.go` runs **only on the user's
active products**. The algorithm:

1. Start with `role = "user"`.
2. For each mapping entry whose `role` is `"user"` or `"admin"`:
   - For each owned active product matching `mapping.ProductID`:
     - If `mapping.role == "admin"`, return `"admin"` immediately.
   - Otherwise leave `role` as is.
3. Return `role`.

In other words: **admin wins**. Any single matching admin-mapped product
elevates the user, regardless of other mappings or order. Mappings with a
role other than `"user"` / `"admin"` are silently ignored; `ValidateConfig`
rejects such entries at Configure time so they shouldn't get this far.

The role is exposed as the `silo_role` claim on the
`AuthenticateResponse`. The Silo host is what actually uses this — see
the host's role-mapping logic for how it translates the claim to a Silo
role. The plugin only emits the claim.

Role is computed every login. If you change a user's products in WHMCS,
sign-out and sign-in to pick up the new role; live sessions are unaffected
until they expire.

## The 5-minute product cache

`internal/whmcs/cache.go` keeps the WHMCS product list (the output of
`GetProducts`) in process memory for 5 minutes (`productCacheTTL` in
`cmd/silo-plugin-whmcs-login/main.go`).

This cache backs the SPA's product picker only. **It is not on the login
path.** Each login triggers fresh `GetClientsProducts` calls because per-user
product ownership cannot be cached safely.

The SPA renders these fields on the products response:

- `cached_at` — when the cache was last successfully populated.
- `last_attempt_at` — when the most recent fetch was attempted, success or
  failure.
- `last_error` — error from the most recent attempt (empty if it succeeded).
- `configured` — false if no admin API credentials.

The refresh button on the SPA calls `POST /api/v1/admin/products/refresh`,
which forces an immediate `GetProducts` call regardless of TTL.

When does the cache get reset? Whenever `Configure` runs — that is, when the
config changes through the admin form or the host re-invokes Configure. New
admin API credentials therefore invalidate the cache automatically.

The cache is in-process. Restarting the plugin process clears it.

## "Simulate login" — preview before saving

`POST /api/v1/admin/simulate-login` runs the same gating + role evaluation
against a WHMCS client of your choice. Three useful properties:

- It accepts either `email` or `client_id`. Email is resolved via
  `GetClientByEmail` exactly like a real login.
- It accepts **unsaved** values for `allowed_product_ids`,
  `claim_role_mapping`, and `fetch_discord_id` in the request body, so the
  SPA can preview unsaved page state without persisting first.
- It always returns 200 with structured `ok`/`reason`/`error` fields so the
  SPA can render a coloured outcome, even on failure.

Useful when:

- You added a product ID to the allow list but want to confirm an admin
  user has it before saving.
- You added an admin role mapping and want to confirm only the right people
  get elevated.
- A user complains they can no longer log in — pull up their email in the
  simulator and you'll see the exact product list the plugin sees, with
  active/allowed/role-hit annotations.
</content>
</invoke>