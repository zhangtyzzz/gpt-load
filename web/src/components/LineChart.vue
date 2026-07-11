<script setup lang="ts">
import { getDashboardChart, getGroupList } from "@/api/dashboard";
import type { ChartData } from "@/types/models";
import { getGroupDisplayName } from "@/utils/display";
import { NButton, NSelect, NSpin } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();

// 图表数据
const chartData = ref<ChartData | null>(null);
const selectedGroup = ref<number | null>(null);
const loading = ref(true);
const chartError = ref(false);
const hoveredPoint = ref<{
  datasetIndex: number;
  pointIndex: number;
  x: number;
  y: number;
} | null>(null);
const tooltipData = ref<{
  time: string;
  datasets: Array<{
    label: string;
    value: number;
    color: string;
  }>;
} | null>(null);
const tooltipPosition = ref({ x: 0, y: 0 });
const chartSvg = ref<SVGElement>();

// 图表尺寸和边距
const chartWidth = 800;
const chartHeight = 260;
const padding = { top: 40, right: 40, bottom: 60, left: 80 };

// 格式化分组选项
const groupOptions = ref<Array<{ label: string; value: number | null }>>([]);

// 计算有效的绘图区域
const plotWidth = chartWidth - padding.left - padding.right;
const plotHeight = chartHeight - padding.top - padding.bottom;

// 计算数据的最大值和最小值
const dataRange = computed(() => {
  if (!chartData.value) {
    return { min: 0, max: 100 };
  }

  const allValues = chartData.value.datasets.flatMap(d => d.data);
  const max = Math.max(...allValues, 0);
  const min = Math.min(...allValues, 0);

  // 如果所有数据都是0，设置一个合理的范围
  if (max === 0 && min === 0) {
    return { min: 0, max: 10 };
  }

  // 添加一些padding让图表更好看
  const paddingValue = Math.max((max - min) * 0.1, 1);
  return {
    min: Math.max(0, min - paddingValue),
    max: max + paddingValue,
  };
});

// 生成Y轴刻度
const yTicks = computed(() => {
  const { min, max } = dataRange.value;
  const range = max - min;
  const tickCount = 5;
  const step = range / (tickCount - 1);

  return Array.from({ length: tickCount }, (_, i) => min + i * step);
});

// 格式化时间标签
const formatTimeLabel = (isoString: string) => {
  const date = new Date(isoString);
  return date.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
};

// 生成可见的X轴标签（避免重叠）
const visibleLabels = computed(() => {
  if (!chartData.value) {
    return [];
  }

  const labels = chartData.value.labels;
  const maxLabels = 8; // 最多显示8个标签
  const step = Math.ceil(labels.length / maxLabels);

  return labels
    .map((label, index) => ({ text: formatTimeLabel(label), index }))
    .filter(label => label.index % step === 0);
});

// 位置计算函数
const getXPosition = (index: number) => {
  if (!chartData.value) {
    return 0;
  }
  const totalPoints = chartData.value.labels.length;
  if (totalPoints <= 1) {
    return padding.left + plotWidth / 2;
  }
  return padding.left + (index / (totalPoints - 1)) * plotWidth;
};

const getYPosition = (value: number) => {
  const { min, max } = dataRange.value;
  const ratio = (value - min) / (max - min);
  return padding.top + (1 - ratio) * plotHeight;
};

// Helper to find segments of non-zero data (用于填充区域)
const getSegments = (data: number[]) => {
  const segments: Array<Array<{ value: number; index: number }>> = [];
  let currentSegment: Array<{ value: number; index: number }> = [];

  data.forEach((value, index) => {
    if (value > 0) {
      currentSegment.push({ value, index });
    } else {
      if (currentSegment.length > 0) {
        segments.push(currentSegment);
        currentSegment = [];
      }
    }
  });

  if (currentSegment.length > 0) {
    segments.push(currentSegment);
  }

  return segments;
};

// 生成线条路径（连续线条，包括0值点）
const generateLinePath = (data: number[]) => {
  if (data.length === 0) {
    return "";
  }

  // 找到第一个和最后一个非0值的位置
  let firstNonZeroIndex = -1;
  let lastNonZeroIndex = -1;

  for (let i = 0; i < data.length; i++) {
    if (data[i] > 0) {
      if (firstNonZeroIndex === -1) {
        firstNonZeroIndex = i;
      }
      lastNonZeroIndex = i;
    }
  }

  // 如果没有非0值，返回空路径
  if (firstNonZeroIndex === -1) {
    return "";
  }

  // 生成连续的路径，从第一个非0值到最后一个非0值
  const pathCommands: string[] = [];

  for (let i = firstNonZeroIndex; i <= lastNonZeroIndex; i++) {
    const x = getXPosition(i);
    const y = getYPosition(data[i]);
    const command = i === firstNonZeroIndex ? "M" : "L";
    pathCommands.push(`${command} ${x},${y}`);
  }

  return pathCommands.join(" ");
};

// 生成填充区域路径（只为有数据的区域填充）
const generateAreaPath = (data: number[]) => {
  const segments = getSegments(data);
  const pathParts: string[] = [];
  const baseY = getYPosition(dataRange.value.min);

  segments.forEach(segment => {
    if (segment.length > 0) {
      const points = segment.map(p => ({
        x: getXPosition(p.index),
        y: getYPosition(p.value),
      }));
      const firstPoint = points[0];
      const lastPoint = points[points.length - 1];

      const lineCommands = points.map(p => `L ${p.x},${p.y}`).join(" ");

      pathParts.push(`M ${firstPoint.x},${baseY} ${lineCommands} L ${lastPoint.x},${baseY} Z`);
    }
  });

  return pathParts.join(" ");
};

// 数字格式化
const formatNumber = (value: number) => {
  // if (value >= 1000000) {
  //   return `${(value / 1000000).toFixed(1)}M`;
  // } else
  if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}K`;
  }
  return Math.round(value).toString();
};

const isErrorDataset = (datasetIndex: number) => datasetIndex === 1;

const chartSummary = computed(() => {
  const data = chartData.value;
  if (!data) {
    return t("charts.requestTrend24h");
  }

  const series = data.datasets.map(dataset => {
    const points = dataset.data
      .map((value, index) => `${formatTimeLabel(data.labels[index])} ${formatNumber(value)}`)
      .join(", ");
    return `${dataset.label}: ${points}`;
  });

  return `${t("charts.accessibleSummary", { points: data.labels.length })}. ${series.join("; ")}`;
});

// 鼠标交互
const handleMouseMove = (event: MouseEvent) => {
  if (!chartData.value || !chartSvg.value) {
    return;
  }

  const rect = chartSvg.value.getBoundingClientRect();
  // 考虑SVG的viewBox缩放
  const scaleX = 800 / rect.width;
  const scaleY = 260 / rect.height;

  const mouseX = (event.clientX - rect.left) * scaleX;
  const mouseY = (event.clientY - rect.top) * scaleY;

  // 首先找到最接近的X轴位置（时间点）
  let closestXDistance = Infinity;
  let closestTimeIndex = -1;

  chartData.value.labels.forEach((_, pointIndex) => {
    const x = getXPosition(pointIndex);
    const xDistance = Math.abs(mouseX - x);

    if (xDistance < closestXDistance) {
      closestXDistance = xDistance;
      closestTimeIndex = pointIndex;
    }
  });

  // 如果鼠标距离最近的时间点太远，不显示提示
  if (closestXDistance > 50) {
    hoveredPoint.value = null;
    tooltipData.value = null;
    return;
  }

  // 收集该时间点所有数据集的数据
  const datasetsAtTime = chartData.value.datasets.map(dataset => ({
    label: dataset.label,
    value: dataset.data[closestTimeIndex],
    color: dataset.color,
  }));

  if (closestTimeIndex >= 0) {
    hoveredPoint.value = {
      datasetIndex: 0, // 不再需要特定的数据集索引
      pointIndex: closestTimeIndex,
      x: mouseX,
      y: mouseY,
    };

    // 显示 tooltip
    const x = getXPosition(closestTimeIndex);
    const avgY =
      datasetsAtTime.reduce((sum, item) => sum + getYPosition(item.value), 0) /
      datasetsAtTime.length;

    tooltipPosition.value = {
      x,
      y: avgY - 20, // 在平均高度上方显示
    };

    tooltipData.value = {
      time: formatTimeLabel(chartData.value.labels[closestTimeIndex]),
      datasets: datasetsAtTime,
    };
  } else {
    hoveredPoint.value = null;
    tooltipData.value = null;
  }
};

const hideTooltip = () => {
  hoveredPoint.value = null;
  tooltipData.value = null;
};

// 获取分组列表
const fetchGroups = async () => {
  try {
    const response = await getGroupList();
    groupOptions.value = [
      { label: t("charts.allGroups"), value: null },
      ...response.data.map(group => ({
        label: getGroupDisplayName(group),
        value: group.id || 0,
      })),
    ];
  } catch (error) {
    console.error("Failed to fetch groups:", error);
  }
};

// 获取图表数据
const fetchChartData = async () => {
  try {
    loading.value = true;
    chartError.value = false;
    const response = await getDashboardChart(selectedGroup.value || undefined);
    chartData.value = response.data;
  } catch (error) {
    chartData.value = null;
    chartError.value = true;
    console.error("Failed to fetch chart data:", error);
  } finally {
    loading.value = false;
  }
};

// 监听分组选择变化
watch(selectedGroup, () => {
  fetchChartData();
});

onMounted(() => {
  fetchGroups();
  fetchChartData();
});
</script>

<template>
  <div class="chart-container">
    <div class="chart-header">
      <div class="chart-title-section">
        <h3 class="chart-title">{{ t("charts.requestTrend24h") }}</h3>
      </div>
      <n-select
        v-model:value="selectedGroup"
        :options="groupOptions as any"
        :placeholder="t('charts.allGroups')"
        size="small"
        style="width: 150px"
        clearable
        :loading="loading"
        :aria-label="t('charts.allGroups')"
      />
    </div>

    <div v-if="chartData" class="chart-content">
      <div class="chart-wrapper">
        <div class="chart-legend">
          <div v-for="dataset in chartData.datasets" :key="dataset.label" class="legend-item">
            <div class="legend-indicator" :style="{ backgroundColor: dataset.color }" />
            <span class="legend-label">{{ dataset.label }}</span>
          </div>
        </div>
        <svg
          ref="chartSvg"
          viewBox="0 0 800 260"
          class="chart-svg"
          role="img"
          aria-labelledby="request-chart-title request-chart-description"
          @mousemove="handleMouseMove"
          @mouseleave="hideTooltip"
        >
          <title id="request-chart-title">{{ t("charts.requestTrend24h") }}</title>
          <desc id="request-chart-description">{{ chartSummary }}</desc>
          <!-- 背景网格 -->
          <defs>
            <pattern id="grid" width="40" height="30" patternUnits="userSpaceOnUse">
              <path
                d="M 40 0 L 0 0 0 30"
                fill="none"
                :stroke="`var(--chart-grid)`"
                stroke-width="1"
                opacity="0.3"
              />
            </pattern>
          </defs>
          <rect width="100%" height="100%" fill="url(#grid)" />

          <!-- Y轴刻度线和标签 -->
          <g class="y-axis">
            <line
              :x1="padding.left"
              :y1="padding.top"
              :x2="padding.left"
              :y2="chartHeight - padding.bottom"
              :stroke="`var(--chart-axis)`"
              stroke-width="2"
            />
            <g v-for="(tick, index) in yTicks" :key="index">
              <line
                :x1="padding.left - 5"
                :y1="getYPosition(tick)"
                :x2="padding.left"
                :y2="getYPosition(tick)"
                :stroke="`var(--chart-text)`"
                stroke-width="1"
              />
              <text
                :x="padding.left - 10"
                :y="getYPosition(tick) + 4"
                text-anchor="end"
                class="axis-label"
              >
                {{ formatNumber(tick) }}
              </text>
            </g>
          </g>

          <!-- X轴刻度线和标签 -->
          <g class="x-axis">
            <line
              :x1="padding.left"
              :y1="chartHeight - padding.bottom"
              :x2="chartWidth - padding.right"
              :y2="chartHeight - padding.bottom"
              :stroke="`var(--chart-axis)`"
              stroke-width="2"
            />
            <g v-for="(label, index) in visibleLabels" :key="index">
              <line
                :x1="getXPosition(label.index)"
                :y1="chartHeight - padding.bottom"
                :x2="getXPosition(label.index)"
                :y2="chartHeight - padding.bottom + 5"
                :stroke="`var(--chart-text)`"
                stroke-width="1"
              />
              <text
                :x="getXPosition(label.index)"
                :y="chartHeight - padding.bottom + 18"
                text-anchor="middle"
                class="axis-label"
              >
                {{ label.text }}
              </text>
            </g>
          </g>

          <!-- 数据线条 -->
          <g v-for="(dataset, datasetIndex) in chartData.datasets" :key="dataset.label">
            <!-- 渐变定义 -->
            <defs>
              <linearGradient :id="`gradient-${datasetIndex}`" x1="0%" y1="0%" x2="0%" y2="100%">
                <stop offset="0%" :stop-color="dataset.color" stop-opacity="0.3" />
                <stop offset="100%" :stop-color="dataset.color" stop-opacity="0.05" />
              </linearGradient>
            </defs>

            <!-- 填充区域 -->
            <path
              :d="generateAreaPath(dataset.data)"
              :fill="`url(#gradient-${datasetIndex})`"
              class="area-path"
              :style="{ opacity: isErrorDataset(datasetIndex) ? 0.3 : 0.6 }"
            />

            <!-- 主线条 -->
            <path
              :d="generateLinePath(dataset.data)"
              :stroke="dataset.color"
              :stroke-width="isErrorDataset(datasetIndex) ? 1 : 2"
              fill="none"
              class="line-path"
              :style="{
                opacity: isErrorDataset(datasetIndex) ? 0.75 : 1,
                filter: 'drop-shadow(0 1px 3px rgba(0,0,0,0.1))',
              }"
            />

            <!-- 数据点 -->
            <g v-for="(value, pointIndex) in dataset.data" :key="pointIndex">
              <circle
                v-if="value > 0"
                :cx="getXPosition(pointIndex)"
                :cy="getYPosition(value)"
                :r="isErrorDataset(datasetIndex) ? 2 : 3"
                :fill="dataset.color"
                :stroke="dataset.color"
                stroke-width="1"
                class="data-point"
                :class="{
                  'point-hover': hoveredPoint?.pointIndex === pointIndex,
                }"
                :style="{ opacity: isErrorDataset(datasetIndex) ? 0.8 : 1 }"
              />
            </g>
          </g>

          <!-- 悬停指示线 -->
          <line
            v-if="hoveredPoint"
            :x1="getXPosition(hoveredPoint.pointIndex)"
            :y1="padding.top"
            :x2="getXPosition(hoveredPoint.pointIndex)"
            :y2="chartHeight - padding.bottom"
            stroke="#999"
            stroke-width="1"
            stroke-dasharray="5,5"
            opacity="0.7"
          />
        </svg>

        <!-- 提示框 -->
        <div
          v-if="tooltipData"
          class="chart-tooltip"
          :style="{
            left: tooltipPosition.x + 'px',
            top: tooltipPosition.y + 'px',
          }"
        >
          <div class="tooltip-time">{{ tooltipData.time }}</div>
          <div v-for="dataset in tooltipData.datasets" :key="dataset.label" class="tooltip-value">
            <span class="tooltip-color" :style="{ backgroundColor: dataset.color }" />
            {{ dataset.label }}: {{ formatNumber(dataset.value) }}
          </div>
        </div>
      </div>
      <p id="request-chart-data-summary" class="sr-only">{{ chartSummary }}</p>
    </div>

    <div v-else-if="loading" class="chart-loading" aria-busy="true">
      <n-spin size="large" />
      <p>{{ t("common.loading") }}</p>
    </div>

    <div v-else class="chart-error" role="alert">
      <p>{{ t("charts.loadFailed") }}</p>
      <n-button size="small" @click="fetchChartData">{{ t("common.retry") }}</n-button>
    </div>
  </div>
</template>

<style scoped>
.chart-container {
  padding: 1.25rem;
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-lg);
  background: var(--card-bg-solid);
  box-shadow: var(--shadow-sm);
  color: var(--text-primary);
}

.chart-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 12px;
  gap: 16px;
}

.chart-title-section {
  flex: 1;
}

.chart-title {
  margin: 0;
  color: var(--text-primary);
  font-size: 1.1rem;
  line-height: 1.35;
  font-weight: 650;
  letter-spacing: -0.015em;
}

.chart-subtitle {
  margin: 0;
  font-size: 14px;
  font-weight: 400;
}

.chart-subtitle {
  color: var(--text-secondary);
}

.chart-legend {
  position: absolute;
  top: 8px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 10;
  display: flex;
  justify-content: center;
  gap: 12px;
  padding: 0.15rem;
  border: 1px solid var(--border-color-light);
  border-radius: 999px;
  background: var(--chart-legend-bg);
  box-shadow: var(--shadow-sm);
  -webkit-backdrop-filter: blur(14px) saturate(140%);
  backdrop-filter: blur(14px) saturate(140%);
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-weight: 600;
  font-size: 13px;
  padding: 0.4rem 0.65rem;
  border-radius: 999px;
  color: var(--chart-legend-text);
  transition: background-color var(--motion-fast) var(--ease-out);
}

.legend-item:hover {
  background: var(--hover-bg);
}

.legend-indicator {
  width: 0.55rem;
  height: 0.55rem;
  border-radius: 50%;
}

.legend-label {
  font-size: 13px;
  color: inherit;
}

.chart-wrapper {
  position: relative;
  display: flex;
  justify-content: center;
}

.chart-svg {
  width: 100%;
  height: auto;
  border-radius: 0.75rem;
  background: var(--chart-bg);
}

.axis-label {
  fill: var(--chart-text);
  font-size: 12px;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}

.area-path {
  opacity: 0.6;
}

.data-point {
  cursor: pointer;
  transition: opacity var(--motion-fast) var(--ease-out);
}

.data-point:hover,
.point-hover {
  r: 5;
  filter: drop-shadow(0 0 6px rgba(0, 0, 0, 0.3));
}

.chart-tooltip {
  position: absolute;
  background: rgba(0, 0, 0, 0.9);
  color: white;
  padding: 12px 16px;
  border-radius: 8px;
  font-size: 13px;
  pointer-events: none;
  transform: translateX(-50%) translateY(-100%);
  z-index: 1000;
  box-shadow: var(--shadow-lg);
  border: 1px solid var(--border-color-light);
  min-width: 140px;
  max-width: 220px;
}

.tooltip-time {
  font-weight: 700;
  margin-bottom: 8px;
  text-align: center;
  color: #e2e8f0;
  font-size: 12px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.2);
  padding-bottom: 6px;
}

.tooltip-value {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  margin-bottom: 4px;
  font-size: 12px;
}

.tooltip-value:last-child {
  margin-bottom: 0;
}

.tooltip-color {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.chart-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 260px;
  color: var(--text-secondary);
}

.chart-error {
  display: grid;
  min-height: 260px;
  place-items: center;
  align-content: center;
  gap: 0.75rem;
  color: var(--error-color);
}

.chart-loading p {
  margin-top: 16px;
  font-size: 16px;
  opacity: 0.8;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .chart-container {
    padding: 1rem;
  }

  .chart-title {
    font-size: 20px;
  }

  .chart-header {
    flex-direction: column;
    gap: 12px;
    align-items: flex-start;
  }

  .chart-wrapper {
    flex-direction: column;
    align-items: center;
  }

  .chart-legend {
    position: relative;
    transform: none;
    left: auto;
    top: auto;
    margin-top: 8px;
    margin-bottom: 12px;
    background: transparent;
    backdrop-filter: none;
    border: none;
    width: 100%;
    flex-wrap: wrap;
    gap: 8px;
    justify-content: center;
  }

  .legend-item {
    padding: 4px 10px;
    font-size: 12px;
    color: var(--text-primary);
    background: var(--bg-secondary);
    border: 1px solid var(--border-color-light);
    gap: 6px;
  }

  .chart-svg {
    width: 100%;
    height: auto;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .chart-legend {
    background: var(--card-bg-solid);
    backdrop-filter: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .data-point,
  .legend-item {
    transition: none;
  }
}
</style>
