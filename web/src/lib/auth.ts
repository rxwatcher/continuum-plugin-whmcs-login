// Continuum's plugin proxy authenticates each request via a Bearer token or
// ?token= query param. The SPA receives the token on its initial load via
// URL ?token= (set by the sidebar / continuum admin link). We capture it
// once into memory for use on all subsequent fetches.
//
// Theme is also captured here so semantic Tailwind classes pick up the
// active continuum theme.

let cachedToken: string | null = null;
let cachedTheme: string | null = null;

export function captureFromURL(params: URLSearchParams): void {
  const t = params.get("token");
  if (t) cachedToken = t;

  const th = params.get("theme") ?? sessionStorage.getItem("continuum-theme");
  if (th) {
    cachedTheme = th;
    sessionStorage.setItem("continuum-theme", th);
  }
}

export function getCachedToken(): string | null {
  return cachedToken;
}

export function getCachedTheme(): string | null {
  return cachedTheme;
}

// Exposed for tests only.
export function _resetForTest(): void {
  cachedToken = null;
  cachedTheme = null;
}
