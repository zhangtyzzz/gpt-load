import type { Group } from "@/types/models";
import { config, flushPromises, shallowMount } from "@vue/test-utils";
import { NButton } from "naive-ui";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const { getGroupKeys } = vi.hoisted(() => ({
  getGroupKeys: vi.fn(),
}));

vi.mock("@/api/keys", () => ({
  keysApi: {
    getGroupKeys,
  },
}));

vi.mock("vue-i18n", () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

vi.mock("naive-ui", async importOriginal => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useDialog: () => ({ warning: vi.fn(), create: vi.fn() }),
  };
});

import KeyTable from "./KeyTable.vue";

const group = {
  id: 7,
  name: "primary",
  display_name: "Primary",
} as Group;

describe("KeyTable list states", () => {
  beforeAll(() => {
    config.global.renderStubDefaultSlot = true;
  });

  afterAll(() => {
    config.global.renderStubDefaultSlot = false;
  });

  beforeEach(() => {
    getGroupKeys.mockReset();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("shows an inline error with retry instead of an empty state after loading fails", async () => {
    getGroupKeys.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({
      items: [],
      pagination: { total_items: 0, total_pages: 0 },
    });

    const wrapper = shallowMount(KeyTable, {
      props: { selectedGroup: group },
    });
    await flushPromises();

    expect(wrapper.find(".error-container").exists()).toBe(true);
    expect(wrapper.find(".empty-container").exists()).toBe(false);
    expect(wrapper.text()).toContain("common.retry");

    const retryButton = wrapper
      .findAllComponents(NButton)
      .find(button => button.text().includes("common.retry"));
    if (!retryButton) {
      throw new Error("retry button was not rendered");
    }
    retryButton.vm.$emit("click");
    await flushPromises();

    expect(getGroupKeys).toHaveBeenCalledTimes(2);
    expect(wrapper.find(".error-container").exists()).toBe(false);
    expect(wrapper.find(".empty-container").exists()).toBe(true);
  });
});
