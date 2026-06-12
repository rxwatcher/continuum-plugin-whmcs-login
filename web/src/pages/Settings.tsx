import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { copyText } from "@/lib/copyText";
import { currentOAuthCallbackUrl } from "@/lib/oauthCallbackUrl";

type ClaimRoleMap = { product_id: string; role: string };

type ConfigSummary = {
  whmcs_server_url: string;
  client_id: string;
  has_client_secret: boolean;
  icon_url_path: string;
  whmcs_admin_api_id: string;
  has_whmcs_admin_api_secret: boolean;
  fetch_discord_id: boolean;
  discord_id_custom_field: string;
  claim_role_mapping: ClaimRoleMap[] | null;
  link_by_email: boolean;
};

// Secrets (client secret, admin API secret) are host-managed: stored encrypted
// by the host and injected at runtime, so they are NOT editable here.
type FormState = Partial<ConfigSummary>;

export default function Settings() {
  const qc = useQueryClient();
  const cfgQ = useQuery({
    queryKey: ["config-summary"],
    queryFn: () => api.get<ConfigSummary>("/api/v1/admin/config-summary"),
  });

  const [form, setForm] = useState<FormState>({});
  const [roleMappingJSON, setRoleMappingJSON] = useState("");
  const callbackUrl = currentOAuthCallbackUrl();
  const copyCallbackUrl = async () => {
    if (await copyText(callbackUrl)) {
      toast.success("Callback URL copied");
    } else {
      toast.error("Copy failed. Select the URL and copy it manually.");
    }
  };

  useEffect(() => {
    if (cfgQ.data) {
      setForm({
        whmcs_server_url: cfgQ.data.whmcs_server_url,
        client_id: cfgQ.data.client_id,
        icon_url_path: cfgQ.data.icon_url_path,
        whmcs_admin_api_id: cfgQ.data.whmcs_admin_api_id,
        fetch_discord_id: cfgQ.data.fetch_discord_id,
        discord_id_custom_field: cfgQ.data.discord_id_custom_field,
        link_by_email: cfgQ.data.link_by_email,
      });
      setRoleMappingJSON(JSON.stringify(cfgQ.data.claim_role_mapping ?? [], null, 2));
    }
  }, [cfgQ.data]);

  const save = useMutation({
    mutationFn: async () => {
      const body: Record<string, unknown> = {};

      if (form.whmcs_server_url !== undefined) body.whmcs_server_url = form.whmcs_server_url;
      if (form.client_id !== undefined) body.client_id = form.client_id;
      if (form.icon_url_path !== undefined) body.icon_url_path = form.icon_url_path;
      if (form.whmcs_admin_api_id !== undefined) body.whmcs_admin_api_id = form.whmcs_admin_api_id;
      if (form.fetch_discord_id !== undefined) body.fetch_discord_id = form.fetch_discord_id;
      if (form.discord_id_custom_field !== undefined)
        body.discord_id_custom_field = form.discord_id_custom_field;
      if (form.link_by_email !== undefined) body.link_by_email = form.link_by_email;

      let parsedMapping: ClaimRoleMap[];
      try {
        parsedMapping = JSON.parse(roleMappingJSON || "[]");
      } catch (e) {
        throw new Error(`Invalid claim_role_mapping JSON: ${(e as Error).message}`);
      }
      if (!Array.isArray(parsedMapping)) {
        throw new Error("claim_role_mapping must be a JSON array");
      }
      for (const [i, m] of parsedMapping.entries()) {
        if (typeof m !== "object" || m === null) {
          throw new Error(`claim_role_mapping[${i}] must be an object`);
        }
        if (typeof m.product_id !== "string" || m.product_id === "") {
          throw new Error(`claim_role_mapping[${i}].product_id must be a non-empty string`);
        }
        if (m.role !== "user" && m.role !== "admin") {
          throw new Error(`claim_role_mapping[${i}].role must be 'user' or 'admin'`);
        }
      }
      body.claim_role_mapping = parsedMapping;

      await api.patch("/api/v1/admin/config", body);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["config-summary"] });
      toast.success("Saved");
    },
    onError: (e: Error) => toast.error(e.message),
  });

  if (cfgQ.isLoading) return <Skeleton className="h-96 w-full" />;
  if (cfgQ.error) {
    return (
      <div className="text-destructive">
        Failed to load config: {String(cfgQ.error)}
      </div>
    );
  }

  return (
    <div className="max-w-2xl space-y-8">
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>

      <Section title="WHMCS connection">
        <Field label="Callback URL">
          <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
            <Input readOnly value={callbackUrl} />
            <Button type="button" variant="outline" onClick={copyCallbackUrl}>
              Copy
            </Button>
          </div>
          <p className="text-muted-foreground text-xs">
            Paste this as the redirect or callback URL in WHMCS OAuth credentials.
          </p>
        </Field>
        <Field label="WHMCS server URL">
          <Input
            value={form.whmcs_server_url ?? ""}
            onChange={(e) =>
              setForm((f) => ({ ...f, whmcs_server_url: e.target.value }))
            }
            placeholder="https://billing.example.com"
          />
        </Field>
        <Field label="OAuth client ID">
          <Input
            value={form.client_id ?? ""}
            onChange={(e) => setForm((f) => ({ ...f, client_id: e.target.value }))}
          />
        </Field>
        <Field label="Custom icon URL or path">
          <Input
            value={form.icon_url_path ?? ""}
            onChange={(e) => setForm((f) => ({ ...f, icon_url_path: e.target.value }))}
            placeholder="https://example.com/whmcs.svg or /assets/whmcs-logo.svg"
          />
          <p className="text-muted-foreground text-xs">
            Leave blank to use the bundled WHMCS logo.
          </p>
        </Field>
        <Field label="OAuth client secret">
          <HostManagedSecret configured={!!cfgQ.data?.has_client_secret} />
        </Field>
      </Section>

      <Section
        title="Admin API"
        description="Required for product gating and Discord ID fetch."
      >
        <Field label="API identifier">
          <Input
            value={form.whmcs_admin_api_id ?? ""}
            onChange={(e) =>
              setForm((f) => ({ ...f, whmcs_admin_api_id: e.target.value }))
            }
          />
        </Field>
        <Field label="API secret">
          <HostManagedSecret configured={!!cfgQ.data?.has_whmcs_admin_api_secret} />
        </Field>
      </Section>

      <Section title="Discord ID">
        <Field label="">
          <label className="inline-flex items-center gap-2 text-sm">
            <Checkbox
              checked={!!form.fetch_discord_id}
              onCheckedChange={(v) =>
                setForm((f) => ({ ...f, fetch_discord_id: !!v }))
              }
            />
            <span>Fetch Discord ID from WHMCS custom field</span>
          </label>
        </Field>
        <Field label="Custom field name">
          <Input
            value={form.discord_id_custom_field ?? ""}
            onChange={(e) =>
              setForm((f) => ({ ...f, discord_id_custom_field: e.target.value }))
            }
          />
        </Field>
      </Section>

      <Section
        title="Account linking"
        description="Controls whether the host may link a WHMCS sign-in to an existing Silo account that shares the same email."
      >
        <Field label="">
          <label className="inline-flex items-center gap-2 text-sm">
            <Checkbox
              checked={!!form.link_by_email}
              onCheckedChange={(v) => setForm((f) => ({ ...f, link_by_email: !!v }))}
            />
            <span>Allow linking to an existing account by email (unverified)</span>
          </label>
          <p className="text-muted-foreground text-xs">
            Off by default. Linking on an unverified email is an account-takeover
            vector; enable only if you trust your WHMCS email verification.
          </p>
        </Field>
      </Section>

      <Section
        title="Claim role mapping (advanced)"
        description={`Array of {"product_id":"5","role":"admin"} entries. Empty = everyone is 'user'.`}
      >
        <Field label="JSON">
          <textarea
            className="bg-background border-input w-full rounded-md border p-2 font-mono text-sm"
            rows={8}
            value={roleMappingJSON}
            onChange={(e) => setRoleMappingJSON(e.target.value)}
          />
        </Field>
      </Section>

      <div className="flex justify-end">
        <Button onClick={() => save.mutate()} disabled={save.isPending}>
          Save
        </Button>
      </div>
    </div>
  );
}

function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="border-border/70 bg-card space-y-4 rounded-2xl border p-6">
      <div>
        <h2 className="text-lg font-medium">{title}</h2>
        {description && (
          <p className="text-muted-foreground mt-1 text-xs">{description}</p>
        )}
      </div>
      {children}
    </section>
  );
}

// HostManagedSecret renders a read-only indicator for a secret the host owns.
// These secrets are declared in the plugin manifest's global_config_schema with
// secret: true, stored encrypted by the host, and injected at runtime — they
// are configured in the host's plugin settings, not here.
function HostManagedSecret({ configured }: { configured: boolean }) {
  return (
    <div className="space-y-1">
      <div className="text-sm">
        {configured ? "Configured (host-managed)" : "Not configured"}
      </div>
      <p className="text-muted-foreground text-xs">
        Managed by the host: stored encrypted and injected at runtime. Set or
        change it from the plugin's settings in the Silo host admin.
      </p>
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      {label && <Label>{label}</Label>}
      {children}
    </div>
  );
}
