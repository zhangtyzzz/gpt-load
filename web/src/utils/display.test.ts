import { describe, expect, it } from "vitest";

import { KEY_MASK_MARKER, maskKey, maskProxyKeys } from "./display";

describe("maskKey", () => {
  it("keeps the first and last four characters around the mask marker", () => {
    expect(maskKey("sk-abcdefghijklmnop")).toBe("sk-a****mnop");
  });

  it("uses the marker the backend shares, so a request-log row and a key row render alike", () => {
    // The backend masks the request-log key identifier with utils.MaskKeyIdentifier.
    // Both sides must produce the same string for the same key, otherwise the two
    // screens cannot be compared by eye.
    expect(KEY_MASK_MARKER).toBe("****");
    expect(maskKey("sk-live-abcdefghijklmnop")).toBe("sk-l****mnop");
  });

  it("never returns a short key verbatim", () => {
    for (const key of ["a", "sk-12345", "12345678"]) {
      expect(maskKey(key)).toBe(KEY_MASK_MARKER);
      expect(maskKey(key)).not.toBe(key);
    }
  });

  it("returns an empty string for an absent key", () => {
    expect(maskKey("")).toBe("");
  });

  it("does not reveal the middle of a key", () => {
    expect(maskKey("sk-aSUPERSECRETMIDDLEmnop")).not.toContain("SUPERSECRETMIDDLE");
  });
});

describe("maskProxyKeys", () => {
  it("masks each key in a comma-separated list", () => {
    expect(maskProxyKeys("sk-abcdefghijklmnop, sk-qrstuvwxyz012345")).toBe(
      "sk-a****mnop, sk-q****2345"
    );
  });

  it("returns an empty string for empty input", () => {
    expect(maskProxyKeys("")).toBe("");
  });
});
