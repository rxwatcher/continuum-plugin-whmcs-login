import { useState, useMemo, useEffect } from "react";
import { Check, ChevronLeft, ChevronRight, Search } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";

export type Product = {
  pid: number;
  name: string;
  gid?: number;
  type?: string;
  paytype?: string;
  groupname?: string;
};

interface Props {
  products: Product[];
  initialEnabled: number[];
  onSave: (csv: string) => void;
  saving?: boolean;
}

// ProductSelector is the two-column shuttle widget used by the Products page.
// Behavior is a faithful port of librarymanagerre's product-selector-client.tsx:
//
//  - Left column lists products NOT currently enabled, filterable by search.
//  - Right column lists currently-enabled products.
//  - Click a row to move it to the other column (local state only).
//  - "Save Changes" emits the resulting comma-separated PID list to onSave.
//  - "Enable All" / "Disable All" do what they say.
//
// initialEnabled is honored on first render only; subsequent updates from
// upstream (e.g. after a successful save round-trip) flow through the
// useEffect below.
export default function ProductSelector({ products, initialEnabled, onSave, saving }: Props) {
  const [enabled, setEnabled] = useState<Set<number>>(() => new Set(initialEnabled));
  const [query, setQuery] = useState("");

  // Re-seed the enabled Set when initialEnabled changes (e.g. after a
  // save+refetch). We intentionally do this on identity change of the
  // array, not deep-equal, since react-query will reuse the array reference
  // when the value is unchanged.
  useEffect(() => {
    setEnabled(new Set(initialEnabled));
  }, [initialEnabled]);

  const available = useMemo(
    () =>
      products
        .filter((p) => !enabled.has(p.pid))
        .filter((p) => query === "" || p.name.toLowerCase().includes(query.toLowerCase())),
    [products, enabled, query],
  );
  const enabledList = useMemo(
    () => products.filter((p) => enabled.has(p.pid)),
    [products, enabled],
  );

  const toggle = (p: Product) => {
    setEnabled((prev) => {
      const next = new Set(prev);
      if (next.has(p.pid)) next.delete(p.pid);
      else next.add(p.pid);
      return next;
    });
  };
  const enableAll = () => setEnabled(new Set(products.map((p) => p.pid)));
  const disableAll = () => setEnabled(new Set());
  const enabledIDs = useMemo(() => Array.from(enabled).sort((a, b) => a - b), [enabled]);
  const save = () => onSave(enabledIDs.join(","));

  return (
    <div className="border-border/70 bg-card text-card-foreground rounded-lg border">
      <div className="flex flex-col gap-3 p-6 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h2 className="text-lg font-semibold">Select allowed WHMCS products</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            A user must own at least one selected active product to sign in. Leave the
            selection empty only when every WHMCS OAuth user should be allowed.
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button variant="outline" size="sm" onClick={enableAll}>
            Select all
          </Button>
          <Button variant="outline" size="sm" onClick={disableAll}>
            Clear
          </Button>
        </div>
      </div>

      <Separator />

      <div className="grid gap-4 p-6 lg:grid-cols-2">
        <Column
          title="Available products"
          products={available}
          isSelected={false}
          count={`${available.length} products`}
          onClick={toggle}
          searchBox={
            <div className="relative">
              <Search className="text-muted-foreground absolute left-2 top-2.5 size-4" />
              <Input
                placeholder="Search products..."
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                className="pl-8"
              />
            </div>
          }
        />
        <Column
          title="Allowed products"
          products={enabledList}
          isSelected={true}
          count={`${enabledList.length} products`}
          onClick={toggle}
        />
      </div>

      <Separator />

      <div className="flex flex-col gap-3 p-6 sm:flex-row sm:items-center sm:justify-between">
        <SelectionSummary enabledIDs={enabledIDs} />
        <Button onClick={save} size="sm" disabled={saving}>
          <Check className="mr-2 size-4" />
          Save product access
        </Button>
      </div>
    </div>
  );
}

function Column({
  title,
  products,
  isSelected,
  count,
  onClick,
  searchBox,
}: {
  title: string;
  products: Product[];
  isSelected: boolean;
  count: string;
  onClick: (p: Product) => void;
  searchBox?: React.ReactNode;
}) {
  return (
    <div className="flex flex-1 flex-col">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-sm font-medium">{title}</h3>
        <span className="text-muted-foreground text-xs">{count}</span>
      </div>
      {searchBox && <div className="mb-2">{searchBox}</div>}
      <div className="bg-background flex-1 rounded-md border">
        <ScrollArea className="h-[420px]">
          <div className="space-y-1 p-2">
            {products.map((p) => (
              <div
                key={p.pid}
                onClick={() => onClick(p)}
                className={cn(
                  "group flex cursor-pointer items-start gap-3 rounded-md p-3 text-sm transition-colors",
                  "hover:bg-accent hover:text-accent-foreground",
                  isSelected && "bg-primary/5",
                )}
              >
                <div className="min-w-0 flex-1 space-y-1">
                  <div className="flex items-start justify-between gap-2">
                    <span className="break-words font-medium">{p.name}</span>
                    <span className="text-muted-foreground shrink-0 font-mono text-xs">
                      PID {p.pid}
                    </span>
                  </div>
                  <ProductMeta product={p} />
                </div>
                <span className="opacity-0 transition-opacity group-hover:opacity-100">
                  {isSelected ? (
                    <ChevronLeft className="size-4" />
                  ) : (
                    <ChevronRight className="size-4" />
                  )}
                </span>
              </div>
            ))}
            {products.length === 0 && (
              <div className="text-muted-foreground p-2 text-center text-sm">
                {isSelected ? "No products" : "No matching products"}
              </div>
            )}
          </div>
        </ScrollArea>
      </div>
    </div>
  );
}

function ProductMeta({ product }: { product: Product }) {
  const details = [
    product.groupname,
    product.type,
    product.paytype,
    product.gid ? `Group ${product.gid}` : "",
  ].filter(Boolean);

  if (details.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-wrap gap-1.5">
      {details.map((detail) => (
        <span
          key={detail}
          className="bg-muted text-muted-foreground rounded-sm px-1.5 py-0.5 text-[11px]"
        >
          {detail}
        </span>
      ))}
    </div>
  );
}

function SelectionSummary({ enabledIDs }: { enabledIDs: number[] }) {
  if (enabledIDs.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        No products selected. Current backend behavior allows any WHMCS OAuth account.
      </p>
    );
  }

  return (
    <p className="text-muted-foreground text-sm">
      Saving {enabledIDs.length} selected product{enabledIDs.length === 1 ? "" : "s"}:
      <span className="ml-1 font-mono">{enabledIDs.join(", ")}</span>
    </p>
  );
}
