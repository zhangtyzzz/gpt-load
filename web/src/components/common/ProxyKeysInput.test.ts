import { NInput } from "naive-ui";
import { shallowMount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

vi.mock("vue-i18n", async importOriginal => {
  const actual = await importOriginal<typeof import("vue-i18n")>();
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) };
});

vi.mock("naive-ui", async importOriginal => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useMessage: () => ({ error: vi.fn(), warning: vi.fn(), success: vi.fn() }),
  };
});

import ProxyKeysInput from "./ProxyKeysInput.vue";

describe("ProxyKeysInput", () => {
  it("does not let password managers reuse the console login credential", () => {
    const wrapper = shallowMount(ProxyKeysInput, {
      props: { modelValue: "" },
    });
    const input = wrapper.findComponent(NInput);

    expect(input.props("value")).toBe("");
    expect(input.props("inputProps")).toMatchObject({
      autocomplete: "new-password",
      name: "gpt-load-proxy-keys",
      autocapitalize: "none",
      spellcheck: false,
    });
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  });
});
