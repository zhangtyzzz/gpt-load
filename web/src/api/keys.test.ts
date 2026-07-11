import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}));

vi.mock("@/utils/http", () => ({
  default: { get, post },
}));

import { keysApi } from "./keys";

const createObjectURLDescriptor = Object.getOwnPropertyDescriptor(URL, "createObjectURL");
const revokeObjectURLDescriptor = Object.getOwnPropertyDescriptor(URL, "revokeObjectURL");

describe("keysApi credential transport", () => {
  beforeEach(() => {
    get.mockReset();
    post.mockReset();
    localStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
    if (createObjectURLDescriptor) {
      Object.defineProperty(URL, "createObjectURL", createObjectURLDescriptor);
    } else {
      Reflect.deleteProperty(URL, "createObjectURL");
    }
    if (revokeObjectURLDescriptor) {
      Object.defineProperty(URL, "revokeObjectURL", revokeObjectURLDescriptor);
    } else {
      Reflect.deleteProperty(URL, "revokeObjectURL");
    }
  });

  it("puts a complete upstream key in a POST body, never in the URL", async () => {
    const secret = "sk-upstream-search-secret";
    post.mockResolvedValue({
      data: {
        items: [],
        pagination: { total_items: 0, total_pages: 0 },
      },
    });

    await keysApi.getGroupKeys({
      group_id: 7,
      page: 2,
      page_size: 24,
      status: "active",
      key_value: secret,
    });

    expect(get).not.toHaveBeenCalled();
    expect(post).toHaveBeenCalledWith(
      "/keys/search",
      { group_id: 7, status: "active", key_value: secret },
      { params: { page: 2, page_size: 24 }, hideMessage: true }
    );

    const [url, _body, config] = post.mock.calls[0];
    expect(url).not.toContain(secret);
    expect(JSON.stringify(config)).not.toContain(secret);
  });

  it("keeps ordinary list filters on the non-sensitive GET endpoint", async () => {
    get.mockResolvedValue({
      data: {
        items: [],
        pagination: { total_items: 0, total_pages: 0 },
      },
    });

    await keysApi.getGroupKeys({
      group_id: 7,
      page: 1,
      page_size: 12,
      status: "invalid",
    });

    expect(post).not.toHaveBeenCalled();
    expect(get).toHaveBeenCalledWith("/keys", {
      params: { group_id: 7, status: "invalid", page: 1, page_size: 12 },
      hideMessage: true,
    });
  });

  it("downloads through the authenticated client without an auth key in the URL", async () => {
    vi.useFakeTimers();
    const authSecret = "console-auth-secret";
    const blob = new Blob(["sk-exported-upstream-key"]);
    const createObjectURL = vi.fn(() => "blob:keys-export");
    const revokeObjectURL = vi.fn();
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: createObjectURL });
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: revokeObjectURL });
    localStorage.setItem("authKey", authSecret);
    get.mockResolvedValue(blob);

    await keysApi.exportKeys(7, "active");

    expect(get).toHaveBeenCalledWith("/keys/export", {
      params: { group_id: 7, status: "active" },
      responseType: "blob",
      hideMessage: true,
    });
    const [url, config] = get.mock.calls[0];
    expect(url).not.toContain(authSecret);
    expect(JSON.stringify(config)).not.toContain(authSecret);
    expect(JSON.stringify(config)).not.toContain("authKey");
    expect(createObjectURL).toHaveBeenCalledWith(blob);
    expect(revokeObjectURL).not.toHaveBeenCalled();
    vi.runAllTimers();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:keys-export");
    expect(click).toHaveBeenCalledOnce();
  });
});
