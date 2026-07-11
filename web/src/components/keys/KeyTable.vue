<script setup lang="ts">
import { keysApi } from "@/api/keys";
import type { APIKey, Group, KeyStatus } from "@/types/models";
import { appState, triggerSyncOperationRefresh } from "@/utils/app-state";
import { copy } from "@/utils/clipboard";
import { getGroupDisplayName, maskKey } from "@/utils/display";
import {
  AddCircleOutline,
  AlertCircleOutline,
  CheckmarkCircle,
  CopyOutline,
  EyeOffOutline,
  EyeOutline,
  Pencil,
  RemoveCircleOutline,
  Search,
} from "@vicons/ionicons5";
import {
  NAlert,
  NButton,
  NButtonGroup,
  NDropdown,
  NEmpty,
  NIcon,
  NInput,
  NInputGroup,
  NModal,
  NSelect,
  NSpin,
  NTag,
  useDialog,
  type MessageReactive,
} from "naive-ui";
import { h, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import KeyCreateDialog from "./KeyCreateDialog.vue";
import KeyDeleteDialog from "./KeyDeleteDialog.vue";

const { t } = useI18n();

interface KeyRow extends APIKey {
  is_visible: boolean;
}

interface Props {
  selectedGroup: Group | null;
}

const props = defineProps<Props>();

const keys = ref<KeyRow[]>([]);
const loading = ref(false);
const loadError = ref(false);
const searchText = ref("");
const statusFilter = ref<"all" | "active" | "invalid">("all");
const currentPage = ref(1);
const pageSize = ref(12);
const total = ref(0);
const totalPages = ref(0);
const dialog = useDialog();
const confirmInput = ref("");

// 状态过滤选项
const statusOptions = [
  { label: t("common.all"), value: "all" },
  { label: t("keys.valid"), value: "active" },
  { label: t("keys.invalid"), value: "invalid" },
];

// 更多操作下拉菜单选项
const moreOptions = [
  { label: t("keys.exportAllKeys"), key: "copyAll" },
  { label: t("keys.exportValidKeys"), key: "copyValid" },
  { label: t("keys.exportInvalidKeys"), key: "copyInvalid" },
  { type: "divider" },
  { label: t("keys.restoreAllInvalidKeys"), key: "restoreAll" },
  {
    label: t("keys.clearAllInvalidKeys"),
    key: "clearInvalid",
    props: { style: { color: "#d03050" } },
  },
  {
    label: t("keys.clearAllKeys"),
    key: "clearAll",
    props: { style: { color: "red", fontWeight: "bold" } },
  },
  { type: "divider" },
  { label: t("keys.validateAllKeys"), key: "validateAll" },
  { label: t("keys.validateValidKeys"), key: "validateActive" },
  { label: t("keys.validateInvalidKeys"), key: "validateInvalid" },
];

let testingMsg: MessageReactive | null = null;
const isDeling = ref(false);
const isRestoring = ref(false);

const createDialogShow = ref(false);
const deleteDialogShow = ref(false);

// 备注编辑相关
const notesDialogShow = ref(false);
const editingKey = ref<KeyRow | null>(null);
const editingNotes = ref("");
let latestLoadRequest = 0;

watch(
  () => props.selectedGroup,
  async newGroup => {
    if (newGroup) {
      // 检查重置页面是否会触发分页观察者。
      const willWatcherTrigger = currentPage.value !== 1 || statusFilter.value !== "all";
      resetPage();
      // 如果分页观察者不触发，则手动加载。
      if (!willWatcherTrigger) {
        await loadKeys();
      }
    }
  },
  { immediate: true }
);

watch([currentPage, pageSize], async () => {
  await loadKeys();
});

watch(statusFilter, async () => {
  if (currentPage.value !== 1) {
    currentPage.value = 1;
  } else {
    await loadKeys();
  }
});

// 监听任务完成事件，自动刷新密钥列表
watch(
  () => appState.groupDataRefreshTrigger,
  () => {
    // 检查是否需要刷新当前分组的密钥列表
    if (appState.lastCompletedTask && props.selectedGroup) {
      // 通过分组名称匹配
      const isCurrentGroup = appState.lastCompletedTask.groupName === props.selectedGroup.name;

      const shouldRefresh =
        appState.lastCompletedTask.taskType === "KEY_VALIDATION" ||
        appState.lastCompletedTask.taskType === "KEY_IMPORT" ||
        appState.lastCompletedTask.taskType === "KEY_DELETE";

      if (isCurrentGroup && shouldRefresh) {
        // 刷新当前分组的密钥列表
        loadKeys();
      }
    }
  }
);

// 处理搜索输入的防抖
function handleSearchInput() {
  if (currentPage.value !== 1) {
    currentPage.value = 1;
  } else {
    loadKeys();
  }
}

// 处理更多操作菜单
function handleMoreAction(key: string) {
  switch (key) {
    case "copyAll":
      copyAllKeys();
      break;
    case "copyValid":
      copyValidKeys();
      break;
    case "copyInvalid":
      copyInvalidKeys();
      break;
    case "restoreAll":
      restoreAllInvalid();
      break;
    case "validateAll":
      validateKeys("all");
      break;
    case "validateActive":
      validateKeys("active");
      break;
    case "validateInvalid":
      validateKeys("invalid");
      break;
    case "clearInvalid":
      clearAllInvalid();
      break;
    case "clearAll":
      clearAll();
      break;
  }
}

async function loadKeys() {
  const requestId = ++latestLoadRequest;

  if (!props.selectedGroup?.id) {
    keys.value = [];
    total.value = 0;
    totalPages.value = 0;
    loadError.value = false;
    loading.value = false;
    return;
  }

  try {
    loading.value = true;
    loadError.value = false;
    const result = await keysApi.getGroupKeys({
      group_id: props.selectedGroup.id,
      page: currentPage.value,
      page_size: pageSize.value,
      status: statusFilter.value === "all" ? undefined : (statusFilter.value as KeyStatus),
      key_value: searchText.value.trim() || undefined,
    });

    if (requestId !== latestLoadRequest) {
      return;
    }

    keys.value = result.items as KeyRow[];
    total.value = result.pagination.total_items;
    totalPages.value = result.pagination.total_pages;
  } catch (_error) {
    if (requestId !== latestLoadRequest) {
      return;
    }

    keys.value = [];
    total.value = 0;
    totalPages.value = 0;
    loadError.value = true;
    console.error("Load keys failed");
  } finally {
    if (requestId === latestLoadRequest) {
      loading.value = false;
    }
  }
}

// 处理批量删除成功后的刷新
async function handleBatchDeleteSuccess() {
  await loadKeys();
  // 触发同步操作刷新
  if (props.selectedGroup) {
    triggerSyncOperationRefresh(props.selectedGroup.name, "BATCH_DELETE");
  }
}

async function copyKey(key: KeyRow) {
  const success = await copy(key.key_value);
  if (success) {
    window.$message.success(t("keys.keyCopied"));
  } else {
    window.$message.error(t("keys.copyFailed"));
  }
}

async function testKey(_key: KeyRow) {
  if (!props.selectedGroup?.id || !_key.key_value || testingMsg) {
    return;
  }

  testingMsg = window.$message.info(t("keys.testingKey"), {
    duration: 0,
  });

  try {
    const response = await keysApi.testKeys(props.selectedGroup.id, _key.key_value);
    const curValid = response.results?.[0] || {};
    if (curValid.is_valid) {
      window.$message.success(
        t("keys.testSuccess", { duration: formatDuration(response.total_duration) })
      );
    } else {
      window.$message.error(curValid.error || t("keys.testFailed"), {
        keepAliveOnHover: true,
        duration: 5000,
        closable: true,
      });
    }
    await loadKeys();
    // 触发同步操作刷新
    triggerSyncOperationRefresh(props.selectedGroup.name, "TEST_SINGLE");
  } catch (_error) {
    console.error("Test failed");
  } finally {
    testingMsg?.destroy();
    testingMsg = null;
  }
}

function formatDuration(ms: number): string {
  if (ms < 0) {
    return "0ms";
  }

  const minutes = Math.floor(ms / 60000);
  const seconds = Math.floor((ms % 60000) / 1000);
  const milliseconds = ms % 1000;

  let result = "";
  if (minutes > 0) {
    result += `${minutes}m`;
  }
  if (seconds > 0) {
    result += `${seconds}s`;
  }
  if (milliseconds > 0 || result === "") {
    result += `${milliseconds}ms`;
  }

  return result;
}

function toggleKeyVisibility(key: KeyRow) {
  key.is_visible = !key.is_visible;
}

// 获取要显示的值（备注优先，否则显示密钥）
function getDisplayValue(key: KeyRow): string {
  if (key.notes && !key.is_visible) {
    return key.notes;
  }
  return key.is_visible ? key.key_value : maskKey(key.key_value);
}

// 编辑密钥备注
function editKeyNotes(key: KeyRow) {
  editingKey.value = key;
  editingNotes.value = key.notes || "";
  notesDialogShow.value = true;
}

// 保存备注
async function saveKeyNotes() {
  if (!editingKey.value) {
    return;
  }

  try {
    const trimmed = editingNotes.value.trim();
    await keysApi.updateKeyNotes(editingKey.value.id, trimmed);
    editingKey.value.notes = trimmed;
    window.$message.success(t("keys.notesUpdated"));
    notesDialogShow.value = false;
  } catch (error) {
    console.error("Update notes failed", error);
  }
}

async function restoreKey(key: KeyRow) {
  if (!props.selectedGroup?.id || !key.key_value || isRestoring.value) {
    return;
  }

  const d = dialog.warning({
    title: t("keys.restoreKey"),
    content: t("keys.confirmRestoreKey", { key: maskKey(key.key_value) }),
    positiveText: t("common.confirm"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (!props.selectedGroup?.id) {
        return;
      }

      isRestoring.value = true;
      d.loading = true;

      try {
        await keysApi.restoreKeys(props.selectedGroup.id, key.key_value);
        await loadKeys();
        // 触发同步操作刷新
        triggerSyncOperationRefresh(props.selectedGroup.name, "RESTORE_SINGLE");
      } catch (_error) {
        console.error("Restore failed");
      } finally {
        d.loading = false;
        isRestoring.value = false;
      }
    },
  });
}

async function deleteKey(key: KeyRow) {
  if (!props.selectedGroup?.id || !key.key_value || isDeling.value) {
    return;
  }

  const d = dialog.warning({
    title: t("keys.deleteKey"),
    content: t("keys.confirmDeleteKey", { key: maskKey(key.key_value) }),
    positiveText: t("common.confirm"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (!props.selectedGroup?.id) {
        return;
      }

      d.loading = true;
      isDeling.value = true;

      try {
        await keysApi.deleteKeys(props.selectedGroup.id, key.key_value);
        await loadKeys();
        // 触发同步操作刷新
        triggerSyncOperationRefresh(props.selectedGroup.name, "DELETE_SINGLE");
      } catch (_error) {
        console.error("Delete failed");
      } finally {
        d.loading = false;
        isDeling.value = false;
      }
    },
  });
}

function formatRelativeTime(date: string) {
  if (!date) {
    return t("keys.never");
  }
  const now = new Date();
  const target = new Date(date);
  const diffSeconds = Math.floor((now.getTime() - target.getTime()) / 1000);
  const diffMinutes = Math.floor(diffSeconds / 60);
  const diffHours = Math.floor(diffMinutes / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffDays > 0) {
    return t("keys.daysAgo", { days: diffDays });
  }
  if (diffHours > 0) {
    return t("keys.hoursAgo", { hours: diffHours });
  }
  if (diffMinutes > 0) {
    return t("keys.minutesAgo", { minutes: diffMinutes });
  }
  if (diffSeconds > 0) {
    return t("keys.secondsAgo", { seconds: diffSeconds });
  }
  return t("keys.justNow");
}

function getStatusClass(status: KeyStatus): string {
  switch (status) {
    case "active":
      return "status-valid";
    case "invalid":
      return "status-invalid";
    default:
      return "status-unknown";
  }
}

async function copyAllKeys() {
  if (!props.selectedGroup?.id) {
    return;
  }

  try {
    await keysApi.exportKeys(props.selectedGroup.id, "all");
  } catch (error) {
    console.error("Export keys failed");
    showExportError(error);
  }
}

async function copyValidKeys() {
  if (!props.selectedGroup?.id) {
    return;
  }

  try {
    await keysApi.exportKeys(props.selectedGroup.id, "active");
  } catch (error) {
    console.error("Export keys failed");
    showExportError(error);
  }
}

async function copyInvalidKeys() {
  if (!props.selectedGroup?.id) {
    return;
  }

  try {
    await keysApi.exportKeys(props.selectedGroup.id, "invalid");
  } catch (error) {
    console.error("Export keys failed");
    showExportError(error);
  }
}

function showExportError(error: unknown) {
  const status = (error as { response?: { status?: number } } | null)?.response?.status;
  window.$message.error(status ? t("common.requestFailed", { status }) : t("common.networkError"));
}

async function restoreAllInvalid() {
  if (!props.selectedGroup?.id || isRestoring.value) {
    return;
  }

  const d = dialog.warning({
    title: t("keys.restoreKeys"),
    content: t("keys.confirmRestoreAllInvalid"),
    positiveText: t("common.confirm"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (!props.selectedGroup?.id) {
        return;
      }

      isRestoring.value = true;
      d.loading = true;
      try {
        await keysApi.restoreAllInvalidKeys(props.selectedGroup.id);
        await loadKeys();
        // 触发同步操作刷新
        triggerSyncOperationRefresh(props.selectedGroup.name, "RESTORE_ALL_INVALID");
      } catch (_error) {
        console.error("Restore failed");
      } finally {
        d.loading = false;
        isRestoring.value = false;
      }
    },
  });
}

async function validateKeys(status: "all" | "active" | "invalid") {
  if (!props.selectedGroup?.id || testingMsg) {
    return;
  }

  let statusText = t("common.all");
  if (status === "active") {
    statusText = t("keys.valid");
  } else if (status === "invalid") {
    statusText = t("keys.invalid");
  }

  testingMsg = window.$message.info(t("keys.validatingKeysMsg", { type: statusText }), {
    duration: 0,
  });

  try {
    await keysApi.validateGroupKeys(props.selectedGroup.id, status === "all" ? undefined : status);
    localStorage.removeItem("last_closed_task");
    appState.taskPollingTrigger++;
  } catch (_error) {
    console.error("Test failed");
  } finally {
    testingMsg?.destroy();
    testingMsg = null;
  }
}

async function clearAllInvalid() {
  if (!props.selectedGroup?.id || isDeling.value) {
    return;
  }

  const d = dialog.warning({
    title: t("keys.clearKeys"),
    content: t("keys.confirmClearInvalidKeys"),
    positiveText: t("common.confirm"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (!props.selectedGroup?.id) {
        return;
      }

      isDeling.value = true;
      d.loading = true;
      try {
        const { data } = await keysApi.clearAllInvalidKeys(props.selectedGroup.id);
        window.$message.success(data?.message || t("keys.clearSuccess"));
        await loadKeys();
        // 触发同步操作刷新
        triggerSyncOperationRefresh(props.selectedGroup.name, "CLEAR_ALL_INVALID");
      } catch (_error) {
        console.error("Delete failed");
      } finally {
        d.loading = false;
        isDeling.value = false;
      }
    },
  });
}

async function clearAll() {
  if (!props.selectedGroup?.id || isDeling.value) {
    return;
  }

  dialog.warning({
    title: t("keys.clearAllKeys"),
    content: t("keys.confirmClearAllKeys"),
    positiveText: t("common.confirm"),
    negativeText: t("common.cancel"),
    onPositiveClick: () => {
      confirmInput.value = ""; // Reset before opening second dialog
      dialog.create({
        title: t("keys.enterGroupNameToConfirm"),
        content: () =>
          h("div", null, [
            h("p", null, [
              t("keys.dangerousOperationWarning1"),
              h("strong", null, t("common.all")),
              t("keys.dangerousOperationWarning2"),
              h("strong", { style: { color: "#d03050" } }, props.selectedGroup?.name),
              t("keys.toConfirm"),
            ]),
            h(NInput, {
              value: confirmInput.value,
              "onUpdate:value": v => {
                confirmInput.value = v;
              },
              placeholder: t("keys.enterGroupName"),
            }),
          ]),
        positiveText: t("keys.confirmClear"),
        negativeText: t("common.cancel"),
        onPositiveClick: async () => {
          if (confirmInput.value !== props.selectedGroup?.name) {
            window.$message.error(t("keys.incorrectGroupName"));
            return false; // Prevent dialog from closing
          }

          if (!props.selectedGroup?.id) {
            return;
          }

          isDeling.value = true;
          try {
            await keysApi.clearAllKeys(props.selectedGroup.id);
            window.$message.success(t("keys.clearAllKeysSuccess"));
            await loadKeys();
            // Trigger sync operation refresh
            triggerSyncOperationRefresh(props.selectedGroup.name, "CLEAR_ALL");
          } catch (_error) {
            console.error("Clear all failed", _error);
          } finally {
            isDeling.value = false;
          }
        },
      });
    },
  });
}

function changePage(page: number) {
  currentPage.value = page;
}

function changePageSize(size: number) {
  pageSize.value = size;
  currentPage.value = 1;
}

function resetPage() {
  currentPage.value = 1;
  searchText.value = "";
  statusFilter.value = "all";
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
          @click="createDialogShow = true"
        >
          <template #icon>
            <n-icon :component="AddCircleOutline" />
          </template>
          {{ t("keys.addKey") }}
        </n-button>
        <n-button
          type="error"
          size="small"
          secondary
          :disabled="!selectedGroup"
          @click="deleteDialogShow = true"
        >
          <template #icon>
            <n-icon :component="RemoveCircleOutline" />
          </template>
          {{ t("keys.deleteKey") }}
        </n-button>
      </div>
      <div class="toolbar-right">
        <div class="toolbar-controls">
          <n-select
            v-model:value="statusFilter"
            :options="statusOptions"
            size="small"
            class="status-filter"
            :placeholder="t('keys.allStatus')"
            :disabled="!selectedGroup || loading"
          />
          <n-input-group class="search-control">
            <n-input
              v-model:value="searchText"
              :placeholder="t('keys.keyExactMatch')"
              size="small"
              clearable
              :disabled="!selectedGroup || loading"
              @keyup.enter="handleSearchInput"
            >
              <template #prefix>
                <n-icon :component="Search" />
              </template>
            </n-input>
            <n-button
              type="primary"
              ghost
              size="small"
              :disabled="!selectedGroup || loading"
              @click="handleSearchInput"
            >
              {{ t("common.search") }}
            </n-button>
          </n-input-group>
          <n-dropdown :options="moreOptions" trigger="click" @select="handleMoreAction">
            <n-button
              size="small"
              tertiary
              :disabled="!selectedGroup || loading"
              :aria-label="t('common.more')"
            >
              <template #icon>
                <span style="font-size: 16px; font-weight: bold">⋯</span>
              </template>
            </n-button>
          </n-dropdown>
        </div>
      </div>
    </div>

    <!-- 密钥卡片网格 -->
    <div class="keys-grid-container">
      <n-spin :show="loading">
        <div v-if="loadError && !loading" class="error-container" role="alert">
          <n-alert type="error" :title="t('keys.keyListLoadFailed')">
            <n-button size="small" secondary type="error" @click="loadKeys">
              {{ t("common.retry") }}
            </n-button>
          </n-alert>
        </div>
        <div v-else-if="keys.length === 0 && !loading" class="empty-container">
          <n-empty :description="t('keys.noMatchingKeys')" />
        </div>
        <div v-else class="keys-grid">
          <div
            v-for="key in keys"
            :key="key.id"
            class="key-card"
            :class="getStatusClass(key.status)"
          >
            <!-- 主要信息行：Key + 快速操作 -->
            <div class="key-main">
              <div class="key-section">
                <n-tag v-if="key.status === 'active'" type="success" :bordered="false" round>
                  <template #icon>
                    <n-icon :component="CheckmarkCircle" />
                  </template>
                  {{ t("keys.validShort") }}
                </n-tag>
                <n-tag v-else :bordered="false" round>
                  <template #icon>
                    <n-icon :component="AlertCircleOutline" />
                  </template>
                  {{ t("keys.invalidShort") }}
                </n-tag>
                <n-input class="key-text" :value="getDisplayValue(key)" readonly size="small" />
                <div class="quick-actions">
                  <n-button
                    size="tiny"
                    text
                    @click="editKeyNotes(key)"
                    :aria-label="t('keys.editNotes')"
                    :title="t('keys.editNotes')"
                  >
                    <template #icon>
                      <n-icon :component="Pencil" />
                    </template>
                  </n-button>
                  <n-button
                    size="tiny"
                    text
                    @click="toggleKeyVisibility(key)"
                    :aria-label="t('keys.showHide')"
                    :title="t('keys.showHide')"
                  >
                    <template #icon>
                      <n-icon :component="key.is_visible ? EyeOffOutline : EyeOutline" />
                    </template>
                  </n-button>
                  <n-button
                    size="tiny"
                    text
                    @click="copyKey(key)"
                    :aria-label="t('common.copy')"
                    :title="t('common.copy')"
                  >
                    <template #icon>
                      <n-icon :component="CopyOutline" />
                    </template>
                  </n-button>
                </div>
              </div>
            </div>

            <!-- 统计信息 + 操作按钮行 -->
            <div class="key-bottom">
              <div class="key-stats">
                <span class="stat-item">
                  {{ t("keys.requestsShort") }}
                  <strong>{{ key.request_count }}</strong>
                </span>
                <span class="stat-item">
                  {{ t("keys.failuresShort") }}
                  <strong>{{ key.failure_count }}</strong>
                </span>
                <span class="stat-item">
                  {{ key.last_used_at ? formatRelativeTime(key.last_used_at) : t("keys.unused") }}
                </span>
              </div>
              <n-button-group class="key-actions">
                <n-button
                  round
                  tertiary
                  type="info"
                  size="tiny"
                  @click="testKey(key)"
                  :title="t('keys.testKey')"
                >
                  {{ t("keys.testShort") }}
                </n-button>
                <n-button
                  v-if="key.status !== 'active'"
                  tertiary
                  size="tiny"
                  @click="restoreKey(key)"
                  :title="t('keys.restoreKey')"
                  type="warning"
                >
                  {{ t("keys.restoreShort") }}
                </n-button>
                <n-button
                  round
                  tertiary
                  size="tiny"
                  type="error"
                  @click="deleteKey(key)"
                  :title="t('keys.deleteKey')"
                >
                  {{ t("common.deleteShort") }}
                </n-button>
              </n-button-group>
            </div>
          </div>
        </div>
      </n-spin>
    </div>

    <!-- 分页 -->
    <div v-if="!loadError" class="pagination-container">
      <div class="pagination-info">
        <span>{{ t("keys.totalRecords", { total }) }}</span>
        <n-select
          v-if="total > 0"
          v-model:value="pageSize"
          :options="[
            { label: t('keys.recordsPerPage', { count: 12 }), value: 12 },
            { label: t('keys.recordsPerPage', { count: 24 }), value: 24 },
            { label: t('keys.recordsPerPage', { count: 60 }), value: 60 },
            { label: t('keys.recordsPerPage', { count: 120 }), value: 120 },
          ]"
          size="small"
          class="page-size-select"
          @update:value="changePageSize"
        />
      </div>
      <div v-if="total > 0" class="pagination-controls">
        <n-button size="small" :disabled="currentPage <= 1" @click="changePage(currentPage - 1)">
          {{ t("common.previousPage") }}
        </n-button>
        <span class="page-info">
          {{ t("keys.pageInfo", { current: currentPage, total: totalPages }) }}
        </span>
        <n-button
          size="small"
          :disabled="currentPage >= totalPages"
          @click="changePage(currentPage + 1)"
        >
          {{ t("common.nextPage") }}
        </n-button>
      </div>
    </div>

    <key-create-dialog
      v-if="selectedGroup?.id"
      v-model:show="createDialogShow"
      :group-id="selectedGroup.id"
      :group-name="getGroupDisplayName(selectedGroup!)"
      @success="loadKeys"
    />

    <key-delete-dialog
      v-if="selectedGroup?.id"
      v-model:show="deleteDialogShow"
      :group-id="selectedGroup.id"
      :group-name="getGroupDisplayName(selectedGroup!)"
      @success="handleBatchDeleteSuccess"
    />
  </div>

  <!-- 备注编辑对话框 -->
  <n-modal
    v-model:show="notesDialogShow"
    preset="dialog"
    class="notes-dialog"
    :title="t('keys.editKeyNotes')"
  >
    <n-input
      v-model:value="editingNotes"
      type="textarea"
      :placeholder="t('keys.enterNotes')"
      :rows="3"
      maxlength="255"
      show-count
    />
    <template #action>
      <n-button @click="notesDialogShow = false">{{ t("common.cancel") }}</n-button>
      <n-button type="primary" @click="saveKeyNotes">{{ t("common.save") }}</n-button>
    </template>
  </n-modal>
</template>

<style scoped>
.key-table-container {
  display: flex;
  height: 100%;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border-color-light);
  border-color: var(--border-color-light);
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
  background: var(--card-bg-solid);
}

.toolbar :deep(.n-button) {
  font-weight: 500;
}

.toolbar-left,
.toolbar-controls {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.toolbar-left {
  flex-shrink: 0;
}

.toolbar-right {
  display: flex;
  min-width: 0;
  flex: 1;
  justify-content: flex-end;
}

.status-filter {
  width: 120px;
}

.search-control {
  width: 280px;
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
  gap: 10px;
  padding: 14px;
  border: 1px solid var(--border-color-light);
  border-radius: 12px;
  background: var(--card-bg-solid);
  box-shadow: none;
  transition: box-shadow var(--motion-fast) var(--ease-out);
}

.key-card:hover {
  box-shadow: var(--shadow-md);
}

.key-card.status-valid {
  border-color: var(--success-border);
}

.key-card.status-error {
  border-color: var(--error-border);
}

.key-card.status-invalid {
  border-color: var(--invalid-border);
}

.key-main,
.key-section,
.key-bottom {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.key-main,
.key-bottom {
  justify-content: space-between;
}

.key-section,
.key-stats {
  min-width: 0;
  flex: 1;
}

.key-stats {
  display: flex;
  gap: var(--space-2);
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 12px;
}

.stat-item {
  color: var(--text-secondary);
  white-space: nowrap;
}

.stat-item strong {
  color: var(--text-primary);
  font-weight: 600;
}

.key-actions,
.quick-actions {
  flex-shrink: 0;
}

.key-actions :deep(.n-button) {
  padding-inline: 6px;
}

.key-text,
:deep(.n-input__input-el) {
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, Courier, monospace;
}

.key-text {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-weight: 500;
  white-space: nowrap;
}

:deep(.n-input__input-el) {
  font-size: 13px;
}

.quick-actions {
  display: flex;
  gap: var(--space-1);
}

.quick-actions :deep(.n-button) {
  min-width: 32px;
  min-height: 32px;
}

.empty-container,
.error-container {
  display: grid;
  min-height: 14rem;
  place-items: center;
}

.error-container :deep(.n-alert) {
  width: min(100%, 32rem);
}

.pagination-container {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color-light);
  background: var(--card-bg-solid);
}

.pagination-info,
.pagination-controls {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.pagination-info,
.page-info {
  color: var(--text-secondary);
  font-size: 12px;
}

.page-size-select {
  width: 112px;
}

.notes-dialog {
  width: min(520px, calc(100vw - 2rem));
  max-height: calc(100dvh - 2rem);
  overflow-y: auto;
}

@media (max-width: 768px) {
  .toolbar {
    flex-direction: column;
    align-items: stretch;
    padding: var(--space-3);
  }

  .toolbar-left,
  .toolbar-right,
  .toolbar-controls {
    width: 100%;
  }

  .toolbar-left :deep(.n-button) {
    min-height: 44px;
    flex: 1;
  }

  .toolbar-controls {
    align-items: stretch;
    flex-wrap: wrap;
  }

  .status-filter,
  .search-control {
    width: 100%;
  }

  .toolbar-controls > :deep(.n-dropdown) {
    margin-left: auto;
  }

  .toolbar-controls :deep(.n-button),
  .toolbar-controls :deep(.n-base-selection),
  .toolbar-controls :deep(.n-input) {
    min-height: 44px;
  }

  .keys-grid-container {
    padding: var(--space-3);
  }

  .keys-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
  }

  .key-section,
  .key-bottom {
    align-items: stretch;
    flex-direction: column;
  }

  .key-text,
  .quick-actions,
  .key-stats,
  .key-actions {
    width: 100%;
  }

  .quick-actions {
    justify-content: flex-end;
  }

  .quick-actions :deep(.n-button),
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
    gap: 0.75rem;
  }

  .pagination-info,
  .pagination-controls {
    width: 100%;
    justify-content: space-between;
  }

  .pagination-controls :deep(.n-button) {
    min-height: 44px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .key-card {
    transition: none;
  }
}
</style>
