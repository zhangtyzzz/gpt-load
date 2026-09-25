<script setup lang="ts">
import { KeyRound, ScrollText, Settings2, House } from "@lucide/vue";
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
  { key: "dashboard", label: "nav.dashboard", icon: House },
  { key: "keys", label: "nav.keys", icon: KeyRound },
  { key: "logs", label: "nav.logs", icon: ScrollText },
  { key: "settings", label: "nav.settings", icon: Settings2 },
] as const;

const menuOptions = computed<MenuOption[]>(() =>
  items.map(item => ({
    key: item.key,
    icon: () => h(NIcon, { component: item.icon, size: 17, "stroke-width": 1.75 }),
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
  color: var(--text-primary) !important;
}

:deep(.n-menu-item-content-header) {
  color: var(--text-secondary);
}

:deep(.n-menu-item-content__icon) {
  color: var(--text-secondary);
}

:deep(.nav-menu-item) {
  color: inherit;
  font-size: 0.85rem;
  font-weight: 500;
  text-decoration: none;
}

:deep(.n-menu--horizontal) {
  padding: 3px;
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
  background: var(--card-bg-solid);
}

:deep(.n-menu--horizontal .n-menu-item) {
  margin-bottom: 0 !important;
}

:deep(.n-menu--horizontal .n-menu-item-content) {
  min-height: 34px;
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
