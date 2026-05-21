# WHMCS quirks and edge cases

WHMCS's HTTP surface is older than most modern OIDC providers and has a few
shape inconsistencies the plugin tolerates. This page collects the ones an
operator is most likely to hit when debugging, plus the historical context
for why the plugin's parser is shaped the way it is.

## Admin API authentication

`/includes/api.php` is **POST only**, **form-encoded**, and the credentials
ride in the body as `identifier` and `secret`. No bearer tokens, no signed
requests. Implications:

- A reverse proxy that strips `Content-Type: application/x-www-form-urlencoded`
  will break the plugin even though OAuth still works.
- WHMCS evaluates the **IP allowlist on the API user** against the
  TCP-level source IP it sees. If your plugin host is behind a proxy that
  rewrites source IPs (or behind cloud NAT), the IP WHMCS sees is the
  egress IP, not the proxy's. WHMCS does not honour
  `X-Forwarded-For` here.
- Every response is wrapped in `{"result": "success" | "error", "message":
  "..."}`. A 200 status code with `"result":"error"` is the normal failure
  path, **not** an HTTP 4xx. The plugin's `post` helper in
  `internal/whmcs/api.go` parses this envelope and surfaces the message as
  the Go error.

## Type coercion in WHMCS payloads

WHMCS is loose about integer vs string in JSON. The plugin handles these
specifically:

| Field | Shape observed in the wild | Plugin handling |
| --- | --- | --- |
| `products.product[].pid` | int *or* string | Decoded into a `json.RawMessage`, then coerced via `parseRequiredPositiveInt`. Must be positive. |
| `products.product[].gid` | int, string, or omitted | Coerced via `parseOptionalInt`. Optional. |
| `client.id` (from `GetClientsDetails`, `GetClients`) | int *or* string | Read as `json.RawMessage` and rendered to string via `rawToString`. |

If you see "pid: must be a positive integer" from the plugin, something
upstream returned a non-numeric or zero PID — usually a sign of a corrupt or
mis-imported product.

## "products" envelope shapes

`GetProducts` and `GetClientsProducts` agree on the *outer* envelope name
but disagree on the *inner* shape depending on WHMCS version and how many
products are in the list. The plugin's `extractProductArray` tolerates all
four observed shapes:

```jsonc
// Normal: array of products
{"products": {"product": [ {...}, {...} ]}}

// Single product: WHMCS returns an object, not a one-element array
{"products": {"product": {...}}}

// Empty list, modern WHMCS
{"products": []}

// Empty list, older WHMCS
{"products": ""}
```

If you ever see "decode products envelope" or "decode products inner"
errors from the plugin, WHMCS has returned a fifth shape; capture the body
and extend the parser.

## `GetClientsProducts` and the `status` field

This is the most operationally important quirk.

Older WHMCS versions **omit** the `status` field on
`GetClientsProducts`. Newer versions **include** it. The plugin
distinguishes:

- Field omitted entirely -> treat as Active (legacy compat).
- Field present, value `"Active"` (any case, trimmed) -> Active.
- Field present, value `""` -> **Inactive**. This is defensive: rather than
  treat unknown empty status as Active, the plugin excludes it from the
  allowed/role set.
- Anything else (`Suspended`, `Cancelled`, ...) -> Inactive.

If you migrate WHMCS versions and previously-passing logins start failing,
inspect a `GetClientsProducts` response directly:

```bash
curl -s -X POST https://<whmcs>/includes/api.php \
  -d "identifier=<id>&secret=<secret>&action=GetClientsProducts&clientid=<cid>&responsetype=json" \
  | jq '.products.product'
```

If you see `"status": ""` on rows that should be active, the new WHMCS
build started emitting empty-string for active products — extend
`ActiveProductIDs` in `internal/auth/server.go` to treat empty as active
(only after confirming that's the upstream contract; do not just guess).

## `GetClientsDetails.customfields` ordering

The `customfields` array is **not ordered consistently** between WHMCS
versions or between `GetClients` and `GetClientsDetails`. Don't rely on
positional access. The plugin loads them into a name-keyed map; the only
risk there is collision (two custom fields with the same display name —
last one wins). Rename one if you hit that.

## OAuth scopes

The plugin always asks for `openid profile email`. If you allowlist scopes
in WHMCS:

- `openid` is mandatory — without it, `userinfo.sub` is empty and the
  plugin rejects the login with `userinfo response missing subject`.
- `profile` populates `name`, `given_name`, `family_name`, `picture`.
  Optional but recommended; lack of it means Continuum can't render a
  display name beyond the email.
- `email` populates `email`. Strongly recommended — without it,
  `GetClientByEmail` can't run, so the plugin falls back to using the
  OAuth `id` as the WHMCS client ID. That **only** works if your WHMCS
  build returns matching IDs between the OAuth user record and the
  client record.

The plugin does not currently support adding extra scopes via config; the
default set is hardcoded in `BuildAuthorizeURL`.

## PKCE

The plugin always uses PKCE with S256. If your WHMCS OAuth client has
"Require PKCE" off, login still works (the verifier is sent regardless).
If WHMCS rejects the verifier for any reason, the token exchange returns
an error and `ExchangeCode` surfaces it as `Internal: token exchange:
token endpoint <status>: <body>`.

Never disable PKCE for this client — there is no fallback path.

## Refresh tokens

The plugin's `RefreshSession` RPC returns `Unimplemented`. If WHMCS issues
a refresh token (because the client allows it), the plugin discards it.
Continuum handles session renewal on its own, by re-running the OAuth flow.
Don't bother enabling refresh_token on the WHMCS client.

## "OAuth Server" vs older "OpenID Connect" module

WHMCS has, at various points, shipped:

- A community-developed OpenID Connect module.
- The official "OAuth Server" / "Identity Provider" module bundled with
  modern WHMCS (8.x+).

This plugin targets the modern bundled module. The community module's URL
shapes vary by author; if your `/oauth/authorize.php` returns a 404 or a
WHMCS template page, you're probably running the community module — install
the bundled one.

## Reverse-proxy gotchas

The plugin exposes four route prefixes (`/assets/*`, `/admin/*`,
`/api/v1/admin/*`, `/api/v1/health`). Continuum's host already proxies
plugin routes; you don't normally need to do anything. But if you run an
edge reverse proxy in front of Continuum:

- `/admin/*` must forward unchanged, including the `*` trailing path
  segments. The SPA is a single-page app and uses HTML5 history routing.
- `/api/v1/admin/*` must forward the request method and JSON body. PATCH is
  used by config updates; some restrictive proxies disable PATCH by default.
- The `X-Continuum-User-Role`, `X-Continuum-User-Id`, and
  `X-Continuum-User-Theme` headers are injected by the Continuum host. Do
  not pass them through from an external client — the admin middleware
  trusts them, and the host strips them on inbound requests. An edge proxy
  must do the same.
</content>
</invoke>