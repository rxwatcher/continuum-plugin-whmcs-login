# WHMCS Login — operator docs

These are deep-dive operator docs. For the high-level overview, the auth-flow
summary, the configuration key reference, and the build/release section, read
the [top-level README](../README.md) first.

This tree assumes you are running the plugin under Continuum and have admin
access to both the Continuum admin UI and the upstream WHMCS instance.

## Where to start

| If you are doing this... | Read this |
| --- | --- |
| First-time install on a fresh Continuum host | [whmcs-oauth-setup.md](whmcs-oauth-setup.md), then [admin-api-setup.md](admin-api-setup.md) |
| Wiring product gating or role mapping | [product-gating-and-roles.md](product-gating-and-roles.md) |
| Adding the Discord ID claim | [discord-id-enrichment.md](discord-id-enrichment.md) |
| Login fails / users denied / wrong role | [debugging.md](debugging.md) |
| You hit a weird WHMCS response | [whmcs-quirks.md](whmcs-quirks.md) |

## What this plugin is

`continuum.whmcs-login` is an `auth_provider.v1` plugin that authenticates
Continuum users against a WHMCS billing system using its OAuth2/PKCE endpoints
and, optionally, gates access by active product ownership reported by the
WHMCS admin API. The admin SPA at `/admin/` is the operator's primary
interface.

## Schema layout

The plugin owns a Postgres schema (typically `whmcs_login`) with one table:

```
whmcs_login.app_config
  id INTEGER PRIMARY KEY DEFAULT 1   -- singleton row, CHECK (id = 1)
  data JSONB NOT NULL                -- serialized runtime.Config
  updated_at TIMESTAMPTZ
```

The plugin owns this schema and runs migrations on every Configure. Do not
share this schema with other plugins or the Continuum core.

## Things that are not here

- Auth flow diagrams and the config key table — in the README.
- End-user / customer docs — this is an admin-only plugin; there is no
  end-user surface beyond the WHMCS-hosted OAuth consent screen.
</content>
</invoke>