import type { ChannelDescriptor } from "@/types/channels";
import { NForm, NSelect } from "naive-ui";
import { config as testUtilsConfig, flushPromises, shallowMount } from "@vue/test-utils";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const { createGroup, updateGroup, loadCatalog, dialogWarning, routeLeaveGuards } = vi.hoisted(
  () => ({
    createGroup: vi.fn(),
    updateGroup: vi.fn(),
    loadCatalog: vi.fn(),
    dialogWarning: vi.fn(),
    routeLeaveGuards: [] as Array<() => Promise<boolean> | boolean>,
  })
);

const mountedWrappers: Array<{ unmount: () => void }> = [];

const channels: ChannelDescriptor[] = ["openai", "generic-http"].map((id, order) => ({
  id,
  display_name: id,
  description: "",
  order,
  capabilities: {
    test_model: id === "generic-http" ? "hidden" : "required",
    validation_endpoint: id !== "generic-http",
    model_redirect: id !== "generic-http",
    param_overrides: id !== "generic-http",
    header_rules: true,
    affinity: true,
    aggregate: true,
  },
  defaults: { upstream_url: "", test_model: "", validation_endpoint: "" },
  presets: [],
}));

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
  keysApi: { createGroup, updateGroup },
}));

vi.mock("@/composables/useChannelCatalog", async () => {
  const { ref } = await import("vue");
  return {
    useChannelCatalog: () => ({
      catalog: ref({ schema_version: 1, default_channel_type: "openai", items: channels }),
      channelTypes: ref(channels),
      loadCatalog,
    }),
  };
});

vi.mock("naive-ui", async importOriginal => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useDialog: () => ({ warning: dialogWarning }),
    useMessage: () => ({ error: vi.fn(), warning: vi.fn(), success: vi.fn() }),
  };
});

import AggregateGroupModal from "./AggregateGroupModal.vue";
import aggregateGroupSource from "./AggregateGroupModal.vue?raw";

describe("AggregateGroupModal", () => {
  beforeAll(() => {
    testUtilsConfig.global.renderStubDefaultSlot = true;
  });

  afterAll(() => {
    testUtilsConfig.global.renderStubDefaultSlot = false;
  });

  beforeEach(() => {
    createGroup.mockReset().mockResolvedValue({ id: 10, name: "aggregate" });
    updateGroup.mockReset();
    loadCatalog.mockReset().mockResolvedValue(undefined);
    dialogWarning.mockReset();
    routeLeaveGuards.length = 0;
  });

  afterEach(() => {
    mountedWrappers.splice(0).forEach(wrapper => wrapper.unmount());
    vi.restoreAllMocks();
  });

  async function mountOpenForm() {
    const wrapper = shallowMount(AggregateGroupModal, { props: { show: false, group: null } });
    mountedWrappers.push(wrapper);
    await wrapper.setProps({ show: true });
    await flushPromises();
    return wrapper;
  }

  function lastItem<T>(items: T[]): T | undefined {
    return items[items.length - 1];
  }

  it("offers Generic HTTP as an aggregate channel without inventing parent config", async () => {
    const wrapper = await mountOpenForm();

    const channelSelect = wrapper
      .findAllComponents(NSelect)
      .find(component =>
        (component.props("options") as Array<{ value: string }>).some(
          option => option.value === "generic-http"
        )
      );

    expect(channelSelect).toBeDefined();
    expect(channelSelect?.props("options")).toEqual(
      expect.arrayContaining([expect.objectContaining({ value: "generic-http" })])
    );
  });

  it("captures the baseline only after asynchronous catalog hydration", async () => {
    let resolveCatalog: (() => void) | undefined;
    loadCatalog.mockReturnValueOnce(
      new Promise<void>(resolve => {
        resolveCatalog = resolve;
      })
    );
    const wrapper = shallowMount(AggregateGroupModal, { props: { show: false, group: null } });
    mountedWrappers.push(wrapper);
    await wrapper.setProps({ show: true });
    const exposed = wrapper.vm as unknown as {
      handleSubmit: () => Promise<void>;
      initializing: boolean;
      isDirty: boolean;
      requestClose: () => Promise<void>;
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

    await exposed.requestClose();
    expect(dialogWarning).not.toHaveBeenCalled();
    expect(lastItem(wrapper.emitted("update:show") || [])).toEqual([false]);
  });

  it("deduplicates dirty confirmations and protects browser unload", async () => {
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

    const firstClose = exposed.requestClose();
    const secondClose = exposed.requestClose();
    await wrapper.vm.$nextTick();
    expect(dialogWarning).toHaveBeenCalledOnce();
    const cancelledDialog = dialogWarning.mock.calls[0][0] as { onNegativeClick: () => void };
    cancelledDialog.onNegativeClick();
    await Promise.all([firstClose, secondClose]);
    expect(wrapper.emitted("update:show")).toBeUndefined();

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

  it("blocks route leave until dirty changes are explicitly discarded", async () => {
    const wrapper = await mountOpenForm();
    const exposed = wrapper.vm as unknown as { formData: { name: string }; isDirty: boolean };
    exposed.formData.name = "route-change";
    await wrapper.vm.$nextTick();
    const guard = lastItem(routeLeaveGuards);

    const blockedNavigation = Promise.resolve(guard?.());
    await wrapper.vm.$nextTick();
    const blockedDialog = lastItem(dialogWarning.mock.calls)?.[0] as {
      onNegativeClick: () => void;
    };
    blockedDialog.onNegativeClick();
    await expect(blockedNavigation).resolves.toBe(false);

    const allowedNavigation = Promise.resolve(guard?.());
    await wrapper.vm.$nextTick();
    const allowedDialog = lastItem(dialogWarning.mock.calls)?.[0] as {
      onPositiveClick: () => void;
    };
    allowedDialog.onPositiveClick();
    await expect(allowedNavigation).resolves.toBe(true);
    expect(exposed.isDirty).toBe(false);
  });

  it("clears dirty state before a successful create closes", async () => {
    const wrapper = await mountOpenForm();
    const exposed = wrapper.vm as unknown as {
      formData: { name: string };
      isDirty: boolean;
      handleSubmit: () => Promise<void>;
    };
    exposed.formData.name = "aggregate";
    await wrapper.vm.$nextTick();
    expect(exposed.isDirty).toBe(true);

    await exposed.handleSubmit();
    await flushPromises();

    expect(createGroup).toHaveBeenCalledOnce();
    expect(dialogWarning).not.toHaveBeenCalled();
    expect(exposed.isDirty).toBe(false);
    expect(lastItem(wrapper.emitted("update:show") || [])).toEqual([false]);
  });

  it("keeps the mobile footer above the device safe area", () => {
    expect(aggregateGroupSource).toContain("env(safe-area-inset-bottom)");
  });
});
