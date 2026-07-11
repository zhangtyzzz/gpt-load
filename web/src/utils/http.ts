import i18n from "@/locales";
import { useAuthService } from "@/services/auth";
import axios from "axios";
import { appState } from "./app-state";
import { createLoadingCounter } from "./loading-counter";

// 定义不需要显示 loading 的 API 地址列表
const noLoadingUrls = ["/tasks/status"];

declare module "axios" {
  interface AxiosRequestConfig {
    hideMessage?: boolean;
    trackGlobalLoading?: boolean;
  }
}

const globalLoadingCounter = createLoadingCounter(active => {
  appState.loading = active;
});

function beginGlobalLoading() {
  globalLoadingCounter.begin();
}

function finishGlobalLoading(config?: { trackGlobalLoading?: boolean }) {
  if (!config?.trackGlobalLoading) {
    return;
  }

  config.trackGlobalLoading = false;
  globalLoadingCounter.finish();
}

const http = axios.create({
  baseURL: "/api",
  timeout: 60000,
  headers: { "Content-Type": "application/json" },
});

// 请求拦截器
http.interceptors.request.use(config => {
  // 检查当前请求的 URL 是否在屏蔽列表中
  if (config.url && !noLoadingUrls.includes(config.url)) {
    config.trackGlobalLoading = true;
    beginGlobalLoading();
  }
  const authKey = localStorage.getItem("authKey");
  if (authKey) {
    config.headers.Authorization = `Bearer ${authKey}`;
  }
  // 添加语言头
  const locale = localStorage.getItem("locale") || "zh-CN";
  config.headers["Accept-Language"] = locale;
  return config;
});

// 响应拦截器
http.interceptors.response.use(
  response => {
    finishGlobalLoading(response.config);
    if (response.config.method !== "get" && !response.config.hideMessage) {
      window.$message.success(response.data.message ?? i18n.global.t("common.operationSuccess"));
    }
    return response.data;
  },
  error => {
    finishGlobalLoading(error.config);
    const showMessage = !error.config?.hideMessage;

    if (error.response) {
      if (error.response.status === 401) {
        if (window.location.pathname !== "/login") {
          const { logout } = useAuthService();
          logout();
          window.location.href = "/login";
        }
      }
      if (showMessage) {
        window.$message.error(
          error.response.data?.message ||
            i18n.global.t("common.requestFailed", { status: error.response.status }),
          {
            keepAliveOnHover: true,
            duration: 5000,
            closable: true,
          }
        );
      }
    } else if (error.request && showMessage) {
      window.$message.error(i18n.global.t("common.networkError"));
    } else if (showMessage) {
      window.$message.error(i18n.global.t("common.requestSetupError"));
    }
    return Promise.reject(error);
  }
);

export default http;
