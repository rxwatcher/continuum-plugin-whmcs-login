import { mountPath, installID } from "./mountPath";
import { getCachedToken } from "./auth";

function authHeaders(): Record<string, string> {
  const t = getCachedToken();
  return t ? { Authorization: `Bearer ${t}` } : {};
}

async function jsonOrThrow<T>(r: Response): Promise<T> {
  if (!r.ok) throw new Error(`${r.status}: ${await r.text().catch(() => "")}`);
  if (r.status === 204) return undefined as T;
  return (await r.json()) as T;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    headers: {
      ...authHeaders(),
      ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
  };
  if (body !== undefined) init.body = JSON.stringify(body);
  return jsonOrThrow<T>(await fetch(mountPath() + path, init));
}

export const api = {
  get<T>(path: string): Promise<T> {
    return request<T>("GET", path);
  },
  post<T>(path: string, body?: unknown): Promise<T> {
    return request<T>("POST", path, body);
  },
};

// patchPluginConfig saves config changes back through continuum's own admin
// API. Continuum then re-runs Configure on the plugin with the new values.
// We do not proxy through the plugin — saving is a continuum-host concern.
export async function patchPluginConfig(
  id: string,
  entries: Record<string, { value: unknown }>,
): Promise<void> {
  const r = await fetch(`/api/v1/admin/plugins/${id}/config`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", ...authHeaders() },
    body: JSON.stringify({ entries }),
  });
  if (!r.ok) throw new Error(`${r.status}: ${await r.text().catch(() => "")}`);
}

export { installID };
