import { beforeEach, describe, expect, it, vi } from "vitest";

const { get } = vi.hoisted(() => ({ get: vi.fn() }));

vi.mock("@/utils/http", () => ({
  default: { get },
}));

import { channelsApi } from "./channels";

describe("channelsApi catalog compatibility", () => {
  beforeEach(() => {
    get.mockReset();
  });

  it("falls back to registered channel types when the new catalog is unavailable", async () => {
    get.mockImplementation((url: string) => {
      if (url === "/channel-catalog") {
        return Promise.reject(new Error("not found"));
      }
      return Promise.resolve({ data: ["openai", "generic-http"] });
    });

    await expect(channelsApi.getCatalogAndTypes()).resolves.toEqual({
      catalog: [],
      channelTypes: ["openai", "generic-http"],
    });
  });

  it("surfaces an error only when both endpoints fail", async () => {
    get.mockRejectedValue(new Error("offline"));
    await expect(channelsApi.getCatalogAndTypes()).rejects.toThrow("offline");
  });
});
