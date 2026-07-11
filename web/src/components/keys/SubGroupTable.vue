<script setup lang="ts">
import { keysApi } from "@/api/keys";
import type { Group, SubGroupInfo } from "@/types/models";
import { getGroupDisplayName } from "@/utils/display";
import {
  Add,
  CreateOutline,
  EyeOutline,
  InformationCircleOutline,
  Search,
  Trash,
} from "@vicons/ionicons5";
import {
  NButton,
  NButtonGroup,
  NDivider,
  NEmpty,
  NIcon,
  NInput,
  NSelect,
  NSpin,
  NTag,
  NTooltip,
  useDialog,
} from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import AddSubGroupModal from "./AddSubGroupModal.vue";
import EditSubGroupWeightModal from "./EditSubGroupWeightModal.vue";

const { t } = useI18n();

// 获取子分组状态
function getSubGroupStatus(subGroup: SubGroupInfo): {
  status: "active" | "disabled" | "unavailable";
  text: string;
  type: "success" | "warning" | "error";
} {
  if (subGroup.weight === 0) {
    return { status: "disabled", text: t("subGroups.statusDisabled"), type: "warning" };
  }
  if (subGroup.weight > 0 && subGroup.active_keys === 0) {
    return { status: "unavailable", text: t("subGroups.statusUnavailable"), type: "error" };
  }
  return { status: "active", text: t("subGroups.statusActive"), type: "success" };
}

interface SubGroupRow extends SubGroupInfo {
  percentage: number;
}

interface Props {
  selectedGroup: Group | null;
  subGroups?: SubGroupInfo[];
  groups?: Group[];
  loading?: boolean;
}

interface Emits {
  (e: "refresh"): void;
  (e: "group-select", groupId: number): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const dialog = useDialog();

const addModalShow = ref(false);
const editModalShow = ref(false);
const editingSubGroup = ref<SubGroupInfo | null>(null);

// 搜索和过滤状态
const searchText = ref("");
const statusFilter = ref<"all" | "active" | "disabled" | "unavailable">("all");

// 状态过滤选项
const statusOptions = [
  { label: t("common.all"), value: "all" },
  { label: t("subGroups.statusActive"), value: "active" },
  { label: t("subGroups.statusDisabled"), value: "disabled" },
  { label: t("subGroups.statusUnavailable"), value: "unavailable" },
];

// 计算带百分比的子分组数据并按权重排序
const sortedSubGroupsWithPercentage = computed<SubGroupRow[]>(() => {
  if (!props.subGroups) {
    return [];
  }
  const total = props.subGroups.reduce((sum, sg) => sum + sg.weight, 0);
  const withPercentage = props.subGroups.map(sg => ({
    ...sg,
    percentage: total > 0 ? Math.round((sg.weight / total) * 100) : 0,
  }));

  // 按权重降序排序
  return withPercentage.sort((a, b) => b.weight - a.weight);
});

// 过滤后的子分组（应用搜索和状态过滤）
const filteredSubGroups = computed<SubGroupRow[]>(() => {
  let filtered = sortedSubGroupsWithPercentage.value;

  // 名称搜索过滤（不区分大小写）
  if (searchText.value.trim()) {
    const searchLower = searchText.value.trim().toLowerCase();
    filtered = filtered.filter(sg => {
      const name = sg.group.name?.toLowerCase() || "";
      const displayName = sg.group.display_name?.toLowerCase() || "";
      return name.includes(searchLower) || displayName.includes(searchLower);
    });
  }

  // 状态过滤
  if (statusFilter.value !== "all") {
    filtered = filtered.filter(sg => {
      const status = getSubGroupStatus(sg).status;
      return status === statusFilter.value;
    });
  }

  return filtered;
});

function openEditModal(subGroup: SubGroupInfo) {
  editingSubGroup.value = subGroup;
  editModalShow.value = true;
}

async function deleteSubGroup(subGroup: SubGroupInfo) {
  if (!props.selectedGroup?.id) {
    return;
  }

  const d = dialog.warning({
    title: t("subGroups.removeSubGroup"),
    content: t("subGroups.confirmRemoveSubGroup", { name: getGroupDisplayName(subGroup) }),
    positiveText: t("common.confirm"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (!props.selectedGroup?.id) {
        return;
      }

      d.loading = true;
      try {
        const groupId = subGroup.group.id;
        if (!groupId) {
          return;
        }
        await keysApi.deleteSubGroup(props.selectedGroup.id, groupId);
        emit("refresh");
      } finally {
        d.loading = false;
      }
    },
  });
}

// Handle success after modal operations
function handleSuccess() {
  emit("refresh");
}

// Navigate to group info
function goToGroupInfo(groupId: number) {
  emit("group-select", groupId);
}

// Format number with K suffix
function formatNumber(num: number): string {
  if (num >= 1000) {
    return `${(num / 1000).toFixed(1)}K`;
  }
  return num.toString();
}
</script>

<template>
  <div class="key-table-container">
    <!-- 工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <n-button
          type="primary"
          size="small"
          :disabled="!selectedGroup"
          @click="addModalShow = true"
        >
          <template #icon>
            <n-icon :component="Add" />
          </template>
          {{ t("subGroups.addSubGroup") }}
        </n-button>
      </div>
      <div class="toolbar-right">
        <n-select
          v-model:value="statusFilter"
          :options="statusOptions"
          size="small"
          class="status-filter"
          :placeholder="t('keys.allStatus')"
          :disabled="!selectedGroup || props.loading"
        />
        <n-input
          v-model:value="searchText"
          :placeholder="t('keys.searchByName')"
          size="small"
          class="search-input"
          clearable
          :disabled="!selectedGroup || props.loading"
        >
          <template #prefix>
            <n-icon :component="Search" />
          </template>
        </n-input>
      </div>
    </div>

    <!-- 子分组卡片网格 -->
    <div class="keys-grid-container">
      <n-spin :show="props.loading || false">
        <div v-if="!props.subGroups || props.subGroups.length === 0" class="empty-container">
          <n-empty :description="t('subGroups.noSubGroups')" />
        </div>
        <div v-else-if="filteredSubGroups.length === 0" class="empty-container">
          <n-empty :description="t('keys.noMatchingKeys')" />
        </div>
        <div v-else class="keys-grid">
          <div
            v-for="subGroup in filteredSubGroups"
            :key="subGroup.group.id"
            class="key-card status-sub-group"
            :class="{ disabled: subGroup.weight === 0 || subGroup.active_keys === 0 }"
          >
            <!-- Main info row: display name + group name -->
            <div class="key-main">
              <div class="key-section">
                <div class="sub-group-names">
                  <span class="display-name">{{ getGroupDisplayName(subGroup) }}</span>
                </div>
                <div class="quick-actions">
                  <span class="group-name">#{{ subGroup.group.name }}</span>
                </div>
              </div>
            </div>

            <!-- 权重显示 -->
            <div class="weight-display">
              <div class="weight-bar-container">
                <span class="weight-label">
                  {{ t("subGroups.weight") }}
                  <strong>{{ subGroup.weight }}</strong>
                </span>
                <div class="weight-bar">
                  <div
                    class="weight-fill"
                    :class="{
                      'weight-fill-active': subGroup.weight > 0 && subGroup.active_keys > 0,
                      'weight-fill-unavailable': subGroup.weight > 0 && subGroup.active_keys === 0,
                    }"
                    :style="{ width: `${subGroup.percentage}%` }"
                  />
                </div>
                <span class="weight-text">{{ subGroup.percentage }}%</span>
              </div>
            </div>

            <!-- 密钥统计 -->
            <div class="key-stats-row">
              <div class="stats-left">
                <span class="stat-item">
                  <span class="stat-value">{{ formatNumber(subGroup.total_keys) }}</span>
                </span>
                <n-divider vertical />
                <span class="stat-item stat-success">
                  {{ formatNumber(subGroup.active_keys) }}
                </span>
                <n-divider vertical />
                <span class="stat-item stat-error">
                  {{ formatNumber(subGroup.invalid_keys) }}
                </span>
              </div>
              <n-tag :type="getSubGroupStatus(subGroup).type" size="small">
                {{ getSubGroupStatus(subGroup).text }}
              </n-tag>
            </div>

            <!-- 操作按钮行 -->
            <div class="key-bottom">
              <div class="key-stats">
                <n-tooltip trigger="hover" placement="top">
                  <template #trigger>
                    <n-button
                      round
                      tertiary
                      type="default"
                      size="tiny"
                      :aria-label="t('keys.basicInfo')"
                    >
                      <template #icon>
                        <n-icon :component="InformationCircleOutline" />
                      </template>
                    </n-button>
                  </template>
                  <div class="sub-group-info-tooltip">
                    <!-- 分组名称和状态 -->
                    <div class="info-header">
                      <div class="info-title">{{ getGroupDisplayName(subGroup) }}</div>
                      <n-tag :type="getSubGroupStatus(subGroup).type" size="small">
                        {{ getSubGroupStatus(subGroup).text }}
                      </n-tag>
                    </div>

                    <!-- 详细信息 -->
                    <div class="info-details">
                      <div class="info-row">
                        <span class="info-label">{{ t("keys.testModel") }}:</span>
                        <span class="info-value">{{ subGroup.group.test_model || "-" }}</span>
                      </div>
                      <div class="info-row" v-if="subGroup.group.channel_type !== 'gemini'">
                        <span class="info-label">{{ t("keys.testPath") }}:</span>
                        <span class="info-value">
                          {{ subGroup.group.validation_endpoint || "-" }}
                        </span>
                      </div>

                      <!-- 上游地址 -->
                      <div
                        class="info-row"
                        v-if="subGroup.group.upstreams && subGroup.group.upstreams.length > 0"
                      >
                        <span class="info-label">{{ t("keys.upstreamAddresses") }}:</span>
                        <div class="info-value upstream-list">
                          <input
                            v-for="(upstream, index) in subGroup.group.upstreams"
                            :key="index"
                            class="upstream-input"
                            :value="upstream.url"
                            readonly
                          />
                        </div>
                      </div>
                    </div>
                  </div>
                </n-tooltip>
              </div>
              <n-button-group class="key-actions">
                <n-button
                  round
                  tertiary
                  type="default"
                  size="tiny"
                  @click="subGroup.group.id && goToGroupInfo(subGroup.group.id)"
                  :title="t('subGroups.viewSubGroup')"
                >
                  <template #icon>
                    <n-icon :component="EyeOutline" />
                  </template>
                  {{ t("common.view") }}
                </n-button>
                <n-button
                  round
                  tertiary
                  type="info"
                  size="tiny"
                  @click="openEditModal(subGroup)"
                  :title="t('subGroups.editWeight')"
                >
                  <template #icon>
                    <n-icon :component="CreateOutline" />
                  </template>
                  {{ t("common.edit") }}
                </n-button>
                <n-button
                  round
                  tertiary
                  size="tiny"
                  type="error"
                  @click="deleteSubGroup(subGroup)"
                  :title="t('subGroups.removeSubGroup')"
                >
                  <template #icon>
                    <n-icon :component="Trash" />
                  </template>
                  {{ t("subGroups.remove") }}
                </n-button>
              </n-button-group>
            </div>
          </div>
        </div>
      </n-spin>
    </div>

    <!-- 底部信息 -->
    <div class="pagination-container">
      <div class="pagination-info">
        <span>
          {{ t("subGroups.totalSubGroups", { total: filteredSubGroups.length }) }}
          <template v-if="filteredSubGroups.length !== (props.subGroups?.length || 0)">
            / {{ props.subGroups?.length || 0 }}
          </template>
        </span>
      </div>
      <div class="pagination-controls">
        <span class="page-info">
          {{ t("subGroups.sortedByWeight") }}
        </span>
      </div>
    </div>

    <!-- 添加子分组弹窗 -->
    <add-sub-group-modal
      v-if="selectedGroup?.id"
      v-model:show="addModalShow"
      :aggregate-group="selectedGroup"
      :existing-sub-groups="subGroups || []"
      :groups="groups || []"
      @success="handleSuccess"
    />

    <!-- 编辑权重弹窗 -->
    <edit-sub-group-weight-modal
      v-if="editingSubGroup && selectedGroup?.id"
      v-model:show="editModalShow"
      :aggregate-group="selectedGroup"
      :sub-group="editingSubGroup"
      :sub-groups="subGroups || []"
      @success="handleSuccess"
      @update:show="
        show => {
          if (!show) editingSubGroup = null;
        }
      "
    />
  </div>
</template>

<style scoped>
.key-table-container {
  display: flex;
  height: 100%;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-lg);
  background: var(--card-bg-solid);
  box-shadow: var(--shadow-sm);
}

.toolbar {
  display: flex;
  min-height: 64px;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-4);
  border-bottom: 1px solid var(--border-color-light);
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.toolbar-right {
  min-width: 0;
  flex: 1;
  justify-content: flex-end;
}

.status-filter {
  width: 120px;
}

.search-input {
  width: 200px;
}

.keys-grid-container {
  flex: 1;
  padding: var(--space-4);
  overflow-y: auto;
}

.keys-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: var(--space-4);
}

.key-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: 14px;
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-md);
  background: var(--card-bg-solid);
  box-shadow: none;
  transition: box-shadow var(--motion-fast) var(--ease-out);
}

.key-card:hover {
  box-shadow: var(--shadow-md);
}

.key-card.status-sub-group {
  border-color: color-mix(in srgb, var(--primary-color) 26%, transparent);
}

.key-card.disabled {
  background: var(--bg-secondary);
}

.key-card.disabled .display-name,
.key-card.disabled .group-name,
.key-card.disabled .weight-label {
  color: var(--text-disabled);
}

.key-card.disabled .weight-fill {
  background: var(--color-disabled);
}

.key-main,
.key-section,
.key-bottom,
.weight-bar-container,
.key-stats-row,
.stats-left {
  display: flex;
  align-items: center;
}

.key-main,
.key-bottom,
.key-stats-row {
  justify-content: space-between;
}

.key-section,
.sub-group-names,
.stats-left,
.key-stats {
  min-width: 0;
  flex: 1;
}

.key-section,
.key-bottom {
  gap: var(--space-2);
}

.sub-group-names {
  display: flex;
  align-items: baseline;
}

.display-name {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  color: var(--text-primary);
  font-size: 16px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quick-actions {
  display: flex;
  flex-shrink: 0;
}

.group-name {
  flex-shrink: 0;
  padding: 2px 6px;
  border-radius: var(--border-radius-sm);
  background: var(--primary-color-suppl);
  color: var(--primary-color);
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, Courier, monospace;
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
}

.weight-display,
.key-stats-row {
  margin-block: var(--space-1);
}

.weight-bar-container {
  gap: var(--space-3);
}

.weight-label,
.key-stats,
.pagination-info,
.page-info {
  color: var(--text-secondary);
  font-size: 12px;
}

.weight-label {
  white-space: nowrap;
}

.weight-label strong,
.stat-value {
  color: var(--text-primary);
  font-weight: 600;
}

.weight-bar {
  height: 8px;
  flex: 1;
  overflow: hidden;
  border-radius: 999px;
  background: var(--bg-tertiary);
}

.weight-fill {
  height: 100%;
  border-radius: inherit;
  background: var(--color-disabled);
  transition: width var(--motion-default) var(--ease-out);
}

.weight-fill-active {
  background: var(--success-color);
}

.weight-fill-unavailable {
  background: repeating-linear-gradient(
    45deg,
    var(--error-border) 0,
    var(--error-border) 8px,
    var(--error-color) 8px,
    var(--error-color) 12px
  );
}

.weight-text {
  min-width: 40px;
  color: var(--text-primary);
  font-size: 14px;
  font-weight: 600;
  text-align: right;
}

.stats-left {
  font-size: 14px;
}

.stat-item {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  white-space: nowrap;
}

.stat-success {
  color: var(--success-color);
  font-weight: 600;
}

.stat-error {
  color: var(--error-color);
  font-weight: 600;
}

.key-stats {
  display: flex;
  gap: var(--space-2);
  overflow: hidden;
}

.key-actions {
  flex-shrink: 0;
}

.key-actions :deep(.n-button) {
  padding-inline: 6px;
}

.pagination-container {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color-light);
}

.pagination-info,
.pagination-controls {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.empty-container {
  display: grid;
  min-height: 14rem;
  place-items: center;
}

.sub-group-info-tooltip {
  width: min(420px, calc(100vw - 2rem));
  max-width: 100%;
  max-height: min(70dvh, 560px);
  padding: var(--space-2);
  overflow-y: auto;
}

.info-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border-color-light);
}

.info-title {
  color: inherit;
  font-size: 15px;
  font-weight: 600;
}

.info-details,
.upstream-list {
  display: flex;
  flex-direction: column;
}

.info-details {
  gap: var(--space-2);
}

.info-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-3);
  font-size: 13px;
  line-height: 1.5;
}

.info-label {
  flex-shrink: 0;
  color: inherit;
  opacity: 0.72;
}

.info-value {
  min-width: 0;
  flex: 1;
  color: inherit;
  font-weight: 500;
  text-align: right;
  word-break: break-word;
}

.upstream-list {
  width: 100%;
  gap: var(--space-1);
}

.upstream-input {
  width: 100%;
  box-sizing: border-box;
  padding: var(--space-1) var(--space-2);
  overflow-x: auto;
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-sm);
  outline: none;
  background: var(--bg-secondary);
  color: inherit;
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, Courier, monospace;
  font-size: 12px;
  white-space: nowrap;
}

@media (max-width: 768px) {
  .toolbar {
    align-items: stretch;
    flex-direction: column;
    padding: var(--space-3);
  }

  .toolbar-left,
  .toolbar-right {
    width: 100%;
  }

  .toolbar-left :deep(.n-button),
  .toolbar-right :deep(.n-base-selection),
  .toolbar-right :deep(.n-input) {
    min-height: 44px;
  }

  .toolbar-left :deep(.n-button),
  .status-filter,
  .search-input {
    width: 100%;
  }

  .toolbar-right {
    align-items: stretch;
    flex-direction: column;
  }

  .keys-grid-container {
    padding: var(--space-3);
  }

  .keys-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
  }

  .key-bottom {
    align-items: stretch;
    flex-direction: column;
  }

  .key-stats,
  .key-actions {
    width: 100%;
  }

  .key-stats :deep(.n-button),
  .key-actions :deep(.n-button) {
    min-width: 44px;
    min-height: 44px;
  }

  .key-actions {
    display: flex;
  }

  .key-actions :deep(.n-button) {
    flex: 1;
  }

  .pagination-container {
    align-items: flex-start;
    flex-direction: column;
  }

  .info-row {
    flex-direction: column;
    gap: var(--space-1);
  }

  .info-value {
    width: 100%;
    text-align: left;
  }
}

@media (prefers-reduced-motion: reduce) {
  .key-card,
  .weight-fill {
    transition: none;
  }
}
</style>
