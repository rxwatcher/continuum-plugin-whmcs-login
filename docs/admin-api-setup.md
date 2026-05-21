# WHMCS admin API setup

The OAuth/OIDC client alone is enough to authenticate users. The admin API is
required for any of:

- Product gating (`allowed_product_ids` non-empty).
- Role mapping (`claim_role_mapping` non-empty).
- Discord ID enrichment (`fetch_discord_id` true).
- The admin SPA's product picker (the dropdown of WHMCS product IDs).
- The admin SPA's "Simulate login" debugging tool.

Without admin API credentials all of the above silently degrade — see
[Missing vs wrong credentials](#missing-vs-wrong-credentials) at the bottom of
this page.

## 1. Create the API role

In WHMCS admin: Setup -> Staff Management -> Administrator Roles.

Create a role (e.g. `Continuum API`) with the minimum surface needed:

| API action | Reason |
| --- | --- |
| `GetProducts` | Powers the SPA's product dropdown / cache. |
| `GetClients` | Looks up a WHMCS client by email (`GetClientByEmail` uses `GetClients(search=...)`). |
| `GetClientsProducts` | Reads the authenticated user's active products for gating + role mapping. |
| `GetClientsDetails` | Required if `fetch_discord_id` is enabled. Returns the client record and the custom-field map. |

Do not grant other API actions. The plugin does not call them, and a narrow
role limits blast radius if the credentials leak.

## 2. Create the API user

In WHMCS admin: Setup -> Staff Management -> Administrator Users.

- Create a staff user dedicated to this plugin, assigned to the role from
  step 1. WHMCS lets you create staff users that have only API access.
- On the user's profile, generate API credentials (sometimes labeled "API
  Authentication Credentials" or "API Authentication Tokens"). You will get:
  - **API Identifier** — set in the plugin as `whmcs_admin_api_id`.
  - **API Secret** — set in the plugin as `whmcs_admin_api_secret`. Capture
    this immediately; WHMCS does not display it again on most builds.
- Restrict the API user's allowed IP list to the egress IP of the Continuum
  plugin host. WHMCS supports IP allowlisting on API credentials.

## 3. Wire the credentials in the plugin

In the admin SPA, fill `WHMCS admin API ID` and `WHMCS admin API secret`.
Submitting the form triggers a re-Configure on the plugin process; the next
admin API call will use the new credentials.

The plugin only stores secrets when the SPA sends a non-empty value (see
`HandleUpdateConfig` in `internal/admin/server.go`), so leaving the secret
field blank when editing other fields keeps the existing secret. The SPA
never reads secrets back: `config-summary` exposes only `has_client_secret`
and `has_whmcs_admin_api_secret`.

## 4. Verify

In the admin SPA:

1. Open the "Allowed products" section. The dropdown should populate. If it
   stays empty and the freshness panel shows an error, the API credentials
   are wrong; see below.
2. Press the refresh button. The "Last attempt" timestamp should update to
   "just now"; "Last error" should be empty.
3. Use the "Simulate login" panel with an email of a known WHMCS client.
   You should see a JSON response with `ok: true`, a `client_id`, a list of
   owned products with active/allowed/role-hit flags, and a final
   `allowed` / `role` decision.

## Missing vs wrong credentials

Two different failure modes are worth distinguishing.

### Credentials are not set at all

- `applyConfig` in `cmd/continuum-plugin-whmcs-login/main.go` constructs no
  product cache (`prodCache` stays `nil`).
- `GET /api/v1/admin/products` returns a 200 with
  `{"configured": false, "message": "WHMCS admin API credentials are
  required ..."}`. The SPA renders setup guidance, not an error toast.
- `POST /api/v1/admin/simulate-login` returns `{"ok": false, "reason":
  "admin_api_required"}`. The SPA shows a "Configure admin API credentials
  first" hint.
- A real login attempt fails with `FailedPrecondition` from
  `ExchangeCode` **only if** product gating, role mapping, or Discord ID
  enrichment is configured. If none of those is enabled, login completes
  without ever touching the admin API.

### Credentials are set but wrong

- The plugin still constructs a `whmcs.APIClient`. Every call hits
  `/includes/api.php` and WHMCS returns an envelope with
  `result: "error"` and a message like `Invalid IP` or
  `Authentication Failed`.
- `GET /api/v1/admin/products` still returns 200, but `last_error` carries
  the WHMCS message and `products` is empty. The freshness panel surfaces
  it. The cache never gets populated, so every poll re-hits WHMCS.
- `POST /api/v1/admin/simulate-login` returns
  `{"ok": false, "reason": "client_lookup_failed", "error": "<whmcs
  message>"}` or similar at whichever step first calls the API.
- A real login attempt fails with `Internal` from `ExchangeCode` with the
  WHMCS message. Users see a generic "login failed" page from the Continuum
  host; the WHMCS message only appears in the plugin's logs.

### Common causes of "wrong"

- API user not assigned the role, or the role is missing one of the four
  actions listed above. WHMCS reports this as `Authentication Failed for
  ...` even though the credentials are valid.
- IP allowlist on the API user does not include the Continuum plugin host's
  egress IP. WHMCS reports `IP Address ... is not on the IP Allow List`.
- The plugin host is behind a NAT or load balancer and the egress IP is not
  what you think. Test from the plugin runtime, not from your laptop.
- Trailing slash or wrong scheme on the WHMCS server URL — the plugin trims
  trailing slashes but it does not normalise scheme or host.
</content>
</invoke>