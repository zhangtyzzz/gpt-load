import { describe, expect, it } from "vitest";
import { generateSecureRandomString } from "./secure-random";

describe("generateSecureRandomString", () => {
  it("uses the URL-safe 64-character alphabet at the requested length", () => {
    const token = generateSecureRandomString(48);

    expect(token).toHaveLength(48);
    expect(token).toMatch(/^[A-Za-z0-9_-]+$/);
  });

  it("rejects invalid lengths", () => {
    expect(() => generateSecureRandomString(0)).toThrow(RangeError);
    expect(() => generateSecureRandomString(1.5)).toThrow(RangeError);
  });
});
