import type { ChannelDescriptor, GenericHttpChannelConfig } from "@/types/channels";
import { NForm, NInput, NSelect } from "naive-ui";
import { config as testUtilsConfig, flushPromises, shallowMount } from "@vue/test-utils";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const {
  createGroup,
  updateGroup,
  getGroupConfigOptions,
  loadCatalog,
  messageError,
  dialogWarning,
  routeLeaveGuards,
} = vi.hoisted(() => ({
  createGroup: vi.fn(),
  updateGroup: vi.fn(),
  getGroupConfigOptions: vi.fn(),
  loadCatalog: vi.fn(),
  messageError: vi.fn(),
  dialogWarning: vi.fn(),
  routeLeaveGuards: [] as Array<() => Promise<boolean> | boolean>,
}));

const mountedWrappers: Array<{ unmount: () => void }> = [];

const descriptors: ChannelDescriptor[] = [
  {
    id: "openai",
    display_name: "OpenAI",
    description: "",
    order: 1,
    capabilities: {
      test_model: "required",
      validation_endpoint: true,
      model_redirect: true,
      param_overrides: true,
      header_rules: true,
      affinity: true,
      aggregate: true,
    },
    defaults: {
      upstream_url: "https://api.openai.com",
      test_model: "gpt-default",
      validation_endpoint: "/v1/chat/completions",
    },
    presets: [],
  },
  {
    id: "anthropic",
    display_name: "Anthropic",
    description: "",
    order: 2,
    capabilities: {
      test_model: "required",
      validation_endpoint: true,
      model_redirect: true,
      param_overrides: true,
      header_rules: true,
      affinity: true,
      aggregate: true,
    },
    defaults: {
      upstream_url: "https://api.anthropic.com",
      test_model: "claude-default",
      validation_endpoint: "/v1/messages",
    },
    presets: [],
  },
  {
    id: "generic-http",
    display_name: "Generic HTTP",
    description: "",
    order: 3,
    capabilities: {
      test_model: "hidden",
      validation_endpoint: false,
      model_redirect: false,
      param_overrides: false,
      header_rules: true,
      affinity: true,
      aggregate: true,
    },
    defaults: { upstream_url: "", test_model: "", validation_endpoint: "" },
    presets: [],
  },
];

vi.mock("vue-i18n", async importOriginal => {
  const actual = await importOriginal<typeof import("vue-i18n")>();
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) };
});

vi.mock("@vueuse/core", async () => {
  const { ref } = await import("vue");
  return { useMediaQuery: () => ref(false) };
});

vi.mock("vue-router", () => ({
  onBeforeRouteLeave: (guard: () => Promise<boolean> | boolean) => {
    routeLeaveGuards.push(guard);
  },
}));

vi.mock("@/api/keys", () => ({
  keysApi: { createGroup, updateGroup, getGroupConfigOptions },
}));

vi.mock("@/composables/useChannelCatalog", async importOriginal => {
  const actual = await importOriginal<typeof import("@/composables/useChannelCatalog")>();
  const { ref } = await import("vue");
  return {
    ...actual,
    useChannelCatalog: () => ({
      catalog: ref({ schema_version: 1, default_channel_type: "openai", items: descriptors }),
      channelTypes: ref(descriptors),
      loadCatalog,
    }),
  };
});

vi.mock("naive-ui", async importOriginal => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useDialog: () => ({ warning: dialogWarning }),
    useMessage: () => ({ error: messageError, warning: vi.fn(), success: vi.fn() }),
  };
});

import GroupFormModal from "./GroupFormModal.vue";
import groupFormSource from "./GroupFormModal.vue?raw";
import GenericHttpChannelFields from "./GenericHttpChannelFields.vue";

function findChannelSelect(wrapper: ReturnType<typeof shallowMount>) {
  return wrapper
    .findAllComponents(NSelect)
    .find(component =>
      (component.props("options") as Array<{ value: string }> | undefined)?.some(
        option => option.value === "generic-http"
      )
    );
}

function setGroupName(wrapper: ReturnType<typeof shallowMount>, value: string) {
  const input = wrapper
    .findAllComponents(NInput)
    .find(component => component.props("placeholder") === "gemini");
  input?.vm.$emit("update:value", value);
}

function lastItem<T>(items: T[]): T | undefined {
  return items[items.length - 1];
}

async function submit(wrapper: ReturnType<typeof shallowMount>) {
  const exposed = wrapper.vm as unknown as { handleSubmit: () => Promise<void> };
  await exposed.handleSubmit();
  await flushPromises();
}

describe("GroupFormModal Generic HTTP state", () => {
  beforeAll(() => {
    testUtilsConfig.global.renderStubDefaultSlot = true;
  });

  afterAll(() => {
    testUtilsConfig.global.renderStubDefaultSlot = false;
  });

  beforeEach(() => {
    createGroup.mockReset();
    updateGroup.mockReset();
    getGroupConfigOptions.mockReset().mockResolvedValue([]);
    loadCatalog.mockReset().mockResolvedValue(undefined);
    messageError.mockReset();
    dialogWarning.mockReset();
    routeLeaveGuards.length = 0;
    createGroup.mockResolvedValue({ id: 9, name: "proxy" });
  });

  afterEach(() => {
    mountedWrappers.splice(0).forEach(wrapper => wrapper.unmount());
  });

  async function mountOpenForm() {
    const wrapper = shallowMount(GroupFormModal, { props: { show: false, group: null } });
    mountedWrappers.push(wrapper);
    await wrapper.setProps({ show: true });
    await flushPromises();
    return wrapper;
  }

  it("writes a complete default Generic config into the form and sends it in a real create payload", async () => {
    const removeListener = vi.spyOn(globalThis, "removeEventListener");
    const wrapper = await mountOpenForm();
    findChannelSelect(wrapper)?.vm.$emit("update:value", "generic-http");
    await wrapper.vm.$nextTick();

    const generic = wrapper.findComponent(GenericHttpChannelFields);
    const defaultConfig = generic.props("modelValue") as GenericHttpChannelConfig;
    expect(defaultConfig).toMatchObject({
      version: 1,
      preset_id: "custom",
      retry: { safe_methods: ["GET", "HEAD"], failover_statuses: [] },
      max_error_body_bytes: 65536,
    });
    expect(defaultConfig).not.toHaveProperty("route_affinity");

    generic.vm.$emit("update:upstreams", [{ url: "https://custom.example", weight: 1 }]);
    generic.vm.$emit("validity", true);
    setGroupName(wrapper, "custom-proxy");
    await wrapper.vm.$nextTick();
    await submit(wrapper);

    expect(createGroup).toHaveBeenCalledOnce();
    expect(createGroup.mock.calls[0][0]).toMatchObject({
      name: "custom-proxy",
      channel_type: "generic-http",
      upstreams: [{ url: "https://custom.example", weight: 1 }],
      channel_config: defaultConfig,
      test_model: "",
      validation_endpoint: "",
      param_overrides: {},
      model_redirect_rules: {},
      model_redirect_strict: false,
    });
    expect(dialogWarning).not.toHaveBeenCalled();
    expect(removeListener).toHaveBeenCalledWith("beforeunload", expect.any(Function));
  });

  it("atomically restores legacy defaults and clears Generic config when switching back", async () => {
    const wrapper = await mountOpenForm();
    const channelSelect = findChannelSelect(wrapper);
    channelSelect?.vm.$emit("update:value", "generic-http");
    await wrapper.vm.$nextTick();
    const generic = wrapper.findComponent(GenericHttpChannelFields);
    generic.vm.$emit("update:upstreams", [{ url: "https://generic.example", weight: 1 }]);

    channelSelect?.vm.$emit("update:value", "anthropic");
    await wrapper.vm.$nextTick();
    setGroupName(wrapper, "anthropic-proxy");
    await submit(wrapper);

    expect(createGroup.mock.calls[0][0]).toMatchObject({
      channel_type: "anthropic",
      upstreams: [{ url: "https://api.anthropic.com", weight: 1 }],
      test_model: "claude-default",
      validation_endpoint: "/v1/messages",
      channel_config: undefined,
    });
  });

  it("marks an atomic preset application dirty without weakening discard protection", async () => {
    const wrapper = await mountOpenForm();
    findChannelSelect(wrapper)?.vm.$emit("update:value", "generic-http");
    await wrapper.vm.$nextTick();
    const generic = wrapper.findComponent(GenericHttpChannelFields);
    const currentConfig = generic.props("modelValue") as GenericHttpChannelConfig;
    const exposed = wrapper.vm as unknown as {
      captureSavedSnapshot: () => void;
      isDirty: boolean;
    };

    exposed.captureSavedSnapshot();
    expect(exposed.isDirty).toBe(false);
    generic.vm.$emit("apply:preset", {
      config: { ...currentConfig, preset_id: "exa-http", future_backend_field: { keep: true } },
      upstreams: [{ url: "https://api.exa.example", weight: 1 }],
    });
    await wrapper.vm.$nextTick();

    expect(exposed.isDirty).toBe(true);
    expect(generic.props("modelValue")).toMatchObject({
      preset_id: "exa-http",
      future_backend_field: { keep: true },
    });
  });

  it("blocks a create payload when any Generic upstream URL is unsafe", async () => {
    const wrapper = await mountOpenForm();
    findChannelSelect(wrapper)?.vm.$emit("update:value", "generic-http");
    await wrapper.vm.$nextTick();
    const generic = wrapper.findComponent(GenericHttpChannelFields);
    generic.vm.$emit("update:upstreams", [
      { url: "https://valid.example", weight: 1 },
      { url: "https://user:secret@invalid.example", weight: 1 },
    ]);
    generic.vm.$emit("validity", true);
    setGroupName(wrapper, "unsafe-proxy");

    await submit(wrapper);

    expect(createGroup).not.toHaveBeenCalled();
    expect(messageError).toHaveBeenCalledWith("channels.validation.allUpstreamsInvalid");
  });

  it("keeps one mobile vertical scroll owner and a safe-area footer at 390px", () => {
    expect(groupFormSource.match(/overflow-y:\s*auto/g)).toHaveLength(1);
    expect(groupFormSource).toContain("max-height: calc(100dvh - 1rem)");
    expect(groupFormSource).toContain("env(safe-area-inset-bottom)");
    expect(groupFormSource).toContain("grid-template-columns: repeat(2, minmax(0, 1fr))");
  });

  it("captures initialization as clean and closes an untouched form without confirmation", async () => {
    const addListener = vi.spyOn(globalThis, "addEventListener");
    const wrapper = await mountOpenForm();
    const exposed = wrapper.vm as unknown as {
      isDirty: boolean;
      requestClose: () => Promise<void>;
    };

    expect(exposed.isDirty).toBe(false);
    expect(addListener).not.toHaveBeenCalledWith("beforeunload", expect.any(Function));
    await exposed.requestClose();

    expect(dialogWarning).not.toHaveBeenCalled();
    expect(lastItem(wrapper.emitted("update:show") || [])).toEqual([false]);
  });

  it("does not mark delayed catalog initialization or form hydration dirty", async () => {
    let resolveCatalog: (() => void) | undefined;
    loadCatalog.mockReturnValueOnce(
      new Promise<void>(resolve => {
        resolveCatalog = resolve;
      })
    );
    const wrapper = shallowMount(GroupFormModal, { props: { show: false, group: null } });
    mountedWrappers.push(wrapper);
    await wrapper.setProps({ show: true });
    const exposed = wrapper.vm as unknown as {
      handleSubmit: () => Promise<void>;
      initializing: boolean;
      isDirty: boolean;
    };
    expect(exposed.isDirty).toBe(false);
    expect(exposed.initializing).toBe(true);
    expect(wrapper.findComponent(NForm).props("disabled")).toBe(true);
    await exposed.handleSubmit();
    expect(createGroup).not.toHaveBeenCalled();

    resolveCatalog?.();
    await flushPromises();
    expect(exposed.initializing).toBe(false);
    expect(wrapper.findComponent(NForm).props("disabled")).toBe(false);
    expect(exposed.isDirty).toBe(false);
    expect(dialogWarning).not.toHaveBeenCalled();
  });

  it("keeps dirty edits when confirmation is cancelled and removes beforeunload after discard", async () => {
    const addListener = vi.spyOn(globalThis, "addEventListener");
    const removeListener = vi.spyOn(globalThis, "removeEventListener");
    const wrapper = await mountOpenForm();
    const exposed = wrapper.vm as unknown as {
      formData: { name: string };
      isDirty: boolean;
      requestClose: () => Promise<void>;
    };
    exposed.formData.name = "changed";
    await wrapper.vm.$nextTick();

    expect(exposed.isDirty).toBe(true);
    expect(addListener).toHaveBeenCalledWith("beforeunload", expect.any(Function));

    const cancelledClose = exposed.requestClose();
    await wrapper.vm.$nextTick();
    const cancelledDialog = lastItem(dialogWarning.mock.calls)?.[0] as {
      onNegativeClick: () => void;
    };
    cancelledDialog.onNegativeClick();
    await cancelledClose;
    expect(wrapper.emitted("update:show")).toBeUndefined();
    expect(exposed.isDirty).toBe(true);

    const confirmedClose = exposed.requestClose();
    await wrapper.vm.$nextTick();
    const confirmedDialog = lastItem(dialogWarning.mock.calls)?.[0] as {
      onPositiveClick: () => void;
    };
    confirmedDialog.onPositiveClick();
    await confirmedClose;
    expect(lastItem(wrapper.emitted("update:show") || [])).toEqual([false]);
    expect(removeListener).toHaveBeenCalledWith("beforeunload", expect.any(Function));
  });

  it("uses the same async confirmation for route leave", async () => {
    const wrapper = await mountOpenForm();
    const exposed = wrapper.vm as unknown as { formData: { name: string }; isDirty: boolean };
    exposed.formData.name = "route-change";
    await wrapper.vm.$nextTick();
    const guard = lastItem(routeLeaveGuards);
    expect(guard).toBeDefined();

    const blockedNavigation = Promise.resolve(guard?.());
    await wrapper.vm.$nextTick();
    const blockedDialog = lastItem(dialogWarning.mock.calls)?.[0] as {
      onNegativeClick: () => void;
    };
    blockedDialog.onNegativeClick();
    await expect(blockedNavigation).resolves.toBe(false);
    expect(exposed.isDirty).toBe(true);

    const allowedNavigation = Promise.resolve(guard?.());
    await wrapper.vm.$nextTick();
    const allowedDialog = lastItem(dialogWarning.mock.calls)?.[0] as {
      onPositiveClick: () => void;
    };
    allowedDialog.onPositiveClick();
    await expect(allowedNavigation).resolves.toBe(true);
    expect(exposed.isDirty).toBe(false);
  });
});
