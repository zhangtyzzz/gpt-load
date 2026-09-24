<script setup lang="ts">
import { BarChartOutline, KeyOutline, ListOutline, SettingsOutline } from "@vicons/ionicons5";
import { NIcon, NMenu, type MenuOption } from "naive-ui";
import { computed, h, watch } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
const props = withDefaults(defineProps<{ mode?: "horizontal" | "vertical" }>(), {
  mode: "horizontal",
});
const emit = defineEmits<{ close: [] }>();
const route = useRoute();
const activeMenu = computed(() => String(route.name || "dashboard"));

const sections = [
  {
    key: "overview",
    label: "nav.section.overview",
    items: [{ key: "dashboard", label: "nav.dashboard", icon: BarChartOutline }],
  },
  {
    key: "management",
    label: "nav.section.management",
    items: [
      { key: "keys", label: "nav.keys", icon: KeyOutline },
      { key: "logs", label: "nav.logs", icon: ListOutline },
    ],
  },
  {
    key: "system",
    label: "nav.section.system",
    items: [{ key: "settings", label: "nav.settings", icon: SettingsOutline }],
  },
] as const;

const menuOptions = computed<MenuOption[]>(() =>
  sections.map(section => ({
    key: section.key,
    type: "group" as const,
    label: () => t(section.label),
    children: section.items.map(item => ({
      key: item.key,
      icon: () => h(NIcon, { component: item.icon, size: 17 }),
      label: () =>
        h(
          RouterLink,
          { to: { name: item.key }, class: "nav-menu-item" },
          { default: () => t(item.label) }
        ),
    })),
  }))
);

watch(activeMenu, () => {
  if (props.mode === "vertical") {
    emit("close");
  }
});
</script>

<template>
  <n-menu :mode="mode" :options="menuOptions" :value="activeMenu" class="app-menu" />
</template>

<style scoped>
.app-menu {
  background: transparent;
}

:deep(.n-menu-item) {
  margin: 0 !important;
  border-radius: 6px;
}

:deep(.n-menu-item-content) {
  padding: 0 0.75rem !important;
  border-radius: 6px !important;
}

:deep(.n-menu-item-content::before) {
  border-radius: 6px !important;
  left: 0 !important;
  right: 0 !important;
}

:deep(.n-menu-item-content:hover::before) {
  background: var(--hover-bg) !important;
}

:deep(.n-menu-item-content--selected::before) {
  background: var(--accent-soft) !important;
  box-shadow: none;
}

:deep(.n-menu-item-content--selected .n-menu-item-content-header),
:deep(.n-menu-item-content--selected .n-menu-item-content__icon) {
  color: var(--primary-color) !important;
}

:deep(.n-menu-item-content-header) {
  color: var(--text-secondary);
}

:deep(.n-menu-item-content__icon) {
  color: var(--text-secondary);
}

:deep(.n-menu-item-group-title) {
  padding: 14px 0.75rem 5px !important;
  color: var(--text-tertiary);
  font-size: 0.66rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

:deep(.nav-menu-item) {
  color: inherit;
  font-size: 0.85rem;
  font-weight: 500;
  text-decoration: none;
}

:deep(.n-menu--horizontal .n-menu-item),
:deep(.n-menu--horizontal .n-menu-item-content) {
  border-radius: 6px !important;
}

:deep(.n-menu--vertical) {
  width: 100%;
}

:deep(.n-menu--vertical .n-menu-item) {
  margin-bottom: 2px !important;
}

:deep(.n-menu--vertical .n-menu-item-content) {
  min-height: 40px;
}

@media (prefers-contrast: more) {
  :deep(.n-menu-item-content--selected::before) {
    outline: 1px solid var(--primary-color);
  }
}
</style>
