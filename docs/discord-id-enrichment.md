# Discord ID enrichment

The plugin can lift a single WHMCS custom field value off the authenticated
client and expose it on the `AuthenticateResponse` as the `discord_id`
claim. This is the only optional claim the plugin emits beyond
`raw_userinfo`, `products`, and `silo_role`.

## Prerequisites

- Admin API credentials are configured (this path is admin-API-only;
  WHMCS's OAuth userinfo does not expose custom fields).
- A WHMCS custom field of type Text or similar exists on the **Client**
  record (Setup -> Custom Client Fields). The field's *Field Name* is what
  the plugin matches against.

## Configuration

| Config key | Default | Effect |
| --- | --- | --- |
| `fetch_discord_id` | false | Master switch. When false, no claim is emitted and `GetClientsDetails` is not called for the Discord lookup. |
| `discord_id_custom_field` | `Discord ID` | The exact custom-field *name* the plugin looks for in WHMCS. Case-sensitive in WHMCS's custom-field map. |

The default name `Discord ID` (with a single space) is preserved through
config parsing in two places: `runtime.loadConfig` and `store.GetConfig`. An
empty value in either path reverts to the default rather than disabling the
field name. To truly disable enrichment, set `fetch_discord_id = false`.

## How the lookup works

In `ExchangeCode` (after the gating step):

```go
if cfg.FetchDiscordID {
    cd, err := api.GetClientsDetails(ctx, clientID)
    if err == nil {
        if id, ok := cd.CustomFields[cfg.DiscordIDCustomField]; ok && id != "" {
            claims["discord_id"] = id
        }
    }
    // Discord ID failure is non-fatal — login succeeds, claim is absent.
}
```

Important properties:

- The lookup is **non-fatal**. If `GetClientsDetails` errors, if the custom
  field is missing, or if its value is empty, login still completes; the
  `discord_id` claim is simply not present in the response.
- The match is on the **field name** (`fieldname` in the WHMCS payload),
  not the field ID. If you rename the custom field in WHMCS without updating
  the plugin config, enrichment silently stops.
- Whitespace is **not** trimmed from the value. If users self-edit their
  Discord ID in the WHMCS client area and add a stray space, the claim
  carries that space. Validate or normalize downstream.

## Common pitfalls

- **Field is on the Order/Product, not the Client.** WHMCS distinguishes
  product-level and client-level custom fields. `GetClientsDetails` only
  returns the client-level fields. Put the Discord ID field under "Custom
  Client Fields", not under a product's custom fields.
- **Field is admin-only.** If the admin-only checkbox is set, WHMCS hides
  the field from end users in the client area but it still returns through
  `GetClientsDetails`, so the plugin will read it. That can be the design,
  but be aware.
- **The custom field is not unique by name.** WHMCS allows two custom client
  fields with the same display name; whichever one comes back last in the
  `customfields` array wins, because the plugin loads them into a map keyed
  by name. Rename one if you hit this.
- **Casing.** `Discord ID` is not the same key as `discord_id` or
  `Discord Id`. Match the WHMCS *Field Name* exactly. If you set the field
  up with a different display name, update `discord_id_custom_field` to
  match.

## Verifying

Use the SPA's "Simulate login" with `fetch_discord_id` overridden to true.
The response carries `discord_id` (possibly empty string) and the
`client_details` block; cross-check that the `client_details` block came
back at all and that your field name resolves to a value.
</content>
</invoke>