import { normalizeGenericHttpConfig } from "@/composables/useChannelCatalog";
import type { Group, SubGroupInfo } from "@/types/models";
import { NSelect } from "naive-ui";
import { config as testUtilsConfig, flushPromises, shallowMount } from "@vue/test-utils";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const { addSubGroups, dialogWarning, routeLeaveGuards } = vi.hoisted(() => ({
  addSubGroups: vi.fn(),
  dialogWarning: vi.fn(),
  routeLeaveGuards: [] as Array<() => Promise<boolean> | boolean>,
}));

const mountedWrappers: Array<{ unmount: () => void }> = [];

vi.mock("vue-i18n", async importOriginal => {
  const actual = await importOriginal<typeof import("vue-i18n")>();
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) };
});

vi.mock("@/api/keys", () => ({
  keysApi: { addSubGroups },
}));

vi.mock("vue-router", () => ({
  onBeforeRouteLeave: (guard: () => Promise<boolean> | boolean) => {
    routeLeaveGuards.push(guard);
  },
}));

vi.mock("naive-ui", async importOriginal => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useDialog: () => ({ warning: dialogWarning }),
    useMessage: () => ({ error: vi.fn(), warning: vi.fn(), success: vi.fn() }),
  };
});

import AddSubGroupModal from "./AddSubGroupModal.vue";
import addSubGroupSource from "./AddSubGroupModal.vue?raw";

function group(id: number, presetId: string, type: "standard" | "aggregate" = "standard"): Group {
  const channelConfig = normalizeGenericHttpConfig(undefined);
  channelConfig.preset_id = presetId;
  return {
    id,
    name: `group-${id}`,
    display_name: `Group ${id}`,
    description: "",
    sort: id,
    test_model: "",
    channel_type: "generic-http",
    channel_config: channelConfig,
    upstreams: [{ url: `https://${id}.example`, weight: 1 }],
    validation_endpoint: "",
    config: {},
    param_overrides: {},
    model_redirect_rules: {},
    model_redirect_strict: false,
    proxy_keys: "",
    group_type: type,
  };
}

describe("AddSubGroupModal Generic HTTP compatibility", () => {
  beforeAll(() => {
    testUtilsConfig.global.renderStubDefaultSlot = true;
  });

  afterAll(() => {
    testUtilsConfig.global.renderStubDefaultSlot = false;
  });

  beforeEach(() => {
    addSubGroups.mockReset().mockResolvedValue(undefined);
    dialogWarning.mockReset();
    routeLeaveGuards.length = 0;
  });

  afterEach(() => {
    mountedWrappers.splice(0).forEach(wrapper => wrapper.unmount());
    vi.restoreAllMocks();
  });

  function lastItem<T>(items: T[]): T | undefined {
    return items[items.length - 1];
  }

  async function mountOpenForm() {
    const aggregate = group(100, "custom", "aggregate");
    const candidate = group(2, "custom");
    const wrapper = shallowMount(AddSubGroupModal, {
      props: {
        show: false,
        aggregateGroup: aggregate,
        existingSubGroups: [],
        groups: [candidate],
      },
    });
    mountedWrappers.push(wrapper);
    await wrapper.setProps({ show: true });
    await flushPromises();
    return { wrapper, aggregate, candidate };
  }

  it("only lists candidates whose normalized config exactly matches existing children", () => {
    const aggregate = group(100, "custom", "aggregate");
    const existing = group(1, "tavily-http");
    const matching = group(2, "tavily-http");
    const differentPreset = group(3, "custom");
    const existingInfo: SubGroupInfo = {
      group: existing,
      weight: 1,
      total_keys: 1,
      active_keys: 1,
      invalid_keys: 0,
    };
    const wrapper = shallowMount(AddSubGroupModal, {
      props: {
        show: true,
        aggregateGroup: aggregate,
        existingSubGroups: [existingInfo],
        groups: [existing, matching, differentPreset],
      },
    });
    mountedWrappers.push(wrapper);

    const options = wrapper.findComponent(NSelect).props("options") as Array<{ value: number }>;
    expect(options).toEqual([expect.objectContaining({ value: matching.id })]);
    expect(wrapper.text()).toContain("channels.aggregate.genericCompatibility");
  });

  it("captures a clean baseline after reset and closes untouched without confirmation", async () => {
    const { wrapper } = await mountOpenForm();
    const exposed = wrapper.vm as unknown as {
      isDirty: boolean;
      requestClose: () => Promise<void>;
    };

    expect(exposed.isDirty).toBe(false);
    await exposed.requestClose();

    expect(dialogWarning).not.toHaveBeenCalled();
    expect(lastItem(wrapper.emitted("update:show") || [])).toEqual([false]);
  });

  it("deduplicates dirty confirmations and protects browser unload", async () => {
    const addListener = vi.spyOn(globalThis, "addEventListener");
    const removeListener = vi.spyOn(globalThis, "removeEventListener");
    const { wrapper, candidate } = await mountOpenForm();
    const exposed = wrapper.vm as unknown as {
      formData: { sub_groups: Array<{ group_id: number | null; weight: number }> };
      isDirty: boolean;
      requestClose: () => Promise<void>;
    };
    exposed.formData.sub_groups[0].group_id = candidate.id || null;
    await wrapper.vm.$nextTick();

    expect(exposed.isDirty).toBe(true);
    expect(addListener).toHaveBeenCalledWith("beforeunload", expect.any(Function));

    const firstClose = exposed.requestClose();
    const secondClose = exposed.requestClose();
    await wrapper.vm.$nextTick();
    expect(dialogWarning).toHaveBeenCalledOnce();
    const cancelledDialog = dialogWarning.mock.calls[0][0] as { onMaskClick: () => void };
    cancelledDialog.onMaskClick();
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

  it("uses the same confirmation for route leave", async () => {
    const { wrapper, candidate } = await mountOpenForm();
    const exposed = wrapper.vm as unknown as {
      formData: { sub_groups: Array<{ group_id: number | null; weight: number }> };
      isDirty: boolean;
    };
    exposed.formData.sub_groups[0].group_id = candidate.id || null;
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

  it("clears dirty state before a successful add closes", async () => {
    const { wrapper, aggregate, candidate } = await mountOpenForm();
    const exposed = wrapper.vm as unknown as {
      formData: { sub_groups: Array<{ group_id: number | null; weight: number }> };
      isDirty: boolean;
      handleSubmit: () => Promise<void>;
    };
    exposed.formData.sub_groups[0].group_id = candidate.id || null;
    await wrapper.vm.$nextTick();
    expect(exposed.isDirty).toBe(true);

    await exposed.handleSubmit();
    await flushPromises();

    expect(addSubGroups).toHaveBeenCalledWith(aggregate.id, [
      { group_id: candidate.id, weight: 1 },
    ]);
    expect(dialogWarning).not.toHaveBeenCalled();
    expect(exposed.isDirty).toBe(false);
    expect(lastItem(wrapper.emitted("update:show") || [])).toEqual([false]);
  });

  it("keeps the mobile footer above the device safe area", () => {
    expect(addSubGroupSource).toContain("env(safe-area-inset-bottom)");
  });
});
