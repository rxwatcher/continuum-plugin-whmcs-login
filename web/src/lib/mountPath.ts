// The plugin SPA is served under /api/v1/plugins/{installationId}/admin/...
// {installationId} is not known at build time, so we derive it at runtime
// from window.location.pathname. Returns the empty string when the SPA is
// served outside the plugin proxy (e.g. local Vite dev server).
export function extractMountPath(pathname: string): string {
  const m = pathname.match(/^(\/api\/v1\/plugins\/[^/]+)/);
  return m ? m[1] : "";
}

export function mountPath(): string {
  return extractMountPath(window.location.pathname);
}
