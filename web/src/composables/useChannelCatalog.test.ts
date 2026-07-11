import { describe, expect, it } from "vitest";
import { normalizeChannelCatalog, normalizeGenericHttpConfig } from "./useChannelCatalog";

function preset(id: string, integrationKind?: string) {
  return {
    id,
    channel_type: "generic-http",
    display_name: id,
    description: `${id} preset`,
    upstreams: [{ url: `https://${id}.example`, weight: 1 }],
    integration_kind: integrationKind,
    suggested_path: integrationKind === "hosted_mcp" ? "/mcp" : "",
    channel_config: {
      version: 1,
      preset_id: id,
      auth: { placement: "header", name: "Authorization", prefix: "Bearer " },
      validation: {
        enabled: true,
        base_url: "",
        method: "GET",
        path: "/usage",
        headers: {},
        body: null,
        valid_statuses: [200],
        invalid_statuses: [401],
      },
      stream_mode: "auto",
      retry: { safe_methods: ["GET", "HEAD"], failover_statuses: [] },
      max_request_body_bytes: 16 * 1024 * 1024,
      max_error_body_bytes: 64 * 1024,
    },
  };
}

describe("channel catalog normalization", () => {
  it("merges backend presets and keeps Generic HTTP aggregate-capable", () => {
    const catalog = normalizeChannelCatalog(
      [preset("tavily-http"), preset("tavily-mcp", "hosted_mcp")],
      ["openai", "anthropic", "generic-http", "future-provider"]
    );

    expect(catalog.items.map(item => item.id)).toEqual([
      "openai",
      "anthropic",
      "generic-http",
      "future-provider",
    ]);
    const generic = catalog.items.find(item => item.id === "generic-http");
    expect(generic?.capabilities.test_model).toBe("hidden");
    expect(generic?.capabilities.aggregate).toBe(true);
    expect(generic?.presets.map(item => item.integration_kind)).toEqual([undefined, "hosted_mcp"]);
  });

  it("writes a complete valid custom config for a new Generic HTTP group", () => {
    expect(normalizeGenericHttpConfig(undefined)).toEqual({
      version: 1,
      preset_id: "custom",
      auth: { placement: "header", name: "Authorization", prefix: "Bearer " },
      validation: {
        enabled: false,
        base_url: "",
        method: "",
        path: "",
        headers: {},
        body: null,
        valid_statuses: [],
        invalid_statuses: [],
      },
      stream_mode: "auto",
      retry: { safe_methods: ["GET", "HEAD"], failover_statuses: [] },
      max_request_body_bytes: 16 * 1024 * 1024,
      max_error_body_bytes: 64 * 1024,
    });
  });

  it("drops deprecated protocol fields while preserving future current-contract fields", () => {
    const config = normalizeGenericHttpConfig({
      version: 9,
      preset_id: "future",
      protocol: "mcp_streamable_http",
      mcp_session_ttl_seconds: 99999999,
      future_top_level: { enabled: true },
      auth: {
        placement: "header",
        name: "x-api-key",
        prefix: "",
        future_auth_field: "keep-me",
      },
      validation: {
        enabled: true,
        base_url: "https://example.com",
        method: "POST",
        path: "/check",
        headers: {},
        body: { ping: true },
        valid_statuses: [200, 202],
        invalid_statuses: [401],
      },
      retry: {
        safe_methods: ["get", "HEAD"],
        safe_statuses_for_any_method: [429],
        failover_statuses: [200, 429],
      },
      max_request_body_bytes: 999999999,
      max_error_body_bytes: 999999999,
    });

    expect(config).not.toHaveProperty("protocol");
    expect(config).not.toHaveProperty("mcp_session_ttl_seconds");
    expect(config.retry).not.toHaveProperty("safe_statuses_for_any_method");
    expect(config.retry.safe_methods).toEqual(["GET", "HEAD"]);
    expect(config.retry.failover_statuses).toEqual([429]);
    expect(config.max_request_body_bytes).toBe(64 * 1024 * 1024);
    expect(config.max_error_body_bytes).toBe(1024 * 1024);
    expect(config.future_top_level).toEqual({ enabled: true });
    expect(config.auth.future_auth_field).toBe("keep-me");
  });

  it("migrates legacy query authentication to header-only injection", () => {
    const config = normalizeGenericHttpConfig({
      auth: { placement: "query", name: "api_key", prefix: "" },
    });

    expect(config.auth).toMatchObject({ placement: "header", name: "api_key", prefix: "" });
  });

  it("keeps legacy Gemini validation-path capability hidden", () => {
    const catalog = normalizeChannelCatalog([], ["gemini"]);
    expect(catalog.items.find(item => item.id === "gemini")?.capabilities.validation_endpoint).toBe(
      false
    );
  });
});
