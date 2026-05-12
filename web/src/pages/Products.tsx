import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api, patchPluginConfig, installID } from "@/lib/api";
import ProductSelector, { type Product } from "@/components/ProductSelector";

type ProductsResponse = { products: Product[]; cached_at: string };
type ConfigSummary = {
  allowed_product_ids: number[];
  whmcs_admin_api_id: string;
  has_whmcs_admin_api_secret: boolean;
};

export default function Products() {
  const qc = useQueryClient();

  const productsQ = useQuery({
    queryKey: ["products"],
    queryFn: () => api.get<ProductsResponse>("/api/v1/admin/products"),
  });
  const configQ = useQuery({
    queryKey: ["config-summary"],
    queryFn: () => api.get<ConfigSummary>("/api/v1/admin/config-summary"),
  });

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

  if (productsQ.isLoading || configQ.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-1/3" />
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
      <div className="space-y-4">
        <h1 className="text-2xl font-semibold tracking-tight">Allowed Products</h1>
        <div className="border-border/70 bg-card text-card-foreground space-y-2 rounded-2xl border p-6">
          <p className="text-muted-foreground text-sm">
            {noAdminAPI
              ? "Configure the WHMCS admin API credentials in Settings to see the product list."
              : `Failed to load products: ${String(productsQ.error)}`}
          </p>
        </div>
      </div>
    );
  }

  const products = productsQ.data?.products ?? [];
  const initialEnabled = configQ.data?.allowed_product_ids ?? [];
  const cachedAt = productsQ.data?.cached_at;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Allowed Products</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Cached at: {cachedAt ? new Date(cachedAt).toLocaleString() : "—"}
          </p>
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => refresh.mutate()}
          disabled={refresh.isPending}
          title="Refresh products from WHMCS"
        >
          <RefreshCw className={refresh.isPending ? "animate-spin" : ""} />
        </Button>
      </div>

      <ProductSelector
        products={products}
        initialEnabled={initialEnabled}
        onSave={(csv) => save.mutate(csv)}
        saving={save.isPending}
      />
    </div>
  );
}
