<script setup lang="ts">
import AppFooter from "@/components/AppFooter.vue";
import BrandMark from "@/components/BrandMark.vue";
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

    <header class="app-topbar material-chrome">
      <div class="topbar-inner">
        <router-link class="topbar-brand interactive" :to="{ name: 'dashboard' }">
          <span class="brand-icon" aria-hidden="true">
            <brand-mark :size="30" />
          </span>
          <span class="brand-copy">
            <strong>GPT Load</strong>
            <small>{{ t("common.console") }}</small>
          </span>
        </router-link>

        <nav v-if="!isMobile" class="topbar-nav" :aria-label="t('common.primaryNavigation')">
          <nav-bar mode="horizontal" />
        </nav>

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
  display: flex;
  min-height: 100vh;
  flex-direction: column;
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

.app-topbar {
  position: sticky;
  top: 0;
  z-index: 100;
  border-bottom: 1px solid var(--border-color-light);
}

.topbar-inner {
  display: grid;
  width: min(100%, 1440px);
  min-height: 56px;
  padding: 0 24px;
  margin: 0 auto;
  align-items: center;
  grid-template-columns: minmax(9rem, auto) minmax(0, 1fr) minmax(9rem, auto);
}

.topbar-brand {
  display: inline-flex;
  width: fit-content;
  min-width: 0;
  align-items: center;
  gap: 0.5rem;
  color: var(--text-primary);
  text-decoration: none;
}

.brand-icon {
  display: grid;
  width: 2.1rem;
  height: 2.1rem;
  flex-shrink: 0;
  place-items: center;
  color: var(--text-primary);
}

.brand-copy {
  display: grid;
  line-height: 1.05;
}

.brand-copy strong {
  font-size: 0.92rem;
  font-weight: 650;
  letter-spacing: -0.02em;
}

.brand-copy small {
  margin-top: 0.2rem;
  color: var(--text-secondary);
  font-size: 0.64rem;
  font-weight: 550;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.topbar-nav {
  justify-self: center;
}

.topbar-actions {
  display: flex;
  align-items: center;
  justify-self: end;
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
  max-width: 1440px;
  flex: 1;
  margin: 0 auto;
  flex-direction: column;
  background: transparent;
}

.content-wrapper {
  flex: 1;
  padding: 28px 24px 40px;
}

@media (max-width: 1150px) {
  .content-wrapper {
    padding: 24px 16px 36px;
  }
}

@media (max-width: 820px) {
  .topbar-inner {
    min-height: 54px;
    padding: 0 12px;
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .brand-copy {
    display: none;
  }

  .content-wrapper {
    padding: 20px 16px 28px;
  }
}

@media (max-width: 420px) {
  .topbar-inner {
    padding: 0 8px;
  }

  .topbar-actions {
    gap: 0;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .app-topbar {
    background: var(--card-bg-solid);
    backdrop-filter: none;
  }
}
</style>
