import { config, flushPromises, mount } from "@vue/test-utils";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { ref } from "vue";

const { getLogs } = vi.hoisted(() => ({ getLogs: vi.fn() }));

vi.mock("@/api/logs", () => ({
  logApi: {
    getLogs,
    getGroups: vi.fn().mockResolvedValue({ code: 0, data: [] }),
    exportLogs: vi.fn(),
  },
}));

vi.mock("vue-i18n", () => ({
  useI18n: () => ({ t: (key: string) => key, locale: ref("en-US") }),
}));

vi.mock("naive-ui", async importOriginal => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useMessage: () => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() }),
  };
});

import { maskKey } from "@/utils/display";
import LogTable from "./LogTable.vue";

// Shaped exactly like a backend response: the identifier is already resolved and
// masked server-side by utils.KeyIdentifier.
const BACKEND_IDENTIFIER = "sk-l****mnop#b91b0e612994";
const BACKEND_FINGERPRINT = "fp:b91b0e612994";
const PLAINTEXT_KEY = "sk-live-abcdefghijklmnop";

function logRow(overrides: Record<string, unknown> = {}) {
  return {
    id: "row-1",
    timestamp: "2026-08-12T10:00:00Z",
    group_id: 1,
    key_id: 0,
    is_success: false,
    source_ip: "10.0.0.1",
    status_code: 400,
    request_path: "/proxy/demo",
    duration_ms: 12,
    error_message: "upstream rejected",
    user_agent: "curl/8",
    request_type: "final",
    group_name: "primary",
    key_value: BACKEND_IDENTIFIER,
    key_fingerprint: BACKEND_FINGERPRINT,
    model: "gpt-4",
    upstream_addr: "https://upstream.example/v1",
    is_stream: false,
    ...overrides,
  };
}

function mountWithRows(rows: Array<Record<string, unknown>>) {
  getLogs.mockResolvedValue({
    code: 0,
    data: {
      items: rows,
      pagination: { page: 1, page_size: 15, total_items: rows.length, total_pages: 1 },
    },
  });
  return mount(LogTable);
}

describe("log key identifier rendering", () => {
  beforeAll(() => {
    config.global.renderStubDefaultSlot = true;
  });

  afterAll(() => {
    config.global.renderStubDefaultSlot = false;
  });

  beforeEach(() => {
    getLogs.mockReset();
    localStorage.clear();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("renders the backend identifier verbatim, without masking it a second time", async () => {
    const wrapper = mountWithRows([logRow()]);
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain(BACKEND_IDENTIFIER);

    // Masking an already-masked value visibly corrupts it. Asserting the corrupted
    // form is absent is what proves the frontend does not re-mask a server-side
    // value: the two mask functions must be applied to a key exactly once,
    // and for this column that once happens on the server.
    const doubleMasked = maskKey(BACKEND_IDENTIFIER);
    expect(doubleMasked).not.toBe(BACKEND_IDENTIFIER);
    expect(text).not.toContain(doubleMasked);
  });

  it("agrees with the key management column on the mask portion of the same key", async () => {
    const wrapper = mountWithRows([logRow()]);
    await flushPromises();

    // What key management renders for this key: the browser masks the plaintext it
    // receives from GET /keys.
    const keyManagementValue = maskKey(PLAINTEXT_KEY);
    expect(keyManagementValue).toBe("sk-l****mnop");

    // The log column starts with exactly that, so the two screens can be matched
    // by eye; the remainder only disambiguates colliding masks.
    expect(BACKEND_IDENTIFIER.startsWith(keyManagementValue)).toBe(true);
    expect(wrapper.text()).toContain(keyManagementValue);
  });

  it("renders a fallback fingerprint row without inventing a mask", async () => {
    const wrapper = mountWithRows([
      logRow({
        id: "row-historical",
        key_value: BACKEND_FINGERPRINT,
        key_fingerprint: BACKEND_FINGERPRINT,
      }),
    ]);
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain(BACKEND_FINGERPRINT);
    expect(text).not.toContain("****");
  });

  it("keeps colliding masks distinguishable in the list", async () => {
    const first = "sk-p****9z7q#2cbb4afc57aa";
    const second = "sk-p****9z7q#75a31be09cd2";
    const wrapper = mountWithRows([
      logRow({ id: "row-a", key_value: first, key_fingerprint: "fp:2cbb4afc57aa" }),
      logRow({ id: "row-b", key_value: second, key_fingerprint: "fp:75a31be09cd2" }),
    ]);
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain(first);
    expect(text).toContain(second);
    expect(first).not.toBe(second);
  });

  it("sends the pasted identifier to the search API unmodified", async () => {
    // An operator copies the whole column value, discriminator included. It has to
    // reach the backend intact: the discriminator is what makes the search land on
    // one key rather than every key sharing the mask.
    const wrapper = mountWithRows([logRow()]);
    await flushPromises();

    // The i18n mock returns the message key, so the placeholder identifies the
    // key-search box among the filter inputs.
    const input = wrapper.find('input[placeholder="logs.keySearchPlaceholder"]');
    expect(input.exists()).toBe(true);
    await input.setValue(BACKEND_IDENTIFIER);
    await input.trigger("keyup.enter");
    await flushPromises();

    const calls = getLogs.mock.calls;
    const lastCall = calls[calls.length - 1];
    expect(lastCall).toBeDefined();
    expect(lastCall?.[0].key_value).toBe(BACKEND_IDENTIFIER);
    // Specifically, the "#…" part must survive; without it the search widens to
    // every key with the same mask.
    expect(lastCall?.[0].key_value).toContain("#");
  });
});
