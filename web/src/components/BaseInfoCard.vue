<script setup lang="ts">
import type { DashboardStatsResponse } from "@/types/models";
import { NCard, NGrid, NGridItem, NSpace, NTag, NTooltip } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();

// Props
interface Props {
  stats: DashboardStatsResponse | null;
}

const props = defineProps<Props>();

// 使用计算属性代替ref
const stats = computed(() => props.stats);
const animatedValues = ref<Record<string, number>>({});

// 格式化数值显示
const formatValue = (value: number, type: "count" | "rate" = "count"): string => {
  if (type === "rate") {
    return `${value.toFixed(1)}%`;
  }
  if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}K`;
  }
  return value.toString();
};

// 格式化趋势显示
const formatTrend = (trend: number): string => {
  const sign = trend >= 0 ? "+" : "";
  return `${sign}${trend.toFixed(1)}%`;
};

const updateAnimatedValues = () => {
  if (stats.value) {
    const totalKeys = (stats.value.key_count?.value ?? 0) + (stats.value.key_count?.sub_value ?? 0);
    animatedValues.value = {
      key_count: totalKeys > 0 ? (stats.value.key_count?.value ?? 0) / totalKeys : 0,
      rpm: Math.min(Math.max(100 + (stats.value.rpm?.trend ?? 0), 0), 100) / 100,
      request_count:
        Math.min(Math.max(100 + (stats.value.request_count?.trend ?? 0), 0), 100) / 100,
      error_rate: Math.min(Math.max(100 - (stats.value.error_rate?.value ?? 0), 0), 100) / 100,
    };
  } else {
    animatedValues.value = {};
  }
};

watch(stats, updateAnimatedValues, { immediate: true });
</script>

<template>
  <div class="stats-container">
    <n-space vertical size="medium">
      <n-grid cols="2 s:4" :x-gap="20" :y-gap="20" responsive="screen">
        <!-- 密钥数量 -->
        <n-grid-item span="1">
          <n-card :bordered="false" class="stat-card">
            <div class="stat-header">
              <div class="stat-icon key-icon">🔑</div>
              <n-tooltip v-if="stats?.key_count.sub_value" trigger="hover">
                <template #trigger>
                  <n-tag type="error" size="small" class="stat-trend">
                    {{ stats.key_count.sub_value }}
                  </n-tag>
                </template>
                {{ stats.key_count.sub_value_tip }}
              </n-tooltip>
            </div>

            <div class="stat-content">
              <div class="stat-value">
                {{ stats?.key_count?.value ?? 0 }}
              </div>
              <div class="stat-title">{{ t("dashboard.totalKeys") }}</div>
            </div>

            <div class="stat-bar">
              <div
                class="stat-bar-fill key-bar"
                :style="{
                  width: `${(animatedValues.key_count ?? 0) * 100}%`,
                }"
              />
            </div>
          </n-card>
        </n-grid-item>

        <!-- RPM (10分钟) -->
        <n-grid-item span="1">
          <n-card :bordered="false" class="stat-card">
            <div class="stat-header">
              <div class="stat-icon rpm-icon">⏱️</div>
              <n-tag
                v-if="stats?.rpm && stats.rpm.trend !== undefined"
                :type="stats?.rpm.trend_is_growth ? 'success' : 'error'"
                size="small"
                class="stat-trend"
              >
                {{ stats ? formatTrend(stats.rpm.trend) : "--" }}
              </n-tag>
            </div>

            <div class="stat-content">
              <div class="stat-value">
                {{ stats?.rpm?.value.toFixed(1) ?? 0 }}
              </div>
              <div class="stat-title">{{ t("dashboard.rpm10Min") }}</div>
            </div>

            <div class="stat-bar">
              <div
                class="stat-bar-fill rpm-bar"
                :style="{
                  width: `${(animatedValues.rpm ?? 0) * 100}%`,
                }"
              />
            </div>
          </n-card>
        </n-grid-item>

        <!-- 24小时请求 -->
        <n-grid-item span="1">
          <n-card :bordered="false" class="stat-card">
            <div class="stat-header">
              <div class="stat-icon request-icon">📈</div>
              <n-tag
                v-if="stats?.request_count && stats.request_count.trend !== undefined"
                :type="stats?.request_count.trend_is_growth ? 'success' : 'error'"
                size="small"
                class="stat-trend"
              >
                {{ stats ? formatTrend(stats.request_count.trend) : "--" }}
              </n-tag>
            </div>

            <div class="stat-content">
              <div class="stat-value">
                {{ stats ? formatValue(stats.request_count.value) : "--" }}
              </div>
              <div class="stat-title">{{ t("dashboard.requests24h") }}</div>
            </div>

            <div class="stat-bar">
              <div
                class="stat-bar-fill request-bar"
                :style="{
                  width: `${(animatedValues.request_count ?? 0) * 100}%`,
                }"
              />
            </div>
          </n-card>
        </n-grid-item>

        <!-- 24小时错误率 -->
        <n-grid-item span="1">
          <n-card :bordered="false" class="stat-card">
            <div class="stat-header">
              <div class="stat-icon error-icon">🛡️</div>
              <n-tag
                v-if="stats?.error_rate.trend !== 0"
                :type="stats?.error_rate.trend_is_growth ? 'success' : 'error'"
                size="small"
                class="stat-trend"
              >
                {{ stats ? formatTrend(stats.error_rate.trend) : "--" }}
              </n-tag>
            </div>

            <div class="stat-content">
              <div class="stat-value">
                {{ stats ? formatValue(stats.error_rate.value ?? 0, "rate") : "--" }}
              </div>
              <div class="stat-title">{{ t("dashboard.errorRate24h") }}</div>
            </div>

            <div class="stat-bar">
              <div
                class="stat-bar-fill error-bar"
                :style="{
                  width: `${(animatedValues.error_rate ?? 0) * 100}%`,
                }"
              />
            </div>
          </n-card>
        </n-grid-item>
      </n-grid>
    </n-space>
  </div>
</template>

<style scoped>
.stats-container {
  width: 100%;
}

.stat-card {
  background: var(--card-bg-solid);
  border-radius: var(--border-radius-lg);
  border: 1px solid var(--border-color-light);
  position: relative;
  overflow: hidden;
  box-shadow: var(--shadow-sm);
  transition:
    transform var(--motion-fast) var(--ease-out),
    box-shadow var(--motion-fast) var(--ease-out);
}

.stat-card:hover {
  transform: translateY(-1px);
  box-shadow: var(--shadow-md);
}

.stat-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.stat-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--border-radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.4rem;
  background: var(--primary-color-suppl);
  color: var(--primary-color);
}

.key-icon {
  background: rgba(0, 113, 227, 0.1);
}

.rpm-icon {
  background: rgba(255, 159, 10, 0.12);
}

.request-icon {
  background: rgba(90, 200, 250, 0.14);
}

.error-icon {
  background: var(--success-bg);
}

.stat-trend {
  font-weight: 600;
}

.stat-trend:before {
  content: "";
  display: inline-block;
  width: 0;
  height: 0;
  margin-right: 4px;
  vertical-align: middle;
}

.stat-content {
  margin-bottom: 16px;
}

.stat-value {
  font-size: clamp(1.75rem, 3vw, 2.2rem);
  font-weight: 700;
  line-height: 1.08;
  color: var(--text-primary);
  margin-bottom: 4px;
  letter-spacing: -0.035em;
  font-variant-numeric: tabular-nums;
}

.stat-title {
  font-size: 0.95rem;
  color: var(--text-secondary);
  font-weight: 500;
}

.stat-bar {
  width: 100%;
  height: 4px;
  background: var(--border-color);
  border-radius: 2px;
  overflow: hidden;
  position: relative;
}

.stat-bar-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.4s var(--ease-out);
}

.key-bar {
  background: #0071e3;
}

.rpm-bar {
  background: #ff9f0a;
}

.request-bar {
  background: #5ac8fa;
}

.error-bar {
  background: #248a3d;
}

@media (prefers-reduced-motion: reduce) {
  .stat-card,
  .stat-bar-fill {
    transition: none;
  }
}

/* 响应式网格 */
:deep(.n-grid-item) {
  min-width: 0;
}
</style>
