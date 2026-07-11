import type { ChannelPresetDescriptor, GenericHttpChannelConfig } from "@/types/channels";
import enUS from "@/locales/en-US";
import jaJP from "@/locales/ja-JP";
import zhCN from "@/locales/zh-CN";
import { NInput, NSelect, type SelectGroupOption, type SelectOption } from "naive-ui";
import { config as testUtilsConfig, shallowMount, type VueWrapper } from "@vue/test-utils";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const { warning, success, error } = vi.hoisted(() => ({
  warning: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}));

vi.mock("vue-i18n", async importOriginal => {
  const actual = await importOriginal<typeof import("vue-i18n")>();
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

vi.mock("@/utils/clipboard", () => ({ copy: vi.fn(async () => true) }));

vi.mock("naive-ui", async importOriginal => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useDialog: () => ({ warning }),
    useMessage: () => ({ success, error }),
  };
});

import GenericHttpChannelFields from "./GenericHttpChannelFields.vue";
import genericHttpSource from "./GenericHttpChannelFields.vue?raw";

function lastEmission(wrapper: VueWrapper, event: string): unknown[] | undefined {
  const emissions = wrapper.emitted(event);
  return emissions?.[emissions.length - 1];
}

function channelConfig(presetId = "custom"): GenericHttpChannelConfig {
  return {
    version: 1,
    preset_id: presetId,
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
  };
}

function preset(id: string, integrationKind?: string): ChannelPresetDescriptor {
  const config = channelConfig(id);
  return {
    id,
    channel_type: "generic-http",
    display_name: id,
    description: `${id} description`,
    upstreams: [
      { url: `https://${id}-a.example`, weight: 2 },
      { url: `https://${id}-b.example`, weight: 1 },
    ],
    integration_kind: integrationKind,
    suggested_path: integrationKind === "hosted_mcp" ? "/mcp" : "",
    channel_config: config,
  };
}

function mountFields(options: {
  config?: GenericHttpChannelConfig;
  presets?: ChannelPresetDescriptor[];
  upstreams?: Array<{ url: string; weight: number }>;
  catalogLoading?: boolean;
}) {
  return shallowMount(GenericHttpChannelFields, {
    props: {
      modelValue: options.config || channelConfig(),
      presets: options.presets || [],
      upstreams: options.upstreams || [{ url: "https://api.example.com", weight: 1 }],
      groupName: "search",
      catalogLoading: options.catalogLoading || false,
    },
  });
}

function findPresetSelect(wrapper: VueWrapper) {
  return wrapper
    .findAllComponents(NSelect)
    .find(component => component.attributes("data-testid") === "preset-select");
}

function flattenedOptions(groups: SelectGroupOption[]): SelectOption[] {
  return groups.flatMap(group => group.children || []);
}

describe("GenericHttpChannelFields", () => {
  beforeAll(() => {
    testUtilsConfig.global.renderStubDefaultSlot = true;
  });

  afterAll(() => {
    testUtilsConfig.global.renderStubDefaultSlot = false;
  });

  beforeEach(() => {
    warning.mockReset();
    success.mockReset();
    error.mockReset();
  });

  it("shows Hosted MCP client guidance only for catalog integration metadata", () => {
    const mcp = preset("tavily-mcp", "hosted_mcp");
    const wrapper = mountFields({ config: mcp.channel_config, presets: [mcp] });

    expect(wrapper.find(".integration-card").exists()).toBe(true);
    expect(wrapper.find(".integration-code").text()).toContain("GPT_LOAD_PROXY_KEY");

    const plain = preset("tavily-http");
    const plainWrapper = mountFields({ config: plain.channel_config, presets: [plain] });
    expect(plainWrapper.find(".integration-card").exists()).toBe(false);
  });

  it("states the stateless Hosted MCP boundary and simple multi-key setup in every locale", () => {
    expect(zhCN.channels.integration.mcpDescription).toContain("stateless");
    expect(zhCN.channels.integration.mcpDescription).toContain("当前普通分组");
    expect(enUS.channels.integration.mcpDescription).toContain("stateless");
    expect(enUS.channels.integration.mcpDescription).toContain("standard group");
    expect(jaJP.channels.integration.mcpDescription).toContain("stateless");
    expect(jaJP.channels.integration.mcpDescription).toContain("通常グループ");
  });

  it("groups presets into searchable HTTP, Hosted MCP, and Custom choices", () => {
    const wrapper = mountFields({
      presets: [preset("exa-http"), preset("tavily-mcp", "hosted_mcp")],
    });
    const select = findPresetSelect(wrapper);
    const groups = select?.props("options") as SelectGroupOption[];

    expect(groups.map(group => group.label)).toEqual([
      "channels.presetGroups.httpApi",
      "channels.presetGroups.hostedMcp",
      "channels.presetGroups.custom",
    ]);
    expect(flattenedOptions(groups).map(option => option.value)).toEqual([
      "exa-http",
      "tavily-mcp",
      "custom",
    ]);

    const filter = select?.props("filter") as (pattern: string, option: SelectOption) => boolean;
    const exa = flattenedOptions(groups).find(
      option => option.value === "exa-http"
    ) as SelectOption;
    expect(filter("EXA", exa)).toBe(true);
    expect(filter("description", exa)).toBe(true);
    expect(filter("missing", exa)).toBe(false);
  });

  it("atomically emits the complete backend preset and complete upstream array", async () => {
    const target = preset("exa-http");
    target.channel_config.future_backend_field = { keep: true };
    const wrapper = mountFields({ presets: [target] });

    findPresetSelect(wrapper)?.vm.$emit("update:value", "exa-http");
    await wrapper.vm.$nextTick();

    const application = lastEmission(wrapper, "apply:preset")?.[0] as {
      config: GenericHttpChannelConfig;
      upstreams: Array<{ url: string; weight: number }>;
    };
    const nextConfig = application.config;
    expect(nextConfig.preset_id).toBe("exa-http");
    expect(nextConfig.future_backend_field).toEqual({ keep: true });
    expect(application.upstreams).toEqual(target.upstreams);
  });

  it("requires a narrow confirmation before replacing multiple existing upstreams", async () => {
    const target = preset("exa-http");
    const wrapper = mountFields({
      presets: [target],
      upstreams: [
        { url: "https://old-a.example", weight: 1 },
        { url: "https://old-b.example", weight: 1 },
      ],
    });

    findPresetSelect(wrapper)?.vm.$emit("update:value", "exa-http");
    await wrapper.vm.$nextTick();
    expect(wrapper.emitted("apply:preset")).toBeUndefined();
    expect(warning).toHaveBeenCalledOnce();

    const options = warning.mock.calls[0][0] as { onPositiveClick: () => void };
    options.onPositiveClick();
    const application = lastEmission(wrapper, "apply:preset")?.[0] as {
      upstreams: Array<{ url: string; weight: number }>;
    };
    expect(application.upstreams).toEqual(target.upstreams);
  });

  it("marks a preset custom after a derived authentication field changes", async () => {
    const target = preset("tavily-http");
    const wrapper = mountFields({ config: target.channel_config, presets: [target] });
    const authName = wrapper
      .findAllComponents(NInput)
      .find(component => component.props("value") === "Authorization");

    authName?.vm.$emit("update:value", "X-Api-Key");
    await wrapper.vm.$nextTick();

    const nextConfig = lastEmission(wrapper, "update:modelValue")?.[0] as GenericHttpChannelConfig;
    expect(nextConfig.preset_id).toBe("custom");
    expect(nextConfig.auth.name).toBe("X-Api-Key");
  });

  it("selects Custom without dropping unknown future configuration fields", async () => {
    const target = preset("tavily-http");
    target.channel_config.future_backend_field = { keep: true };
    const wrapper = mountFields({ config: target.channel_config, presets: [target] });

    findPresetSelect(wrapper)?.vm.$emit("update:value", "custom");
    await wrapper.vm.$nextTick();

    const nextConfig = lastEmission(wrapper, "update:modelValue")?.[0] as GenericHttpChannelConfig;
    expect(nextConfig.preset_id).toBe("custom");
    expect(nextConfig.future_backend_field).toEqual({ keep: true });
  });

  it("keeps Custom available when the catalog is empty and disables selection while loading", () => {
    const loadingWrapper = mountFields({ catalogLoading: true });
    const loadingSelect = findPresetSelect(loadingWrapper);
    expect(loadingSelect?.props("disabled")).toBe(true);
    expect(loadingSelect?.props("loading")).toBe(true);
    expect(loadingWrapper.text()).not.toContain("channels.catalogFallback");

    const fallbackWrapper = mountFields({});
    const fallbackSelect = findPresetSelect(fallbackWrapper);
    const fallbackGroups = fallbackSelect?.props("options") as SelectGroupOption[];
    expect(flattenedOptions(fallbackGroups).map(option => option.value)).toEqual(["custom"]);
    expect(fallbackSelect?.props("disabled")).toBe(false);
    expect(fallbackWrapper.text()).toContain("channels.catalogFallback");
  });

  it("exposes only header credential injection and never builds a query preview", () => {
    const wrapper = mountFields({});

    expect(wrapper.text()).not.toContain("channels.auth.placement");
    expect(genericHttpSource).not.toContain("authPlacementOptions");
    expect(genericHttpSource).not.toContain("channels.auth.query");
    expect(genericHttpSource).not.toContain("encodeURIComponent");
    expect(wrapper.findAll(".credential-preview")).toHaveLength(1);
    expect(channelConfig().auth).toMatchObject({
      placement: "header",
      name: "Authorization",
      prefix: "Bearer ",
    });
  });

  it("uses one compact accessible selector and contains no route-affinity form", () => {
    const wrapper = mountFields({ presets: [preset("exa-http")] });
    const select = findPresetSelect(wrapper);
    expect(select?.props("filterable")).toBe(true);
    expect(select?.attributes("aria-label")).toBe("channels.selectPreset");
    expect(wrapper.find(".preset-summary").text()).toContain("channels.presets.custom.description");
    expect(genericHttpSource).not.toContain("preset-card");
    expect(genericHttpSource).not.toContain("route_affinity");
  });

  it("describes auto streaming only through HTTP negotiation signals in every locale", () => {
    expect(zhCN.channels.sections.transportDescription).toContain("Accept");
    expect(zhCN.channels.sections.transportDescription).toContain("响应媒体类型");
    expect(enUS.channels.sections.transportDescription).toContain("Accept");
    expect(enUS.channels.sections.transportDescription).toContain("response media type");
    expect(jaJP.channels.sections.transportDescription).toContain("Accept");
    expect(jaJP.channels.sections.transportDescription).toContain("メディアタイプ");
  });

  it("keeps the compact preset groups aligned across every locale", () => {
    for (const locale of [zhCN, enUS, jaJP]) {
      expect(Object.keys(locale.channels.presetGroups)).toEqual(["httpApi", "hostedMcp", "custom"]);
      expect(locale.channels.sections).not.toHaveProperty("affinity");
      expect(locale.channels).not.toHaveProperty("affinity");
    }
  });

  it("accepts empty failover statuses but rejects invalid secondary upstreams", () => {
    const validWrapper = mountFields({
      upstreams: [{ url: "https://valid.example", weight: 1 }],
    });
    expect(lastEmission(validWrapper, "validity")?.[0]).toBe(true);

    const wrapper = mountFields({
      upstreams: [
        { url: "https://valid.example", weight: 1 },
        { url: "https://user:secret@invalid.example", weight: 1 },
      ],
    });
    expect(lastEmission(wrapper, "validity")?.[0]).toBe(false);
  });

  it("rejects failover statuses below the backend 300 boundary", async () => {
    const wrapper = mountFields({});
    const input = wrapper
      .findAllComponents(NInput)
      .find(
        component => component.props("placeholder") === "channels.retry.failoverStatusesPlaceholder"
      );

    input?.vm.$emit("update:value", "200");
    await wrapper.vm.$nextTick();
    expect(lastEmission(wrapper, "validity")?.[0]).toBe(false);

    input?.vm.$emit("update:value", "429");
    await wrapper.vm.$nextTick();
    expect(lastEmission(wrapper, "validity")?.[0]).toBe(true);
  });
});
