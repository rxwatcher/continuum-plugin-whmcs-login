import { useState, useMemo, useEffect } from "react";
import { ChevronLeft, ChevronRight, Search } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
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
  const save = () => onSave(Array.from(enabled).sort((a, b) => a - b).join(","));

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium">Product Access Control</h2>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={enableAll}>
            Enable All
          </Button>
          <Button variant="outline" size="sm" onClick={disableAll}>
            Disable All
          </Button>
        </div>
      </div>

      <div className="flex gap-4">
        <Column
          title="Available Products"
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
          title="Enabled Products"
          products={enabledList}
          isSelected={true}
          count={`${enabledList.length} products`}
          onClick={toggle}
        />
      </div>

      <div className="flex items-center justify-end gap-3">
        <p className="text-muted-foreground text-xs">
          If no products are enabled, all products are allowed.
        </p>
        <Button onClick={save} size="sm" disabled={saving}>
          Save Changes
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
      <div className="flex-1 rounded-md border">
        <ScrollArea className="h-[300px]">
          <div className="space-y-1 p-2">
            {products.map((p) => (
              <div
                key={p.pid}
                onClick={() => onClick(p)}
                className={cn(
                  "group flex cursor-pointer items-center gap-2 rounded-md p-2 text-sm transition-colors",
                  "hover:bg-accent hover:text-accent-foreground",
                )}
              >
                <span className="flex-1">{p.name}</span>
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
