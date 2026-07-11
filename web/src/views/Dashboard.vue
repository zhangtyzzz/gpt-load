<script setup lang="ts">
import { getDashboardStats } from "@/api/dashboard";
import BaseInfoCard from "@/components/BaseInfoCard.vue";
import EncryptionMismatchAlert from "@/components/EncryptionMismatchAlert.vue";
import LineChart from "@/components/LineChart.vue";
import SecurityAlert from "@/components/SecurityAlert.vue";
import type { DashboardStatsResponse } from "@/types/models";
import axios from "axios";
import { NAlert, NButton, NSkeleton } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
const dashboardStats = ref<DashboardStatsResponse | null>(null);
const statsState = ref<"loading" | "ready" | "error">("loading");
const healthState = ref<"loading" | "healthy" | "unhealthy">("loading");

const dashboardState = computed<"loading" | "ready" | "error">(() => {
  if (statsState.value === "loading" || healthState.value === "loading") {
    return "loading";
  }

  return statsState.value === "ready" && healthState.value === "healthy" ? "ready" : "error";
});
const dashboardErrorLabel = computed(() =>
  healthState.value === "unhealthy" ? t("dashboard.unhealthy") : t("dashboard.loadFailed")
);

async function loadDashboard() {
  statsState.value = "loading";
  healthState.value = "loading";

  const [statsResult, healthResult] = await Promise.allSettled([
    getDashboardStats(),
    axios.get<{ status?: string }>("/health", { timeout: 5000 }),
  ]);

  if (statsResult.status === "fulfilled") {
    dashboardStats.value = statsResult.value.data;
    statsState.value = "ready";
  } else {
    dashboardStats.value = null;
    statsState.value = "error";
    console.error("Failed to load dashboard stats:", statsResult.reason);
  }

  healthState.value =
    healthResult.status === "fulfilled" && healthResult.value.data.status === "healthy"
      ? "healthy"
      : "unhealthy";
}

onMounted(loadDashboard);
</script>

<template>
  <section class="page-shell dashboard-container" aria-labelledby="dashboard-title">
    <header class="page-heading">
      <div>
        <h1 id="dashboard-title" class="page-title">{{ t("dashboard.title") }}</h1>
        <p class="page-description">{{ t("dashboard.description") }}</p>
      </div>
      <div
        class="health-pill"
        :class="`health-pill--${dashboardState}`"
        role="status"
        aria-live="polite"
      >
        <span class="health-dot" aria-hidden="true" />
        <span v-if="dashboardState === 'loading'">{{ t("dashboard.checking") }}</span>
        <span v-else-if="dashboardState === 'ready'">{{ t("dashboard.ready") }}</span>
        <span v-else>{{ dashboardErrorLabel }}</span>
      </div>
    </header>

    <div class="dashboard-stack">
      <encryption-mismatch-alert />

      <n-alert
        v-if="dashboardState === 'error'"
        type="error"
        :title="dashboardErrorLabel"
        class="dashboard-state"
      >
        <div class="state-message">
          <span>{{ t("dashboard.loadFailed") }}</span>
          <n-button size="small" @click="loadDashboard">
            {{ t("common.retry") }}
          </n-button>
        </div>
      </n-alert>

      <div v-if="statsState === 'loading'" class="dashboard-skeleton surface-card" aria-busy="true">
        <n-skeleton text :repeat="5" />
      </div>

      <security-alert
        v-if="statsState === 'ready' && dashboardStats?.security_warnings"
        :warnings="dashboardStats.security_warnings"
      />
      <base-info-card v-if="statsState === 'ready'" :stats="dashboardStats" />
      <line-chart class="dashboard-chart" />
    </div>
  </section>
</template>

<style scoped>
.dashboard-stack {
  display: grid;
  gap: 1rem;
}

.health-pill {
  display: inline-flex;
  min-height: 32px;
  padding: 0.35rem 0.7rem;
  align-items: center;
  gap: 0.45rem;
  border-radius: 999px;
  font-size: 0.78rem;
  font-weight: 650;
}

.health-pill--ready {
  border: 1px solid var(--success-border);
  background: var(--success-bg);
  color: var(--success-color);
}

.health-pill--loading {
  border: 1px solid var(--border-color-light);
  background: var(--bg-secondary);
  color: var(--text-secondary);
}

.health-pill--error {
  border: 1px solid var(--error-border);
  background: var(--error-bg);
  color: var(--error-color);
}

.health-dot {
  width: 0.46rem;
  height: 0.46rem;
  border-radius: 50%;
  background: currentColor;
}

.dashboard-skeleton {
  padding: 1.5rem;
}

.state-message {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
</style>
