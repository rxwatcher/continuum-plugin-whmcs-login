export function oauthCallbackUrl(origin: string, pathname: string): string {
  const cleanOrigin = origin.replace(/\/+$/, "");
  const match = pathname.match(/^\/api\/v1\/plugins\/([^/]+)/);
  const installationID = match?.[1] || "{installation_id}";
  return `${cleanOrigin}/api/v1/auth/oauth/${installationID}/callback`;
}

export function currentOAuthCallbackUrl(): string {
  return oauthCallbackUrl(window.location.origin, window.location.pathname);
}
