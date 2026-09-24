<script setup lang="ts">
import AppFooter from "@/components/AppFooter.vue";
import GlobalTaskProgressBar from "@/components/GlobalTaskProgressBar.vue";
import LanguageSelector from "@/components/LanguageSelector.vue";
import Logout from "@/components/Logout.vue";
import NavBar from "@/components/NavBar.vue";
import ThemeToggle from "@/components/ThemeToggle.vue";
import { MenuOutline } from "@vicons/ionicons5";
import { useMediaQuery } from "@vueuse/core";
import { NButton, NDrawer, NDrawerContent, NIcon } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
const isMenuOpen = ref(false);
const isMobile = useMediaQuery("(max-width: 820px)");
const drawerWidth = computed(() => Math.min(340, globalThis.innerWidth));

watch(isMobile, value => {
  if (!value) {
    isMenuOpen.value = false;
  }
});
</script>

<template>
  <div class="app-shell">
    <a class="skip-link" href="#main-content">{{ t("common.skipToContent") }}</a>

    <aside class="app-sidebar">
      <router-link class="sidebar-brand interactive" :to="{ name: 'dashboard' }">
        <span class="brand-icon" aria-hidden="true">
          <img src="@/assets/logo-256.png" alt="" />
        </span>
        <span class="brand-copy">
          <strong>GPT Load</strong>
          <small>{{ t("common.console") }}</small>
        </span>
      </router-link>

      <nav class="sidebar-nav" :aria-label="t('common.primaryNavigation')">
        <nav-bar mode="vertical" />
      </nav>
    </aside>

    <div class="app-main">
      <header class="app-topbar material-chrome">
        <router-link v-if="isMobile" class="topbar-brand interactive" :to="{ name: 'dashboard' }">
          <span class="brand-icon" aria-hidden="true">
            <img src="@/assets/logo-256.png" alt="" />
          </span>
          <span class="brand-copy">
            <strong>GPT Load</strong>
          </span>
        </router-link>

        <div class="topbar-actions">
          <language-selector />
          <theme-toggle />
          <logout v-if="!isMobile" />
          <n-button
            v-if="isMobile"
            quaternary
            circle
            :aria-label="t('common.openNavigation')"
            @click="isMenuOpen = true"
          >
            <template #icon><n-icon :component="MenuOutline" /></template>
          </n-button>
        </div>
      </header>

      <main id="main-content" class="layout-content" tabindex="-1">
        <div class="content-wrapper">
          <router-view v-slot="{ Component }">
            <transition name="fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </div>
        <app-footer />
      </main>
    </div>

    <n-drawer v-model:show="isMenuOpen" :width="drawerWidth" placement="right">
      <n-drawer-content
        :title="t('common.navigation')"
        closable
        body-content-style="padding: 12px; display: flex; flex-direction: column; height: 100%;"
      >
        <nav :aria-label="t('common.primaryNavigation')">
          <nav-bar mode="vertical" @close="isMenuOpen = false" />
        </nav>
        <div class="mobile-actions">
          <logout />
        </div>
      </n-drawer-content>
    </n-drawer>
  </div>

  <global-task-progress-bar />
</template>

<style scoped>
.app-shell {
  display: grid;
  min-height: 100vh;
  grid-template-columns: 240px minmax(0, 1fr);
}

.skip-link {
  position: fixed;
  top: 0.75rem;
  left: 0.75rem;
  z-index: 1000;
  padding: 0.625rem 0.875rem;
  border-radius: var(--border-radius-md);
  background: var(--card-bg-solid);
  box-shadow: var(--shadow-lg);
  color: var(--primary-color);
  font-weight: 600;
  transform: translateY(-200%);
}

.skip-link:focus {
  transform: translateY(0);
}

.app-sidebar {
  position: sticky;
  top: 0;
  display: flex;
  height: 100vh;
  flex-direction: column;
  padding: 20px 16px;
  border-right: 1px solid var(--border-color-light);
  background: var(--sidebar-bg);
  overflow-y: auto;
}

.sidebar-brand {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 4px 8px 20px;
  color: var(--text-primary);
  text-decoration: none;
}

.brand-icon {
  display: grid;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  overflow: hidden;
  place-items: center;
  border: 1px solid var(--border-color-light);
  border-radius: 9px;
  background: var(--card-bg-solid);
}

.brand-icon img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.brand-copy {
  display: grid;
  line-height: 1.05;
}

.brand-copy strong {
  font-size: 0.95rem;
  font-weight: 650;
  letter-spacing: -0.02em;
}

.brand-copy small {
  margin-top: 0.25rem;
  color: var(--text-secondary);
  font-size: 0.66rem;
  font-weight: 550;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.sidebar-nav {
  display: flex;
  flex: 1;
  flex-direction: column;
}

.app-main {
  display: flex;
  min-width: 0;
  flex-direction: column;
}

.app-topbar {
  position: sticky;
  top: 0;
  z-index: 100;
  display: flex;
  min-height: 56px;
  align-items: center;
  justify-content: flex-end;
  padding: 0 24px;
  border-bottom: 1px solid var(--border-color-light);
}

.topbar-brand {
  display: inline-flex;
  margin-right: auto;
  align-items: center;
  gap: 0.5rem;
  color: var(--text-primary);
  text-decoration: none;
}

.topbar-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}

.mobile-actions {
  display: flex;
  padding-top: 1rem;
  margin-top: auto;
  border-top: 1px solid var(--border-color-light);
}

.layout-content {
  display: flex;
  width: 100%;
  flex: 1;
  flex-direction: column;
  background: transparent;
}

.content-wrapper {
  flex: 1;
  padding: 28px 32px 40px;
}

@media (max-width: 1150px) {
  .content-wrapper {
    padding: 24px;
  }
}

@media (max-width: 820px) {
  .app-shell {
    grid-template-columns: minmax(0, 1fr);
  }

  .app-sidebar {
    display: none;
  }

  .app-topbar {
    padding: 0 12px;
  }

  .content-wrapper {
    padding: 20px 16px 28px;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .app-topbar {
    background: var(--card-bg-solid);
    backdrop-filter: none;
  }
}
</style>
