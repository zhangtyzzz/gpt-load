<script setup lang="ts">
import AppFooter from "@/components/AppFooter.vue";
import BrandMark from "@/components/BrandMark.vue";
import LanguageSelector from "@/components/LanguageSelector.vue";
import ThemeToggle from "@/components/ThemeToggle.vue";
import { useAuthService } from "@/services/auth";
import { KeyOutline, LockClosedOutline, ShieldCheckmarkOutline } from "@vicons/ionicons5";
import { NButton, NIcon, NInput, useMessage } from "naive-ui";
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

const authKey = ref("");
const loading = ref(false);
const router = useRouter();
const message = useMessage();
const { login } = useAuthService();
const { t } = useI18n();

const handleLogin = async () => {
  if (!authKey.value) {
    message.error(t("login.authKeyRequired"));
    return;
  }
  loading.value = true;
  const success = await login(authKey.value);
  loading.value = false;
  if (success) {
    void router.push("/");
  }
};
</script>

<template>
  <main class="login-page">
    <div class="login-toolbar material-chrome">
      <language-selector />
      <theme-toggle />
    </div>

    <section class="login-shell" aria-labelledby="login-title">
      <div class="login-panel surface-card">
        <div class="panel-brand">
          <span class="brand-logo" aria-hidden="true">
            <brand-mark :size="34" />
          </span>
          <strong>GPT Load</strong>
        </div>

        <div class="panel-icon" aria-hidden="true">
          <n-icon :component="LockClosedOutline" :size="22" />
        </div>
        <div class="panel-heading">
          <h2 id="login-title">{{ t("login.welcome") }}</h2>
          <p>{{ t("login.welcomeDesc") }}</p>
        </div>

        <form class="login-form" @submit.prevent="handleLogin">
          <label class="input-label" for="auth-key">{{ t("login.authKey") }}</label>
          <n-input
            id="auth-key"
            v-model:value="authKey"
            type="password"
            size="large"
            show-password-on="click"
            :placeholder="t('login.authKeyPlaceholder')"
            :input-props="{ autocomplete: 'current-password', name: 'auth-key' }"
          >
            <template #prefix><n-icon :component="KeyOutline" /></template>
          </n-input>

          <n-button
            class="login-button"
            type="primary"
            size="large"
            block
            attr-type="submit"
            :loading="loading"
            :disabled="loading"
          >
            {{ t("login.loginButton") }}
          </n-button>
        </form>

        <div class="trust-note">
          <n-icon :component="ShieldCheckmarkOutline" :size="16" />
          <span>{{ t("login.secureAccess") }}</span>
        </div>
      </div>
    </section>
  </main>
  <app-footer />
</template>

<style scoped>
.login-page {
  position: relative;
  display: grid;
  min-height: calc(100vh - 52px);
  padding: clamp(1.5rem, 4vw, 3rem);
  place-items: center;
}

.login-toolbar {
  position: absolute;
  top: 1rem;
  right: 1rem;
  z-index: 2;
  display: flex;
  padding: 0.2rem;
  border-radius: var(--border-radius-md);
}

.login-shell {
  display: grid;
  width: min(100%, 400px);
  justify-items: stretch;
}

.login-panel {
  display: grid;
  padding: 32px;
  border-radius: 12px;
}

.panel-brand {
  display: flex;
  margin-bottom: 24px;
  align-items: center;
  justify-content: center;
  gap: 0.6rem;
  color: var(--text-primary);
}

.panel-brand strong {
  font-size: 1.05rem;
  font-weight: 650;
  letter-spacing: -0.02em;
}

.brand-logo {
  display: grid;
  width: 2.4rem;
  height: 2.4rem;
  place-items: center;
  color: var(--text-primary);
}

.panel-icon {
  display: grid;
  width: 2.6rem;
  height: 2.6rem;
  margin-bottom: 1rem;
  place-items: center;
  border-radius: var(--border-radius-md);
  background: var(--accent-soft);
  color: var(--primary-color);
}

.panel-heading {
  text-align: left;
}

.panel-heading h2 {
  color: var(--text-primary);
  font-size: 1.3rem;
  font-weight: 600;
  letter-spacing: -0.03em;
  text-wrap: balance;
}

.panel-heading p {
  margin-top: 0.4rem;
  color: var(--text-secondary);
  font-size: 0.875rem;
  line-height: 1.5;
}

.login-form {
  display: grid;
  gap: 0.8rem;
  margin-top: 1.75rem;
}

.input-label {
  color: var(--text-primary);
  font-size: 0.82rem;
  font-weight: 550;
}

.login-button {
  margin-top: 0.6rem;
}

.trust-note {
  display: flex;
  width: fit-content;
  align-items: center;
  gap: 0.45rem;
  margin-top: 1.5rem;
  color: var(--text-secondary);
  font-size: 0.78rem;
  font-weight: 500;
}

.trust-note :deep(.n-icon) {
  color: var(--success-color);
}

@media (max-width: 480px) {
  .login-panel {
    padding: 24px 20px;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .login-toolbar {
    background: var(--card-bg-solid);
    backdrop-filter: none;
  }
}
</style>
