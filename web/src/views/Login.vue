<script setup lang="ts">
import AppFooter from "@/components/AppFooter.vue";
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
      <div class="login-story">
        <div class="story-brand">
          <span class="story-logo"><img src="@/assets/logo-256.png" alt="" /></span>
          <span>GPT Load</span>
        </div>
        <div class="story-copy">
          <p class="story-eyebrow">{{ t("common.console") }}</p>
          <h1 id="login-title">{{ t("login.subtitle") }}</h1>
          <p>{{ t("login.welcomeDesc") }}</p>
        </div>
        <div class="trust-note">
          <n-icon :component="ShieldCheckmarkOutline" :size="20" />
          <span>{{ t("login.secureAccess") }}</span>
        </div>
      </div>

      <div class="login-panel surface-card">
        <div class="panel-icon" aria-hidden="true">
          <n-icon :component="LockClosedOutline" :size="24" />
        </div>
        <div class="panel-heading">
          <h2>{{ t("login.welcome") }}</h2>
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
  padding: clamp(1rem, 4vw, 3rem);
  place-items: center;
  overflow: hidden;
}

.login-page::before {
  position: absolute;
  width: min(70vw, 56rem);
  height: min(70vw, 56rem);
  border-radius: 50%;
  background: radial-gradient(circle, rgba(0, 113, 227, 0.1) 0%, transparent 68%);
  content: "";
  inset: -28rem auto auto -24rem;
  pointer-events: none;
}

.login-toolbar {
  position: absolute;
  top: 1rem;
  right: 1rem;
  z-index: 2;
  display: flex;
  padding: 0.2rem;
  border-radius: 12px;
}

.login-shell {
  position: relative;
  z-index: 1;
  display: grid;
  width: min(100%, 960px);
  align-items: center;
  gap: clamp(3rem, 9vw, 8rem);
  grid-template-columns: minmax(0, 1fr) minmax(320px, 390px);
}

.login-story {
  display: grid;
  gap: 2.5rem;
}

.story-brand {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  color: var(--text-primary);
  font-weight: 700;
  letter-spacing: -0.015em;
}

.story-logo {
  display: grid;
  width: 2.65rem;
  height: 2.65rem;
  overflow: hidden;
  place-items: center;
  border: 1px solid var(--border-color-light);
  border-radius: 0.8rem;
  background: var(--card-bg-solid);
  box-shadow: var(--shadow-sm);
}

.story-logo img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.story-copy {
  max-width: 30rem;
}

.story-eyebrow {
  margin-bottom: 0.7rem;
  color: var(--primary-color) !important;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.09em;
  text-transform: uppercase;
}

.story-copy h1 {
  margin-bottom: 1rem;
  color: var(--text-primary);
  font-size: clamp(2.5rem, 5vw, 4.35rem);
  font-weight: 730;
  letter-spacing: -0.055em;
  line-height: 0.98;
}

.story-copy p {
  color: var(--text-secondary);
  font-size: 1.03rem;
  line-height: 1.6;
}

.trust-note {
  display: flex;
  width: fit-content;
  align-items: center;
  gap: 0.55rem;
  color: var(--text-secondary);
  font-size: 0.82rem;
  font-weight: 550;
}

.trust-note :deep(.n-icon) {
  color: var(--success-color);
}

.login-panel {
  padding: 2rem;
  border-radius: 22px;
  box-shadow: var(--shadow-lg);
}

.panel-icon {
  display: grid;
  width: 3rem;
  height: 3rem;
  margin-bottom: 1.5rem;
  place-items: center;
  border-radius: 0.9rem;
  background: var(--primary-color-suppl);
  color: var(--primary-color);
}

.panel-heading h2 {
  color: var(--text-primary);
  font-size: 1.5rem;
  font-weight: 680;
  letter-spacing: -0.025em;
}

.panel-heading p {
  margin-top: 0.4rem;
  color: var(--text-secondary);
  font-size: 0.9rem;
}

.login-form {
  display: grid;
  gap: 0.8rem;
  margin-top: 1.75rem;
}

.input-label {
  color: var(--text-primary);
  font-size: 0.82rem;
  font-weight: 620;
}

.login-button {
  margin-top: 0.6rem;
}

@media (max-width: 760px) {
  .login-page {
    padding: 5rem 1rem 2rem;
  }

  .login-shell {
    max-width: 420px;
    gap: 2rem;
    grid-template-columns: 1fr;
  }

  .login-story {
    gap: 1.5rem;
    text-align: center;
  }

  .story-brand,
  .trust-note {
    margin: 0 auto;
  }

  .story-copy h1 {
    font-size: clamp(2.35rem, 11vw, 3.4rem);
  }

  .story-copy p:not(.story-eyebrow),
  .trust-note {
    display: none;
  }

  .login-panel {
    padding: 1.5rem;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .login-toolbar {
    background: var(--card-bg-solid);
    backdrop-filter: none;
  }
}
</style>
