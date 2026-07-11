<script setup lang="ts">
import { appState } from "@/utils/app-state";
import { actualTheme } from "@/utils/theme";
import { getLocale } from "@/locales";
import { darkThemeOverrides, lightThemeOverrides } from "@/theme/naive";
import {
  darkTheme,
  NConfigProvider,
  NDialogProvider,
  NLoadingBarProvider,
  NMessageProvider,
  useLoadingBar,
  useMessage,
  type GlobalTheme,
  zhCN,
  enUS,
  jaJP,
  dateZhCN,
  dateEnUS,
  dateJaJP,
} from "naive-ui";
import { computed, defineComponent, watch } from "vue";

const themeOverrides = computed(() =>
  actualTheme.value === "dark" ? darkThemeOverrides : lightThemeOverrides
);

// 根据当前主题动态返回主题对象
const theme = computed<GlobalTheme | undefined>(() => {
  return actualTheme.value === "dark" ? darkTheme : undefined;
});

// 根据当前语言返回对应的 locale 配置
const locale = computed(() => {
  const currentLocale = getLocale();
  switch (currentLocale) {
    case "zh-CN":
      return zhCN;
    case "en-US":
      return enUS;
    case "ja-JP":
      return jaJP;
    default:
      return zhCN;
  }
});

// 根据当前语言返回对应的日期 locale 配置
const dateLocale = computed(() => {
  const currentLocale = getLocale();
  switch (currentLocale) {
    case "zh-CN":
      return dateZhCN;
    case "en-US":
      return dateEnUS;
    case "ja-JP":
      return dateJaJP;
    default:
      return dateZhCN;
  }
});

function useGlobalMessage() {
  window.$message = useMessage();
}

const LoadingBar = defineComponent({
  setup() {
    const loadingBar = useLoadingBar();
    watch(
      () => appState.loading,
      loading => {
        if (loading) {
          loadingBar.start();
        } else {
          loadingBar.finish();
        }
      }
    );
    return () => null;
  },
});

const Message = defineComponent({
  setup() {
    useGlobalMessage();
    return () => null;
  },
});
</script>

<template>
  <n-config-provider
    :theme="theme"
    :theme-overrides="themeOverrides"
    :locale="locale"
    :date-locale="dateLocale"
  >
    <n-loading-bar-provider>
      <n-message-provider placement="top-right">
        <n-dialog-provider>
          <slot />
          <loading-bar />
          <message />
        </n-dialog-provider>
      </n-message-provider>
    </n-loading-bar-provider>
  </n-config-provider>
</template>
