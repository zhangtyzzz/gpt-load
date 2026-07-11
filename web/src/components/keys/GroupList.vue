<script setup lang="ts">
import { keysApi } from "@/api/keys";
import type { Group } from "@/types/models";
import { getGroupDisplayName } from "@/utils/display";
import { Add, LinkOutline, Search } from "@vicons/ionicons5";
import { NButton, NCard, NEmpty, NIcon, NInput, NSpin, NTag } from "naive-ui";
import { computed, onBeforeUpdate, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import AggregateGroupModal from "./AggregateGroupModal.vue";
import GroupFormModal from "./GroupFormModal.vue";

const { t } = useI18n();

interface Props {
  groups: Group[];
  selectedGroup: Group | null;
  loading?: boolean;
}

interface Emits {
  (e: "group-select", group: Group): void;
  (e: "refresh"): void;
  (e: "refresh-and-select", groupId: number): void;
}

const props = withDefaults(defineProps<Props>(), {
  loading: false,
});

const emit = defineEmits<Emits>();

const searchText = ref("");
const showGroupModal = ref(false);
// 存储分组项 DOM 元素的引用
const groupItemRefs = ref<Map<number, HTMLElement>>(new Map());
const showAggregateGroupModal = ref(false);
const displayGroups = ref<Group[]>([]);
const draggingGroupId = ref<number | null>(null);
const dropTarget = ref<{ groupId: number; position: "before" | "after" } | null>(null);
const savingOrder = ref(false);
const suspendAutoScroll = ref(false);

const isTouchDevice = computed(() => {
  if (typeof window === "undefined") {
    return false;
  }
  return "ontouchstart" in window || navigator.maxTouchPoints > 0;
});

const hasSearchFilter = computed(() => Boolean(searchText.value.trim()));

const dragAvailable = computed(
  () => !hasSearchFilter.value && !isTouchDevice.value && displayGroups.value.length > 1
);

const canDrag = computed(() => !props.loading && !savingOrder.value && dragAvailable.value);

watch(
  () => props.groups,
  groups => {
    if (savingOrder.value) {
      return;
    }
    displayGroups.value = groups.map(group => ({ ...group }));
    if (suspendAutoScroll.value) {
      suspendAutoScroll.value = false;
    }
  },
  {
    immediate: true,
    deep: true,
  }
);

onBeforeUpdate(() => {
  groupItemRefs.value.clear();
});

const filteredGroups = computed(() => {
  if (!searchText.value.trim()) {
    return displayGroups.value;
  }
  const search = searchText.value.toLowerCase().trim();
  return displayGroups.value.filter(
    group =>
      group.name.toLowerCase().includes(search) ||
      group.display_name?.toLowerCase().includes(search)
  );
});

// 监听选中项 ID 的变化，并自动滚动到该项
watch(
  () => props.selectedGroup?.id,
  id => {
    if (!id || displayGroups.value.length === 0 || suspendAutoScroll.value) {
      return;
    }

    const element = groupItemRefs.value.get(id);
    if (element) {
      element.scrollIntoView({
        behavior: globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches
          ? "auto"
          : "smooth",
        block: "nearest", // 将元素滚动到最近的边缘
      });
    }
  },
  {
    flush: "post", // 确保在 DOM 更新后执行回调
    immediate: true, // 立即执行一次以处理初始加载
  }
);

function handleGroupClick(group: Group) {
  if (draggingGroupId.value || savingOrder.value) {
    return;
  }
  emit("group-select", group);
}

// 获取渠道类型的标签颜色
function getChannelTagType(channelType: string) {
  switch (channelType) {
    case "openai":
    case "openai-response":
      return "success";
    case "gemini":
      return "info";
    case "anthropic":
      return "warning";
    default:
      return "default";
  }
}

function openCreateGroupModal() {
  showGroupModal.value = true;
}

function openCreateAggregateGroupModal() {
  showAggregateGroupModal.value = true;
}

function handleGroupCreated(group: Group) {
  showGroupModal.value = false;
  showAggregateGroupModal.value = false;
  if (group?.id) {
    emit("refresh-and-select", group.id);
  }
}

function setGroupItemRef(el: Element | null, groupId?: number) {
  if (el instanceof HTMLElement && groupId) {
    groupItemRefs.value.set(groupId, el);
  }
}

function reorderInMemory(
  sourceGroupId: number,
  targetGroupId: number,
  position: "before" | "after"
): boolean {
  const sourceIndex = displayGroups.value.findIndex(group => group.id === sourceGroupId);
  const targetIndex = displayGroups.value.findIndex(group => group.id === targetGroupId);

  if (sourceIndex < 0 || targetIndex < 0) {
    return false;
  }

  const reordered = [...displayGroups.value];
  const [moved] = reordered.splice(sourceIndex, 1);

  let insertIndex = targetIndex;
  if (sourceIndex < targetIndex) {
    insertIndex -= 1;
  }
  if (position === "after") {
    insertIndex += 1;
  }

  if (insertIndex < 0) {
    insertIndex = 0;
  }
  if (insertIndex > reordered.length) {
    insertIndex = reordered.length;
  }

  if (insertIndex === sourceIndex) {
    return false;
  }

  reordered.splice(insertIndex, 0, moved);
  displayGroups.value = reordered;
  return true;
}

async function persistGroupOrder(previousOrder: Group[]) {
  const previousSortMap = new Map<number, number>();
  previousOrder.forEach(group => {
    if (group.id) {
      previousSortMap.set(group.id, group.sort);
    }
  });

  const items: { id: number; sort: number }[] = [];
  displayGroups.value.forEach((group, index) => {
    if (!group.id) {
      return;
    }
    const targetSort = (index + 1) * 10;
    if (previousSortMap.get(group.id) !== targetSort) {
      items.push({ id: group.id, sort: targetSort });
    }
    group.sort = targetSort;
  });

  if (items.length === 0) {
    suspendAutoScroll.value = false;
    return;
  }

  try {
    savingOrder.value = true;
    await keysApi.reorderGroups(items);
    window.$message?.success(t("keys.dragSortSaved"));
    emit("refresh");
  } catch (error) {
    console.error("Failed to reorder groups:", error);
    displayGroups.value = previousOrder.map(group => ({ ...group }));
    window.$message?.error(t("keys.dragSortSaveFailed"));
    emit("refresh");
  } finally {
    savingOrder.value = false;
    suspendAutoScroll.value = false;
  }
}

function handleDragStart(event: DragEvent, groupId?: number) {
  if (!canDrag.value || !groupId) {
    return;
  }

  event.stopPropagation();
  draggingGroupId.value = groupId;
  dropTarget.value = null;
  suspendAutoScroll.value = true;

  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", String(groupId));
  }
}

function resolveDropPosition(event: DragEvent, targetGroupId: number): "before" | "after" {
  const element = groupItemRefs.value.get(targetGroupId);
  if (!element) {
    return "after";
  }
  const rect = element.getBoundingClientRect();
  return event.clientY < rect.top + rect.height / 2 ? "before" : "after";
}

function handleDragOver(event: DragEvent, targetGroupId?: number) {
  if (!canDrag.value || !draggingGroupId.value || !targetGroupId) {
    return;
  }

  event.preventDefault();
  event.stopPropagation();

  const nextPosition = resolveDropPosition(event, targetGroupId);
  if (
    !dropTarget.value ||
    dropTarget.value.groupId !== targetGroupId ||
    dropTarget.value.position !== nextPosition
  ) {
    dropTarget.value = { groupId: targetGroupId, position: nextPosition };
  }

  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = "move";
  }
}

async function handleDrop(event: DragEvent, targetGroupId?: number) {
  event.preventDefault();
  event.stopPropagation();

  const sourceGroupId = draggingGroupId.value;
  const target = dropTarget.value;
  draggingGroupId.value = null;
  dropTarget.value = null;

  if (
    !canDrag.value ||
    !sourceGroupId ||
    !targetGroupId ||
    !target ||
    sourceGroupId === targetGroupId
  ) {
    if (!savingOrder.value) {
      suspendAutoScroll.value = false;
    }
    return;
  }

  const previousOrder = displayGroups.value.map(group => ({ ...group }));
  const changed = reorderInMemory(sourceGroupId, targetGroupId, target.position);
  if (!changed) {
    suspendAutoScroll.value = false;
    return;
  }

  await persistGroupOrder(previousOrder);
}

function handleDragEnd() {
  draggingGroupId.value = null;
  dropTarget.value = null;
  if (!savingOrder.value) {
    suspendAutoScroll.value = false;
  }
}

async function handleKeyboardReorder(groupId: number | undefined, direction: -1 | 1) {
  if (!canDrag.value || !groupId) {
    return;
  }

  const sourceIndex = displayGroups.value.findIndex(group => group.id === groupId);
  const targetIndex = sourceIndex + direction;
  if (sourceIndex < 0 || targetIndex < 0 || targetIndex >= displayGroups.value.length) {
    return;
  }

  const targetGroupId = displayGroups.value[targetIndex].id;
  if (!targetGroupId) {
    return;
  }

  const previousOrder = displayGroups.value.map(group => ({ ...group }));
  suspendAutoScroll.value = true;
  const changed = reorderInMemory(groupId, targetGroupId, direction < 0 ? "before" : "after");
  if (!changed) {
    suspendAutoScroll.value = false;
    return;
  }

  await persistGroupOrder(previousOrder);
}

defineExpose({ openCreateGroupModal });
</script>

<template>
  <div class="group-list-container">
    <n-card class="group-list-card modern-card" :bordered="false" size="small">
      <!-- 搜索框 -->
      <div class="search-section">
        <n-input
          v-model:value="searchText"
          :placeholder="t('keys.searchGroupPlaceholder')"
          size="small"
          clearable
        >
          <template #prefix>
            <n-icon :component="Search" />
          </template>
        </n-input>
      </div>

      <!-- 分组列表 -->
      <div class="groups-section">
        <n-spin :show="loading" size="small">
          <div v-if="filteredGroups.length === 0 && !loading" class="empty-container">
            <n-empty
              size="small"
              :description="searchText ? t('keys.noMatchingGroups') : t('keys.noGroups')"
            />
          </div>
          <ul v-else class="groups-list" :aria-busy="savingOrder">
            <li
              v-for="group in filteredGroups"
              :key="group.id"
              class="group-item"
              :class="{
                active: selectedGroup?.id === group.id,
                aggregate: group.group_type === 'aggregate',
                dragging: draggingGroupId === group.id,
                'drop-before':
                  dropTarget?.groupId === group.id &&
                  dropTarget?.position === 'before' &&
                  draggingGroupId !== group.id,
                'drop-after':
                  dropTarget?.groupId === group.id &&
                  dropTarget?.position === 'after' &&
                  draggingGroupId !== group.id,
              }"
              @dragover="handleDragOver($event, group.id)"
              @drop="handleDrop($event, group.id)"
              :ref="
                el => {
                  setGroupItemRef(el as Element | null, group.id);
                }
              "
            >
              <button
                v-if="dragAvailable"
                type="button"
                class="group-icon"
                :disabled="!canDrag"
                :draggable="canDrag"
                :aria-label="`${t('keys.dragHandle')}: ↑ / ↓`"
                @click.stop
                @dragstart="handleDragStart($event, group.id)"
                @dragend="handleDragEnd"
                @keydown.up.stop.prevent="handleKeyboardReorder(group.id, -1)"
                @keydown.down.stop.prevent="handleKeyboardReorder(group.id, 1)"
              >
                <span v-if="group.group_type === 'aggregate'">🔗</span>
                <span v-else-if="group.channel_type === 'openai'">🤖</span>
                <span v-else-if="group.channel_type === 'openai-response'">🔁</span>
                <span v-else-if="group.channel_type === 'gemini'">💎</span>
                <span v-else-if="group.channel_type === 'anthropic'">🧠</span>
                <span v-else>🔧</span>
              </button>
              <div v-else class="group-icon" aria-hidden="true">
                <span v-if="group.group_type === 'aggregate'">🔗</span>
                <span v-else-if="group.channel_type === 'openai'">🤖</span>
                <span v-else-if="group.channel_type === 'openai-response'">🔁</span>
                <span v-else-if="group.channel_type === 'gemini'">💎</span>
                <span v-else-if="group.channel_type === 'anthropic'">🧠</span>
                <span v-else>🔧</span>
              </div>
              <button
                type="button"
                class="group-select-control"
                :aria-pressed="selectedGroup?.id === group.id"
                @click="handleGroupClick(group)"
              >
                <div class="group-content">
                  <div class="group-name">{{ getGroupDisplayName(group) }}</div>
                  <div class="group-meta">
                    <n-tag size="tiny" :type="getChannelTagType(group.channel_type)">
                      {{ group.channel_type }}
                    </n-tag>
                    <n-tag v-if="group.group_type === 'aggregate'" size="tiny" type="warning" round>
                      {{ t("keys.aggregateGroup") }}
                    </n-tag>
                    <span v-if="group.group_type !== 'aggregate'" class="group-id">
                      #{{ group.name }}
                    </span>
                  </div>
                </div>
              </button>
            </li>
          </ul>
        </n-spin>
      </div>

      <!-- 添加分组按钮 -->
      <div class="add-section">
        <n-button type="primary" size="small" block @click="openCreateGroupModal">
          <template #icon>
            <n-icon :component="Add" />
          </template>
          {{ t("keys.createGroup") }}
        </n-button>
        <n-button secondary size="small" block @click="openCreateAggregateGroupModal">
          <template #icon>
            <n-icon :component="LinkOutline" />
          </template>
          {{ t("keys.createAggregateGroup") }}
        </n-button>
      </div>
    </n-card>
    <group-form-modal v-model:show="showGroupModal" @success="handleGroupCreated" />
    <aggregate-group-modal
      v-model:show="showAggregateGroupModal"
      :groups="groups"
      @success="handleGroupCreated"
    />
  </div>
</template>

<style scoped>
:deep(.n-card__content) {
  height: 100%;
}

.group-list-container,
.group-list-card {
  height: 100%;
}

.group-list-card {
  display: flex;
  flex-direction: column;
  background: var(--card-bg-solid);
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-lg);
  box-shadow: var(--shadow-sm);
}

.group-list-card:hover {
  box-shadow: var(--shadow-sm);
  transform: none;
}

.search-section {
  min-height: 41px;
}

.groups-section {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.empty-container {
  padding: var(--space-5) 0;
}

.groups-list {
  display: flex;
  width: 100%;
  max-height: 100%;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  overflow-y: auto;
  list-style: none;
}

.group-item,
.group-item.aggregate {
  position: relative;
  display: flex;
  box-sizing: border-box;
  min-height: 50px;
  align-items: center;
  gap: var(--space-2);
  padding: 0.375rem;
  border: 1px solid transparent;
  border-radius: 10px;
  background: transparent;
  color: var(--text-primary);
  transition:
    background var(--motion-fast) var(--ease-out),
    color var(--motion-fast) var(--ease-out),
    opacity var(--motion-fast) var(--ease-out);
}

.group-item:hover,
.group-item.aggregate:hover,
:root.dark .group-item:hover,
:root.dark .group-item.aggregate:hover {
  border-color: transparent;
  background: var(--hover-bg);
}

.group-item.dragging {
  opacity: 0.58;
}

.group-item.drop-before::before,
.group-item.drop-after::after {
  position: absolute;
  right: var(--space-2);
  left: var(--space-2);
  z-index: 1;
  height: 3px;
  border-radius: 999px;
  background: var(--primary-color);
  box-shadow: 0 0 0 2px var(--primary-color-suppl);
  content: "";
  pointer-events: none;
}

.group-item.drop-before::before {
  top: -3px;
}

.group-item.drop-after::after {
  bottom: -3px;
}

.group-item.active,
.group-item.aggregate.active,
:root.dark .group-item.active,
:root.dark .group-item.aggregate.active {
  border-color: transparent;
  background: var(--primary-color);
  box-shadow: none;
  color: white;
}

.group-icon {
  display: flex;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  border: 0;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: inherit;
  font: inherit;
  font-size: 16px;
  user-select: none;
}

button.group-icon {
  cursor: grab;
}

button.group-icon:disabled {
  cursor: wait;
  opacity: 0.55;
}

button.group-icon:active,
.group-item.dragging button.group-icon {
  cursor: grabbing;
}

.group-select-control {
  display: flex;
  min-width: 0;
  min-height: 40px;
  flex: 1;
  align-items: center;
  padding: 0;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.group-content {
  min-width: 0;
  flex: 1;
}

.group-name {
  margin-bottom: var(--space-1);
  overflow: hidden;
  font-size: 14px;
  font-weight: 600;
  line-height: 1.2;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 10px;
}

.group-id {
  color: var(--text-secondary);
  opacity: 0.8;
}

.group-item.active .group-icon {
  background: color-mix(in srgb, white 20%, transparent);
}

.group-item.active .group-id {
  color: white;
  opacity: 0.9;
}

.add-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color-light);
}

.groups-list::-webkit-scrollbar {
  width: 4px;
}

.groups-list::-webkit-scrollbar-track {
  background: transparent;
}

.groups-list::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: var(--scrollbar-bg);
}

@media (max-width: 768px) {
  .group-item {
    min-height: 56px;
  }

  .group-icon,
  .group-select-control {
    min-height: 44px;
  }

  .group-icon {
    width: 44px;
    height: 44px;
    flex-basis: 44px;
  }

  .add-section :deep(.n-button) {
    min-height: 44px;
  }
}
</style>
