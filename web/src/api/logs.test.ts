import { beforeEach, describe, expect, it, vi } from "vitest";

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));

vi.mock("@/utils/http", () => ({
  default: { get, post },
}));

import { logApi } from "./logs";

describe("logApi credential-safe filters", () => {
  beforeEach(() => {
    get.mockReset();
    post.mockReset();
    post.mockResolvedValue(new Blob());
  });

  it("sends a complete key search in a POST JSON body, never a GET query", async () => {
    const secret = "sk-post-body-only";
    const filters = { page: 1, page_size: 15, key_value: secret };

    await logApi.getLogs(filters);

    expect(post).toHaveBeenCalledWith("/logs/search", filters, { hideMessage: true });
    expect(get).not.toHaveBeenCalled();
    expect(post.mock.calls[0][0]).not.toContain(secret);
  });

  it("exports with POST JSON filters and shared header authentication", async () => {
    const secret = "sk-export-body-only";
    const filters = { group_name: "primary", key_value: secret };

    await logApi.exportLogs(filters);

    expect(post).toHaveBeenCalledWith("/logs/export", filters, {
      responseType: "blob",
      hideMessage: true,
    });
    expect(get).not.toHaveBeenCalled();
    expect(post.mock.calls[0][0]).not.toContain(secret);
  });
});
