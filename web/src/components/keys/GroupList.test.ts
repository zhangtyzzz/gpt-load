import type { Group } from "@/types/models";
import { config, shallowMount } from "@vue/test-utils";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

vi.mock("vue-i18n", async importOriginal => {
  const actual = await importOriginal<typeof import("vue-i18n")>();
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

vi.mock("@/api/keys", () => ({
  keysApi: {
    reorderGroups: vi.fn(),
  },
}));

import GroupList from "./GroupList.vue";

function makeGroup(id: number, channelType: Group["channel_type"]): Group {
  return {
    id,
    name: `group-${id}`,
    display_name: `Group ${id}`,
    description: "",
    sort: id,
    test_model: "test-model",
    channel_type: channelType,
    upstreams: [],
    validation_endpoint: "",
    config: {},
    param_overrides: {},
    model_redirect_rules: {},
    model_redirect_strict: false,
    proxy_keys: "",
  };
}

describe("GroupList visual state contracts", () => {
  beforeAll(() => {
    config.global.renderStubDefaultSlot = true;
  });

  afterAll(() => {
    config.global.renderStubDefaultSlot = false;
  });

  it("keeps provider identity classes and selected semantics on a populated list", () => {
    const groups = [
      makeGroup(1, "anthropic"),
      makeGroup(2, "openai"),
      makeGroup(3, "gemini"),
      ...Array.from({ length: 13 }, (_, index) => makeGroup(index + 4, "openai-response")),
    ];

    const wrapper = shallowMount(GroupList, {
      props: {
        groups,
        selectedGroup: groups[0],
      },
    });

    expect(wrapper.findAll(".group-item")).toHaveLength(16);
    expect(wrapper.find(".group-item.active .channel-tag--warning").exists()).toBe(true);
    expect(wrapper.find(".channel-tag--success").exists()).toBe(true);
    expect(wrapper.find(".channel-tag--info").exists()).toBe(true);
    expect(wrapper.find('.group-select-control[aria-pressed="true"]').exists()).toBe(true);
  });

  it("renders long provider metadata inside the selected item", () => {
    const group = makeGroup(20, "openai-response");
    group.display_name = "openai-response 长列表样例 20";

    const wrapper = shallowMount(GroupList, {
      props: {
        groups: [group],
        selectedGroup: group,
      },
    });

    expect(wrapper.find(".group-item.active").text()).toContain("#group-20");
    expect(wrapper.find(".channel-tag--success").text()).toContain("openai-response");
  });
});
