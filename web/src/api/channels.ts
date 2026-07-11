import type { ChannelCatalog } from "@/types/channels";
import http from "@/utils/http";

export const channelsApi = {
  async getCatalog(): Promise<unknown> {
    const response = await http.get("/channel-catalog", { hideMessage: true });
    return response.data ?? response;
  },

  async getLegacyChannelTypes(): Promise<string[]> {
    const response = await http.get("/channel-types", { hideMessage: true });
    return Array.isArray(response.data) ? response.data : [];
  },

  async getCatalogAndTypes(): Promise<{ catalog: unknown; channelTypes: string[] }> {
    const [catalogResult, typesResult] = await Promise.allSettled([
      this.getCatalog(),
      this.getLegacyChannelTypes(),
    ]);

    if (catalogResult.status === "rejected" && typesResult.status === "rejected") {
      throw catalogResult.reason;
    }

    return {
      catalog: catalogResult.status === "fulfilled" ? catalogResult.value : [],
      channelTypes: typesResult.status === "fulfilled" ? typesResult.value : [],
    };
  },
};

export type { ChannelCatalog };
