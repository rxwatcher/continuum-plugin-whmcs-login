import { describe, expect, it } from "vitest";

import { isProductFetchConnected } from "./Products";

describe("isProductFetchConnected", () => {
  it("does not report connected when the product endpoint says it is not configured", () => {
    expect(isProductFetchConnected(true, { products: [], cached_at: "", configured: false })).toBe(false);
  });

  it("does not report connected when admin API credentials are missing", () => {
    expect(isProductFetchConnected(false, { products: [], cached_at: "" })).toBe(false);
  });

  it("reports connected when admin API credentials exist and products endpoint is configured", () => {
    expect(isProductFetchConnected(true, { products: [], cached_at: "", configured: true })).toBe(true);
  });
});
