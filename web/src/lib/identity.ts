import { api } from "./api";

export type Identity = {
  user_id: string;
  role: string;
  theme: string;
  isAdmin: boolean;
};

let cached: Identity | null = null;

export async function loadIdentity(): Promise<Identity> {
  if (cached) return cached;
  const me = await api.get<{ user_id: string; role: string; theme: string }>(
    "/api/v1/admin/whoami",
  );
  cached = { ...me, isAdmin: me.role === "admin" };
  return cached;
}

export function currentUser(): Identity | null {
  return cached;
}

export function _resetIdentityForTest(): void {
  cached = null;
}
