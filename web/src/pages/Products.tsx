import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, RefreshCw, Server, Settings, ShieldCheck, XCircle } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { api, patchPluginConfig, installID } from "@/lib/api";
import ProductSelector, { type Product } from "@/components/ProductSelector";

type ProductsResponse = { products: Product[]; cached_at: string };
type ConfigSummary = {
  allowed_product_ids: number[];
  whmcs_server_url: string;
  client_id: string;
  has_client_secret: boolean;
  display_name: string;
  whmcs_admin_api_id: string;
  has_whmcs_admin_api_secret: boolean;
};

type ConnectionFormState = {
  whmcs_server_url: string;
  client_id: string;
  client_secret: string;
  display_name: string;
  whmcs_admin_api_id: string;
  whmcs_admin_api_secret: string;
};

export default function Products() {
  const qc = useQueryClient();
  const [connectionForm, setConnectionForm] = useState<ConnectionFormState>({
    whmcs_server_url: "",
    client_id: "",
    client_secret: "",
    display_name: "",
    whmcs_admin_api_id: "",
    whmcs_admin_api_secret: "",
  });

  const productsQ = useQuery({
    queryKey: ["products"],
    queryFn: () => api.get<ProductsResponse>("/api/v1/admin/products"),
  });
  const configQ = useQuery({
    queryKey: ["config-summary"],
    queryFn: () => api.get<ConfigSummary>("/api/v1/admin/config-summary"),
  });

  useEffect(() => {
    if (!configQ.data) return;
    setConnectionForm((current) => ({
      ...current,
      whmcs_server_url: configQ.data.whmcs_server_url ?? "",
      client_id: configQ.data.client_id ?? "",
      display_name: configQ.data.display_name ?? "",
      whmcs_admin_api_id: configQ.data.whmcs_admin_api_id ?? "",
    }));
  }, [configQ.data]);

  const refresh = useMutation({
    mutationFn: () => api.post<ProductsResponse>("/api/v1/admin/products/refresh"),
    onSuccess: (data) => {
      qc.setQueryData(["products"], data);
      toast.success("Products refreshed");
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const save = useMutation({
    mutationFn: async (csv: string) => {
      await patchPluginConfig(installID(), { allowed_product_ids: { value: csv } });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["config-summary"] });
      toast.success("Saved");
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const saveConnection = useMutation({
    mutationFn: async () => {
      const entries: Record<string, { value: unknown }> = {
        whmcs_server_url: { value: connectionForm.whmcs_server_url.trim() },
        client_id: { value: connectionForm.client_id.trim() },
        display_name: { value: connectionForm.display_name.trim() },
        whmcs_admin_api_id: { value: connectionForm.whmcs_admin_api_id.trim() },
      };
      if (connectionForm.client_secret.trim()) {
        entries.client_secret = { value: connectionForm.client_secret.trim() };
      }
      if (connectionForm.whmcs_admin_api_secret.trim()) {
        entries.whmcs_admin_api_secret = {
          value: connectionForm.whmcs_admin_api_secret.trim(),
        };
      }
      await patchPluginConfig(installID(), entries);
    },
    onSuccess: async () => {
      toast.success("Connection saved");
      setConnectionForm((current) => ({
        ...current,
        client_secret: "",
        whmcs_admin_api_secret: "",
      }));
      await qc.invalidateQueries({ queryKey: ["config-summary"] });
      await qc.invalidateQueries({ queryKey: ["products"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  if (productsQ.isLoading || configQ.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-1/3" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-96 w-full" />
      </div>
    );
  }

  // /products returns 503 when admin API creds aren't set yet. Render a
  // helpful empty state so admins know how to proceed.
  if (productsQ.error) {
    const cfg = configQ.data;
    const noAdminAPI = !cfg?.whmcs_admin_api_id || !cfg?.has_whmcs_admin_api_secret;
    return (
      <div className="space-y-6">
        <PageHeader
          productCount={0}
          selectedCount={cfg?.allowed_product_ids.length ?? 0}
          cachedAt={undefined}
          refresh={() => refresh.mutate()}
          refreshing={refresh.isPending}
        />
        <ConnectionPanel
          config={cfg}
          form={connectionForm}
          setForm={setConnectionForm}
          connected={false}
          error={
            noAdminAPI
              ? "WHMCS admin API credentials are required before products can be fetched."
              : String(productsQ.error)
          }
          saveConnection={() => saveConnection.mutate()}
          savingConnection={saveConnection.isPending}
          refresh={() => refresh.mutate()}
          refreshing={refresh.isPending}
        />
      </div>
    );
  }

  const products = productsQ.data?.products ?? [];
  const initialEnabled = configQ.data?.allowed_product_ids ?? [];
  const cachedAt = productsQ.data?.cached_at;

  return (
    <div className="space-y-6">
      <PageHeader
        productCount={products.length}
        selectedCount={initialEnabled.length}
        cachedAt={cachedAt}
        refresh={() => refresh.mutate()}
        refreshing={refresh.isPending}
      />

      <ConnectionPanel
        config={configQ.data}
        form={connectionForm}
        setForm={setConnectionForm}
        connected={true}
        saveConnection={() => saveConnection.mutate()}
        savingConnection={saveConnection.isPending}
        refresh={() => refresh.mutate()}
        refreshing={refresh.isPending}
      />

      <ProductSelector
        products={products}
        initialEnabled={initialEnabled}
        onSave={(csv) => save.mutate(csv)}
        saving={save.isPending}
      />
    </div>
  );
}

function PageHeader({
  productCount,
  selectedCount,
  cachedAt,
  refresh,
  refreshing,
}: {
  productCount: number;
  selectedCount: number;
  cachedAt?: string;
  refresh: () => void;
  refreshing: boolean;
}) {
  return (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">WHMCS Product Access</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Configure WHMCS login, fetch billing products, then choose which active
          products can sign in.
        </p>
      </div>
      <div className="flex items-center gap-2">
        <StatusPill label={`${productCount} fetched`} />
        <StatusPill label={`${selectedCount} selected`} />
        <Button
          variant="outline"
          size="sm"
          onClick={refresh}
          disabled={refreshing}
          title="Fetch the latest product list from WHMCS"
        >
          <RefreshCw className={refreshing ? "mr-2 size-4 animate-spin" : "mr-2 size-4"} />
          Connect
        </Button>
      </div>
      {cachedAt && (
        <p className="text-muted-foreground text-xs sm:hidden">
          Last fetched {new Date(cachedAt).toLocaleString()}
        </p>
      )}
    </div>
  );
}

function ConnectionPanel({
  config,
  form,
  setForm,
  connected,
  error,
  saveConnection,
  savingConnection,
  refresh,
  refreshing,
}: {
  config?: ConfigSummary;
  form: ConnectionFormState;
  setForm: React.Dispatch<React.SetStateAction<ConnectionFormState>>;
  connected: boolean;
  error?: string;
  saveConnection: () => void;
  savingConnection: boolean;
  refresh: () => void;
  refreshing: boolean;
}) {
  const oauthReady = !!config?.whmcs_server_url && !!config?.client_id && !!config?.has_client_secret;
  const apiReady = !!config?.whmcs_admin_api_id && !!config?.has_whmcs_admin_api_secret;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Server className="size-5" />
          WHMCS connection
        </CardTitle>
        <CardDescription>
          OAuth controls the login button. The admin API fetches WHMCS products for
          access rules.
        </CardDescription>
        <CardAction>
          <Button variant="outline" size="sm" onClick={refresh} disabled={refreshing || !apiReady}>
            <RefreshCw className={refreshing ? "mr-2 size-4 animate-spin" : "mr-2 size-4"} />
            Fetch products
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-3 md:grid-cols-3">
          <ReadinessItem
            icon={<Settings className="size-4" />}
            label="OAuth login"
            value={oauthReady ? "Configured" : "Needs setup"}
            ok={oauthReady}
          />
          <ReadinessItem
            icon={<ShieldCheck className="size-4" />}
            label="Admin API"
            value={apiReady ? "Configured" : "Needs API id and secret"}
            ok={apiReady}
          />
          <ReadinessItem
            icon={connected ? <CheckCircle2 className="size-4" /> : <XCircle className="size-4" />}
            label="Product fetch"
            value={connected ? "Connected" : "Not connected"}
            ok={connected}
          />
        </div>
        {error && (
          <div className="border-destructive/30 bg-destructive/10 text-destructive rounded-md border p-3 text-sm">
            {error} Enter the required connection values below, save, then fetch products.
          </div>
        )}
        <ConnectionFields
          config={config}
          form={form}
          setForm={setForm}
          onSave={saveConnection}
          saving={savingConnection}
        />
      </CardContent>
    </Card>
  );
}

function ConnectionFields({
  config,
  form,
  setForm,
  onSave,
  saving,
}: {
  config?: ConfigSummary;
  form: ConnectionFormState;
  setForm: React.Dispatch<React.SetStateAction<ConnectionFormState>>;
  onSave: () => void;
  saving: boolean;
}) {
  const update = (key: keyof ConnectionFormState) => (value: string) => {
    setForm((current) => ({ ...current, [key]: value }));
  };

  return (
    <div className="border-border/70 bg-background/50 rounded-md border p-4">
      <div className="grid gap-5 xl:grid-cols-2">
        <div className="space-y-3">
          <div>
            <h3 className="text-sm font-medium">WHMCS login</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Controls the public login button and OAuth redirect flow.
            </p>
          </div>
          <div className="grid gap-4">
            <Field label="WHMCS server URL">
              <Input
                value={form.whmcs_server_url}
                onChange={(e) => update("whmcs_server_url")(e.target.value)}
                placeholder="https://billing.example.com"
              />
            </Field>
            <Field label="Login button label">
              <Input
                value={form.display_name}
                onChange={(e) => update("display_name")(e.target.value)}
                placeholder="Sign in with WHMCS"
              />
            </Field>
            <Field label="OAuth client ID">
              <Input
                value={form.client_id}
                onChange={(e) => update("client_id")(e.target.value)}
                placeholder="WHMCS OAuth client ID"
              />
            </Field>
            <Field label="OAuth client secret" badge={config?.has_client_secret ? "Saved" : undefined}>
              <Input
                type="password"
                value={form.client_secret}
                onChange={(e) => update("client_secret")(e.target.value)}
                placeholder="Enter new secret"
              />
            </Field>
          </div>
        </div>

        <div className="space-y-3">
          <div>
            <h3 className="text-sm font-medium">Product access</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Used only to fetch billing products and evaluate product ownership.
            </p>
          </div>
          <div className="grid gap-4">
            <Field label="Admin API identifier">
              <Input
                value={form.whmcs_admin_api_id}
                onChange={(e) => update("whmcs_admin_api_id")(e.target.value)}
                placeholder="WHMCS admin API identifier"
              />
            </Field>
            <Field
              label="Admin API secret"
              badge={config?.has_whmcs_admin_api_secret ? "Saved" : undefined}
            >
              <Input
                type="password"
                value={form.whmcs_admin_api_secret}
                onChange={(e) => update("whmcs_admin_api_secret")(e.target.value)}
                placeholder="Enter new secret"
              />
            </Field>
          </div>
        </div>
      </div>
      <div className="mt-4 flex justify-end">
        <Button size="sm" onClick={onSave} disabled={saving}>
          {saving ? "Saving..." : "Save connection"}
        </Button>
      </div>
    </div>
  );
}

function Field({
  label,
  badge,
  children,
}: {
  label: string;
  badge?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <Label>{label}</Label>
        {badge && (
          <span className="border-border/70 bg-muted text-muted-foreground rounded-full border px-2 py-0.5 text-[11px] font-medium">
            {badge}
          </span>
        )}
      </div>
      {children}
    </div>
  );
}

function ReadinessItem({
  icon,
  label,
  value,
  ok,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  ok: boolean;
}) {
  return (
    <div className="border-border/70 rounded-md border p-3">
      <div className="text-muted-foreground flex items-center gap-2 text-xs">
        {icon}
        {label}
      </div>
      <div className={ok ? "mt-1 text-sm font-medium text-green-500" : "mt-1 text-sm font-medium"}>
        {value}
      </div>
    </div>
  );
}

function StatusPill({ label }: { label: string }) {
  return (
    <span className="border-border/70 bg-card text-muted-foreground hidden rounded-full border px-2.5 py-1 text-xs sm:inline-flex">
      {label}
    </span>
  );
}
