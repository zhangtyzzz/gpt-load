<script setup lang="ts">
import { NDropdown, NButton, NIcon } from "naive-ui";
import { computed } from "vue";
import { SUPPORTED_LOCALES, setLocale, getCurrentLocaleLabel, type Locale } from "@/locales";
import { Language } from "@vicons/ionicons5";
import { useI18n } from "vue-i18n";

const { t } = useI18n();

// 当前语言标签
const currentLabel = computed(() => getCurrentLocaleLabel());

// 下拉选项
const options = computed(() =>
  SUPPORTED_LOCALES.map(locale => ({
    label: locale.label,
    key: locale.key,
  }))
);

// 切换语言
const handleSelect = (key: string) => {
  setLocale(key as Locale);
  // 页面会自动刷新，不需要提示
};
</script>

<template>
  <n-dropdown :options="options" @select="handleSelect" trigger="click">
    <n-button
      quaternary
      size="medium"
      class="language-selector-btn"
      :aria-label="`${t('common.language')}: ${currentLabel}`"
    >
      <template #icon>
        <n-icon :component="Language" />
      </template>
      <span class="language-label">{{ currentLabel }}</span>
    </n-button>
  </n-dropdown>
</template>

<style scoped>
.language-selector-btn {
  min-width: 0;
}

@media (max-width: 420px) {
  .language-label {
    display: none;
  }
}

/* 确保在暗黑模式下有良好的对比度 */
:global(.dark) .language-selector-btn {
  color: var(--n-text-color);
}

.language-selector-btn:hover {
  color: var(--n-primary-color);
}
</style>
