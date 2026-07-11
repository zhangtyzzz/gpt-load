<script setup lang="ts">
import { useAuthService } from "@/services/auth";
import { LogOutOutline } from "@vicons/ionicons5";
import { NButton, NIcon } from "naive-ui";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

const { t } = useI18n();

const router = useRouter();
const { beginLogout, cancelLogout, logout } = useAuthService();

const handleLogout = async () => {
  beginLogout();
  try {
    const navigationFailure = await router.replace("/login");
    if (navigationFailure) {
      cancelLogout();
      return;
    }
    logout();
  } catch (error) {
    cancelLogout();
    throw error;
  }
};
</script>

<template>
  <n-button quaternary round class="logout-button" @click="handleLogout">
    <template #icon>
      <n-icon :component="LogOutOutline" />
    </template>
    {{ t("nav.logout") }}
  </n-button>
</template>

<style scoped>
.logout-button {
  color: var(--text-secondary);
  background: transparent;
  border: 1px solid var(--border-color-light);
  transition:
    color var(--motion-fast) var(--ease-out),
    background-color var(--motion-fast) var(--ease-out),
    border-color var(--motion-fast) var(--ease-out),
    transform var(--motion-fast) var(--ease-out);
  font-weight: 500;
}

.logout-button:hover {
  color: var(--error-color);
  background: var(--error-bg);
  border-color: var(--error-border);
  transform: translateY(-1px);
}

:deep(.n-button__content) {
  gap: 6px;
}
</style>
