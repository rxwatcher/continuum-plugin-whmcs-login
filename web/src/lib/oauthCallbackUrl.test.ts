import { describe, expect, it } from "vitest";
import { oauthCallbackUrl } from "./oauthCallbackUrl";

describe("oauthCallbackUrl", () => {
  it("builds the host OAuth callback URL from the plugin install URL", () => {
    expect(oauthCallbackUrl("https://ct.wave-ninja.eu", "/api/v1/plugins/34/admin/products")).toBe(
      "https://ct.wave-ninja.eu/api/v1/auth/oauth/34/callback",
    );
  });

  it("keeps localhost and non-numeric installation ids working", () => {
    expect(oauthCallbackUrl("http://localhost:8090/", "/api/v1/plugins/whmcs-dev/admin")).toBe(
      "http://localhost:8090/api/v1/auth/oauth/whmcs-dev/callback",
    );
  });
});
