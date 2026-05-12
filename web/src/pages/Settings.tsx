import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { api, patchPluginConfig, installID } from "@/lib/api";

type ClaimRoleMap = { product_id: string; role: string };

type ConfigSummary = {
  whmcs_server_url: string;
  client_id: string;
  has_client_secret: boolean;
  whmcs_admin_api_id: string;
  has_whmcs_admin_api_secret: boolean;
  fetch_discord_id: boolean;
  discord_id_custom_field: string;
  claim_role_mapping: ClaimRoleMap[] | null;
};

type FormState = Partial<ConfigSummary> & {
  client_secret?: string;
  whmcs_admin_api_secret?: string;
};

export default function Settings() {
  const qc = useQueryClient();
  const cfgQ = useQuery({
    queryKey: ["config-summary"],
    queryFn: () => api.get<ConfigSummary>("/api/v1/admin/config-summary"),
  });

  const [form, setForm] = useState<FormState>({});
  const [roleMappingJSON, setRoleMappingJSON] = useState("");

  useEffect(() => {
    if (cfgQ.data) {
      setForm({
        whmcs_server_url: cfgQ.data.whmcs_server_url,
        client_id: cfgQ.data.client_id,
        whmcs_admin_api_id: cfgQ.data.whmcs_admin_api_id,
        fetch_discord_id: cfgQ.data.fetch_discord_id,
        discord_id_custom_field: cfgQ.data.discord_id_custom_field,
      });
      setRoleMappingJSON(JSON.stringify(cfgQ.data.claim_role_mapping ?? [], null, 2));
    }
  }, [cfgQ.data]);

  const save = useMutation({
    mutationFn: async () => {
      const entries: Record<string, { value: unknown }> = {};
      const set = (k: string, v: unknown) => {
        entries[k] = { value: v };
      };

      if (form.whmcs_server_url !== undefined) set("whmcs_server_url", form.whmcs_server_url);
      if (form.client_id !== undefined) set("client_id", form.client_id);
      if (form.client_secret) set("client_secret", form.client_secret);
      if (form.whmcs_admin_api_id !== undefined) set("whmcs_admin_api_id", form.whmcs_admin_api_id);
      if (form.whmcs_admin_api_secret) set("whmcs_admin_api_secret", form.whmcs_admin_api_secret);
      if (form.fetch_discord_id !== undefined) set("fetch_discord_id", form.fetch_discord_id);
      if (form.discord_id_custom_field !== undefined)
        set("discord_id_custom_field", form.discord_id_custom_field);

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
      set("claim_role_mapping", parsedMapping);

      await patchPluginConfig(installID(), entries);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["config-summary"] });
      toast.success("Saved");
      setForm((f) => ({ ...f, client_secret: "", whmcs_admin_api_secret: "" }));
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
        <Field label="OAuth client secret">
          <Input
            type="password"
            placeholder={cfgQ.data?.has_client_secret ? "(unchanged)" : "enter secret"}
            value={form.client_secret ?? ""}
            onChange={(e) => setForm((f) => ({ ...f, client_secret: e.target.value }))}
          />
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
          <Input
            type="password"
            placeholder={
              cfgQ.data?.has_whmcs_admin_api_secret ? "(unchanged)" : "enter secret"
            }
            value={form.whmcs_admin_api_secret ?? ""}
            onChange={(e) =>
              setForm((f) => ({ ...f, whmcs_admin_api_secret: e.target.value }))
            }
          />
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
