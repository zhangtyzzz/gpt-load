import { describe, expect, it } from "vitest";
import {
  areValidHttpUpstreams,
  channelConfigsExactlyMatch,
  isReservedProxyHeaderName,
  sanitizeChannelSpecificFields,
} from "./channel-form";

describe("sanitizeChannelSpecificFields", () => {
  it("clears hidden model and parameter behavior when switching to Generic HTTP", () => {
    const result = sanitizeChannelSpecificFields(
      { model_redirect: false, param_overrides: false },
      {
        param_overrides: { temperature: 0.7 },
        model_redirect_rules: { "gpt-old": "gpt-new" },
        model_redirect_strict: true,
      }
    );

    expect(result).toEqual({
      param_overrides: {},
      model_redirect_rules: {},
      model_redirect_strict: false,
    });
  });

  it("preserves supported channel behavior", () => {
    const fields = {
      param_overrides: { temperature: 0.7 },
      model_redirect_rules: { "gpt-old": "gpt-new" },
      model_redirect_strict: true,
    };

    expect(
      sanitizeChannelSpecificFields({ model_redirect: true, param_overrides: true }, fields)
    ).toEqual(fields);
  });

  it("validates every Generic HTTP upstream instead of only the first", () => {
    expect(
      areValidHttpUpstreams([
        { url: "https://valid.example/api", weight: 1 },
        { url: "https://user:secret@invalid.example", weight: 1 },
      ])
    ).toBe(false);
    expect(
      areValidHttpUpstreams([
        { url: "https://one.example", weight: 1 },
        { url: "http://two.example/path", weight: 2 },
      ])
    ).toBe(true);
    expect(areValidHttpUpstreams([{ url: "https:opaque.example", weight: 1 }])).toBe(false);
    expect(areValidHttpUpstreams([{ url: "https://valid.example/api?region=us", weight: 1 }])).toBe(
      false
    );
  });

  it("compares aggregate child configurations exactly regardless of object key order", () => {
    expect(
      channelConfigsExactlyMatch(
        { preset_id: "a", nested: { b: 2, a: 1 } },
        {
          nested: { a: 1, b: 2 },
          preset_id: "a",
        }
      )
    ).toBe(true);
    expect(channelConfigsExactlyMatch({ preset_id: "a" }, { preset_id: "custom" })).toBe(false);
  });

  it("keeps control-plane and hop-by-hop headers out of upstream configuration", () => {
    expect(isReservedProxyHeaderName("X-Gpt-Load-Key")).toBe(true);
    expect(isReservedProxyHeaderName("x-gpt-load-key")).toBe(true);
    expect(isReservedProxyHeaderName("Proxy-Connection")).toBe(true);
  });
});
