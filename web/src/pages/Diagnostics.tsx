import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  CheckCircle2,
  XCircle,
  AlertCircle,
  Loader2,
  Mail,
  Hash,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";

type ProductRow = {
  pid: number;
  name: string;
  status: string;
  active: boolean;
  allowed: boolean;
  role_hit?: string;
};

type SimulateResponse = {
  ok: boolean;
  reason?: string;
  error?: string;
  allowed?: boolean;
  role?: "user" | "admin";
  client_id?: string;
  products?: ProductRow[];
  owned_active?: string[];
  gating?: {
    required: boolean;
    allow_set: string[];
    passed: boolean;
  };
  role_mapping_count?: number;
  resolved_by_email?: boolean;
  resolved_email?: string;
  client_details?: {
    id?: string;
    email?: string;
    first_name?: string;
    last_name?: string;
  };
  client_details_error?: string;
  discord_id?: string;
};

type Mode = "email" | "client_id";

// Diagnostics is the "simulate-login" page. It POSTs to
// /api/v1/admin/simulate-login with either an email or a client ID and
// renders the entitlement evaluation that ExchangeCode would run during a
// real OAuth callback — without actually completing the OAuth dance.
export default function Diagnostics() {
  const [mode, setMode] = useState<Mode>("email");
  const [emailInput, setEmailInput] = useState("");
  const [clientIDInput, setClientIDInput] = useState("");
  const [result, setResult] = useState<SimulateResponse | null>(null);

  const sim = useMutation({
    mutationFn: async () => {
      const body =
        mode === "email"
          ? { email: emailInput.trim() }
          : { client_id: clientIDInput.trim() };
      return api.post<SimulateResponse>("/api/v1/admin/simulate-login", body);
    },
    onSuccess: (r) => setResult(r),
    onError: (e: Error) =>
      setResult({ ok: false, error: e.message, reason: "request_failed" }),
  });

  const disabled =
    sim.isPending ||
    (mode === "email" ? !emailInput.trim() : !clientIDInput.trim());

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Simulate sign-in</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-muted-foreground text-sm">
            Replay the entitlement evaluation that runs after a WHMCS OAuth
            callback, for any client by email or WHMCS client ID. The product
            gate, role mapping, and Discord-ID lookup all run exactly as they
            would during a real login. The check uses your <em>saved</em>{" "}
            configuration — save changes in Products / Settings first to
            preview them here.
          </p>

          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant={mode === "email" ? "default" : "outline"}
              size="sm"
              onClick={() => setMode("email")}
            >
              <Mail className="mr-2 size-4" />
              By email
            </Button>
            <Button
              type="button"
              variant={mode === "client_id" ? "default" : "outline"}
              size="sm"
              onClick={() => setMode("client_id")}
            >
              <Hash className="mr-2 size-4" />
              By WHMCS client ID
            </Button>
          </div>

          {mode === "email" ? (
            <div className="space-y-2">
              <Label htmlFor="sim-email">WHMCS client email</Label>
              <Input
                id="sim-email"
                type="email"
                placeholder="ada@example.com"
                value={emailInput}
                onChange={(e) => setEmailInput(e.target.value)}
              />
            </div>
          ) : (
            <div className="space-y-2">
              <Label htmlFor="sim-cid">WHMCS client ID</Label>
              <Input
                id="sim-cid"
                inputMode="numeric"
                placeholder="42"
                value={clientIDInput}
                onChange={(e) => setClientIDInput(e.target.value)}
              />
            </div>
          )}

          <Button onClick={() => sim.mutate()} disabled={disabled}>
            {sim.isPending && (
              <Loader2 className="mr-2 size-4 animate-spin" />
            )}
            Simulate
          </Button>

          <div className="text-muted-foreground border-border/40 border-t pt-3 text-xs">
            <strong>Note:</strong> entitlements are evaluated only at sign-in.
            If you remove a product from the allow-list, users with active
            Silo sessions keep their access until they sign in again.
          </div>
        </CardContent>
      </Card>

      {result && <SimResult result={result} />}
    </div>
  );
}

function SimResult({ result }: { result: SimulateResponse }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Result</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <Verdict r={result} />
        {result.client_details && (
          <ClientBlock
            details={result.client_details}
            byEmail={result.resolved_by_email}
            discord={result.discord_id}
          />
        )}
        {result.client_details_error && (
          <div className="text-muted-foreground flex items-start gap-2 text-xs">
            <AlertCircle className="mt-0.5 size-4 shrink-0" />
            <span>
              Couldn't fetch client details ({result.client_details_error}).
              Gating / role evaluation still ran on the products list.
            </span>
          </div>
        )}
        {result.gating && (
          <GatingBlock
            gating={result.gating}
            ownedActive={result.owned_active ?? []}
          />
        )}
        {result.products && result.products.length > 0 && (
          <ProductsTable rows={result.products} />
        )}
        {result.ok && (
          <div className="text-muted-foreground text-xs">
            Role mapping rules evaluated: {result.role_mapping_count ?? 0}.
            Resulting Silo role:{" "}
            <span className="font-mono">{result.role ?? "user"}</span>.
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Verdict({ r }: { r: SimulateResponse }) {
  if (!r.ok) {
    const label =
      r.reason === "admin_api_required"
        ? "Admin API credentials required"
        : r.reason === "client_not_found"
          ? "No WHMCS client found"
          : r.reason === "client_lookup_failed"
            ? "Couldn't reach WHMCS"
            : r.reason === "products_lookup_failed"
              ? "Couldn't fetch products"
              : r.reason === "request_failed"
                ? "Simulator request failed"
                : "Simulation failed";
    return (
      <div className="text-destructive flex items-start gap-2">
        <XCircle className="mt-0.5 size-5 shrink-0" />
        <div>
          <div className="font-medium">{label}</div>
          {r.error && (
            <div className="text-muted-foreground mt-0.5 text-xs">
              {r.error}
            </div>
          )}
        </div>
      </div>
    );
  }
  if (r.allowed) {
    return (
      <div className="flex items-start gap-2 text-green-500">
        <CheckCircle2 className="mt-0.5 size-5 shrink-0" />
        <div>
          <div className="font-medium">Would sign in</div>
          <div className="text-muted-foreground mt-0.5 text-xs">
            Assigned role:{" "}
            <span className="font-mono">{r.role ?? "user"}</span>
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="text-destructive flex items-start gap-2">
      <XCircle className="mt-0.5 size-5 shrink-0" />
      <div>
        <div className="font-medium">Would NOT sign in</div>
        <div className="text-muted-foreground mt-0.5 text-xs">
          No active WHMCS product matches the allowed-products list.
        </div>
      </div>
    </div>
  );
}

function ClientBlock({
  details,
  byEmail,
  discord,
}: {
  details: NonNullable<SimulateResponse["client_details"]>;
  byEmail?: boolean;
  discord?: string;
}) {
  const fullName = [details.first_name, details.last_name]
    .filter(Boolean)
    .join(" ");
  return (
    <div className="border-border/40 rounded-md border p-3">
      <div className="text-muted-foreground text-xs">
        Resolved WHMCS client {byEmail ? "(by email)" : "(by id)"}
      </div>
      <div className="mt-1 grid grid-cols-1 gap-1 sm:grid-cols-2">
        <Pair label="id" value={details.id} />
        <Pair label="email" value={details.email} />
        {fullName && <Pair label="name" value={fullName} />}
        {discord && <Pair label="discord id" value={discord} />}
      </div>
    </div>
  );
}

function Pair({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return (
    <div className="text-xs">
      <span className="text-muted-foreground">{label}: </span>
      <span className="font-mono break-all">{value}</span>
    </div>
  );
}

function GatingBlock({
  gating,
  ownedActive,
}: {
  gating: NonNullable<SimulateResponse["gating"]>;
  ownedActive: string[];
}) {
  if (!gating.required) {
    return (
      <div className="text-muted-foreground text-xs">
        Product gate: <span className="font-mono">disabled</span> — any WHMCS
        client with active products would be allowed. ({ownedActive.length}{" "}
        owned active)
      </div>
    );
  }
  return (
    <div className="text-xs">
      Product gate: {gating.passed ? (
        <span className="text-green-500">pass</span>
      ) : (
        <span className="text-destructive">fail</span>
      )}{" "}
      — allow-list ({gating.allow_set.join(", ") || "—"}) vs. owned active (
      {ownedActive.join(", ") || "—"})
    </div>
  );
}

function ProductsTable({ rows }: { rows: ProductRow[] }) {
  return (
    <div className="space-y-1">
      <div className="text-muted-foreground text-xs">Client products</div>
      <table className="w-full text-xs">
        <thead>
          <tr className="text-muted-foreground border-b text-left">
            <th className="w-16 py-1 pr-2">PID</th>
            <th className="py-1 pr-2">Name</th>
            <th className="w-24 py-1 pr-2">Status</th>
            <th className="w-16 py-1 pr-2">Active</th>
            <th className="w-16 py-1 pr-2">Allowed</th>
            <th className="w-20 py-1 pr-2">Role rule</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((p) => (
            <tr
              key={p.pid}
              className="border-border/30 border-b last:border-b-0"
            >
              <td className="py-1 pr-2 font-mono">{p.pid}</td>
              <td className="py-1 pr-2">{p.name}</td>
              <td className="py-1 pr-2">{p.status}</td>
              <td className="py-1 pr-2">
                {p.active ? (
                  <CheckCircle2 className="size-4 text-green-500" />
                ) : (
                  <XCircle className="text-muted-foreground size-4" />
                )}
              </td>
              <td className="py-1 pr-2">
                {p.allowed ? (
                  <CheckCircle2 className="size-4 text-green-500" />
                ) : (
                  <XCircle className="text-muted-foreground size-4" />
                )}
              </td>
              <td className="py-1 pr-2 font-mono">{p.role_hit || ""}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
