import type { ApiResponse, Group, LogFilter, LogsResponse } from "@/types/models";
import http from "@/utils/http";

export const logApi = {
  // Filters may contain a complete upstream key. Keep them in a JSON body so
  // credentials never enter browser history, proxy access logs, or URLs.
  getLogs: (params: LogFilter): Promise<ApiResponse<LogsResponse>> => {
    return http.post("/logs/search", params, { hideMessage: true });
  },

  // 获取分组列表（用于筛选）
  getGroups: (): Promise<ApiResponse<Group[]>> => {
    return http.get("/groups");
  },

  // 导出日志
  exportLogs: (params: Omit<LogFilter, "page" | "page_size">): Promise<Blob> => {
    return http.post<Blob, Blob>("/logs/export", params, {
      responseType: "blob",
      hideMessage: true,
    });
  },
};
