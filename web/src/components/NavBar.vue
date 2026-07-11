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

const items = [
  { key: "dashboard", label: "nav.dashboard", icon: BarChartOutline },
  { key: "keys", label: "nav.keys", icon: KeyOutline },
  { key: "logs", label: "nav.logs", icon: ListOutline },
  { key: "settings", label: "nav.settings", icon: SettingsOutline },
] as const;

const menuOptions = computed<MenuOption[]>(() =>
  items.map(item => ({
    key: item.key,
    icon: () => h(NIcon, { component: item.icon, size: 17 }),
    label: () =>
      h(
        RouterLink,
        { to: { name: item.key }, class: "nav-menu-item" },
        { default: () => t(item.label) }
      ),
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
  padding: 3px;
  border: 1px solid var(--border-color-light);
  border-radius: 12px;
  background: rgba(118, 118, 128, 0.08);
}

:deep(.n-menu-item) {
  margin: 0 !important;
  border-radius: 9px;
}

:deep(.n-menu-item-content) {
  padding: 0 0.78rem !important;
  border-radius: 9px !important;
}

:deep(.n-menu-item-content::before) {
  border-radius: 9px !important;
}

:deep(.n-menu-item-content--selected::before) {
  background: var(--card-bg-solid) !important;
  box-shadow: var(--shadow-sm);
}

:deep(.n-menu-item-content--selected .n-menu-item-content-header),
:deep(.n-menu-item-content--selected .n-menu-item-content__icon) {
  color: var(--text-primary) !important;
}

:deep(.nav-menu-item) {
  color: inherit;
  font-size: 0.84rem;
  font-weight: 590;
  text-decoration: none;
}

:deep(.n-menu--vertical) {
  width: 100%;
}

:deep(.n-menu--vertical .n-menu-item) {
  margin-bottom: 0.3rem !important;
}

:deep(.n-menu--vertical .n-menu-item-content) {
  min-height: 44px;
  padding: 0 0.875rem !important;
}

@media (prefers-contrast: more) {
  .app-menu {
    border-width: 2px;
  }
}
</style>
