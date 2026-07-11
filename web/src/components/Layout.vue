<script setup lang="ts">
import AppFooter from "@/components/AppFooter.vue";
import GlobalTaskProgressBar from "@/components/GlobalTaskProgressBar.vue";
import LanguageSelector from "@/components/LanguageSelector.vue";
import Logout from "@/components/Logout.vue";
import NavBar from "@/components/NavBar.vue";
import ThemeToggle from "@/components/ThemeToggle.vue";
import { MenuOutline } from "@vicons/ionicons5";
import { useMediaQuery } from "@vueuse/core";
import { NButton, NDrawer, NDrawerContent, NIcon, NLayout, NLayoutContent } from "naive-ui";
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
  <n-layout class="main-layout">
    <a class="skip-link" href="#main-content">{{ t("common.skipToContent") }}</a>

    <header class="layout-header material-chrome">
      <div class="header-content">
        <router-link class="header-brand interactive" :to="{ name: 'dashboard' }">
          <span class="brand-icon" aria-hidden="true">
            <img src="@/assets/logo-256.png" alt="" />
          </span>
          <span class="brand-copy">
            <strong>GPT Load</strong>
            <small>{{ t("common.console") }}</small>
          </span>
        </router-link>

        <nav v-if="!isMobile" class="header-nav" :aria-label="t('common.primaryNavigation')">
          <nav-bar />
        </nav>

        <div class="header-actions">
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

    <n-layout-content id="main-content" class="layout-content" tabindex="-1">
      <div class="content-wrapper">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </div>
    </n-layout-content>

    <app-footer />
  </n-layout>

  <global-task-progress-bar />
</template>

<style scoped>
.main-layout {
  display: flex;
  min-height: 100vh;
  flex-direction: column;
  background: transparent;
}

.skip-link {
  position: fixed;
  top: 0.75rem;
  left: 0.75rem;
  z-index: 1000;
  padding: 0.625rem 0.875rem;
  border-radius: 0.625rem;
  background: var(--card-bg-solid);
  box-shadow: var(--shadow-md);
  color: var(--primary-color);
  font-weight: 650;
  transform: translateY(-200%);
}

.skip-link:focus {
  transform: translateY(0);
}

.layout-header {
  position: sticky;
  top: 0;
  z-index: 100;
  border-width: 0 0 1px;
}

.header-content {
  display: grid;
  width: min(100%, 1440px);
  min-height: 64px;
  padding: 0 1.5rem;
  margin: 0 auto;
  align-items: center;
  grid-template-columns: minmax(12rem, 1fr) auto minmax(12rem, 1fr);
}

.header-brand {
  display: inline-flex;
  width: fit-content;
  min-width: 0;
  align-items: center;
  gap: 0.625rem;
  border-radius: 0.75rem;
  color: var(--text-primary);
  text-decoration: none;
}

.brand-icon {
  display: grid;
  width: 2.25rem;
  height: 2.25rem;
  overflow: hidden;
  place-items: center;
  border: 1px solid var(--border-color-light);
  border-radius: 0.7rem;
  background: var(--card-bg-solid);
  box-shadow: var(--shadow-sm);
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
  font-size: 0.96rem;
  font-weight: 700;
  letter-spacing: -0.015em;
}

.brand-copy small {
  margin-top: 0.25rem;
  color: var(--text-tertiary);
  font-size: 0.68rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.header-nav {
  justify-self: center;
}

.header-actions {
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
  width: 100%;
  max-width: 1440px;
  flex: 1;
  margin: 0 auto;
  background: transparent;
}

.content-wrapper {
  min-height: calc(100vh - 116px);
  padding: 1.75rem 1.5rem 2.5rem;
}

@media (max-width: 820px) {
  .header-content {
    min-height: 58px;
    padding: 0 1rem;
    grid-template-columns: 1fr auto;
  }

  .brand-copy small {
    display: none;
  }

  .content-wrapper {
    min-height: calc(100vh - 106px);
    padding: 1.25rem 1rem 2rem;
  }
}

@media (max-width: 420px) {
  .header-content {
    padding: 0 0.75rem;
  }

  .brand-copy {
    display: none;
  }

  .header-actions {
    gap: 0;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .layout-header {
    background: var(--card-bg-solid);
    backdrop-filter: none;
  }
}
</style>
