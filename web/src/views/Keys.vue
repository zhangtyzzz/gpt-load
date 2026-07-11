<script setup lang="ts">
import { keysApi } from "@/api/keys";
import EncryptionMismatchAlert from "@/components/EncryptionMismatchAlert.vue";
import GroupInfoCard from "@/components/keys/GroupInfoCard.vue";
import GroupList from "@/components/keys/GroupList.vue";
import KeyTable from "@/components/keys/KeyTable.vue";
import SubGroupTable from "@/components/keys/SubGroupTable.vue";
import type { Group, SubGroupInfo } from "@/types/models";
import { AddOutline, AlbumsOutline } from "@vicons/ionicons5";
import { NAlert, NButton, NIcon, NSpin } from "naive-ui";
import { onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

const groups = ref<Group[]>([]);
const loading = ref(true);
const groupLoadError = ref(false);
const selectedGroup = ref<Group | null>(null);
const subGroups = ref<SubGroupInfo[]>([]);
const loadingSubGroups = ref(false);
const subGroupLoadError = ref(false);
const router = useRouter();
const route = useRoute();
const { t } = useI18n();
const groupListRef = ref<InstanceType<typeof GroupList> | null>(null);

onMounted(async () => {
  await loadGroups();
});

async function loadGroups() {
  try {
    loading.value = true;
    groupLoadError.value = false;
    groups.value = await keysApi.getGroups();
    if (groups.value.length === 0) {
      selectedGroup.value = null;
      return;
    }
    const preferredGroupId = route.query.groupId || selectedGroup.value?.id;
    const preferredGroup = groups.value.find(g => String(g.id) === String(preferredGroupId));
    if (preferredGroup) {
      handleGroupSelect(preferredGroup);
    } else {
      handleGroupSelect(groups.value[0]);
    }
  } catch (error) {
    groupLoadError.value = true;
    console.error("Failed to load groups:", error);
    window.$message?.error(t("keys.loadFailed"));
  } finally {
    loading.value = false;
  }
}

async function loadSubGroups() {
  if (!selectedGroup.value?.id || selectedGroup.value.group_type !== "aggregate") {
    subGroupLoadError.value = false;
    loadingSubGroups.value = false;
    subGroups.value = [];
    return;
  }

  const groupId = selectedGroup.value.id;
  const requestId = ++latestSubGroupRequest;
  try {
    loadingSubGroups.value = true;
    subGroupLoadError.value = false;
    subGroups.value = [];
    const result = await keysApi.getSubGroups(groupId);
    if (requestId === latestSubGroupRequest && selectedGroup.value?.id === groupId) {
      subGroups.value = result;
    }
  } catch (error) {
    console.error("Failed to load sub groups:", error);
    if (requestId === latestSubGroupRequest && selectedGroup.value?.id === groupId) {
      subGroupLoadError.value = true;
      subGroups.value = [];
    }
  } finally {
    if (requestId === latestSubGroupRequest && selectedGroup.value?.id === groupId) {
      loadingSubGroups.value = false;
    }
  }
}

let latestSubGroupRequest = 0;

// 监听选中分组变化，加载子分组数据
watch(selectedGroup, async newGroup => {
  if (newGroup?.group_type === "aggregate") {
    await loadSubGroups();
  } else {
    subGroupLoadError.value = false;
    loadingSubGroups.value = false;
    subGroups.value = [];
  }
});

function handleGroupSelect(group: Group | null) {
  selectedGroup.value = group || null;
  if (String(group?.id) !== String(route.query.groupId)) {
    router.push({ name: "keys", query: { groupId: group?.id || "" } });
  }
}

async function refreshGroupsAndSelect(targetGroupId?: number, selectFirst = true) {
  await loadGroups();

  if (targetGroupId) {
    const targetGroup = groups.value.find(g => g.id === targetGroupId);
    if (targetGroup) {
      handleGroupSelect(targetGroup);
      return;
    }
  }

  if (selectedGroup.value) {
    const currentGroup = groups.value.find(g => g.id === selectedGroup.value?.id);
    if (currentGroup) {
      handleGroupSelect(currentGroup);
      if (currentGroup.group_type === "aggregate") {
        await loadSubGroups();
      }
      return;
    }
  }

  if (selectFirst && groups.value.length > 0) {
    handleGroupSelect(groups.value[0]);
  }
}

// 处理子分组选择，跳转到对应的分组
function handleSubGroupSelect(groupId: number) {
  const targetGroup = groups.value.find(g => g.id === groupId);
  if (targetGroup) {
    handleGroupSelect(targetGroup);
  }
}

// 处理聚合分组跳转，跳转到对应的聚合分组
function handleNavigateToGroup(groupId: number) {
  const targetGroup = groups.value.find(g => g.id === groupId);
  if (targetGroup) {
    handleGroupSelect(targetGroup);
  }
}
</script>

<template>
  <section class="page-shell" aria-labelledby="keys-title">
    <header class="page-heading">
      <div>
        <h1 id="keys-title" class="page-title">{{ t("keys.title") }}</h1>
        <p class="page-description">{{ t("keys.description") }}</p>
      </div>
    </header>

    <!-- 加密配置错误警告 -->
    <encryption-mismatch-alert style="margin-bottom: 16px" />

    <n-alert
      v-if="groupLoadError"
      type="error"
      :title="t('keys.loadFailed')"
      class="group-load-alert"
    >
      <div class="group-load-alert__content">
        <span>{{ t("keys.loadFailed") }}</span>
        <n-button size="small" :loading="loading" @click="loadGroups">
          {{ t("common.retry") }}
        </n-button>
      </div>
    </n-alert>

    <div
      v-if="loading && groups.length === 0"
      class="group-load-state surface-card"
      aria-busy="true"
    >
      <n-spin size="large" />
      <p>{{ t("common.loading") }}</p>
    </div>

    <div v-else-if="!groupLoadError || groups.length > 0" class="keys-container">
      <div class="sidebar">
        <group-list
          ref="groupListRef"
          :groups="groups"
          :selected-group="selectedGroup"
          :loading="loading"
          @group-select="handleGroupSelect"
          @refresh="() => refreshGroupsAndSelect()"
          @refresh-and-select="id => refreshGroupsAndSelect(id)"
        />
      </div>

      <!-- 右侧主内容区域，占80% -->
      <div class="main-content">
        <div v-if="!selectedGroup" class="group-empty surface-card">
          <div class="empty-state__content">
            <div class="empty-state__icon" aria-hidden="true">
              <n-icon :component="AlbumsOutline" />
            </div>
            <div>
              <h2 class="empty-state__title">{{ t("keys.noGroupSelected") }}</h2>
              <p class="empty-state__description">{{ t("keys.selectOrCreateGroup") }}</p>
            </div>
            <n-button type="primary" @click="groupListRef?.openCreateGroupModal()">
              <template #icon><n-icon :component="AddOutline" /></template>
              {{ t("keys.createFirstGroup") }}
            </n-button>
          </div>
        </div>

        <template v-else>
          <!-- 分组信息卡片，更紧凑 -->
          <div class="group-info">
            <group-info-card
              :group="selectedGroup"
              :groups="groups"
              :sub-groups="subGroups"
              @refresh="() => refreshGroupsAndSelect()"
              @delete="() => refreshGroupsAndSelect(undefined, true)"
              @copy-success="group => refreshGroupsAndSelect(group.id)"
              @navigate-to-group="handleNavigateToGroup"
            />
          </div>

          <!-- 密钥表格区域 / 子分组列表区域 -->
          <div class="key-table-section">
            <!-- 标准分组显示密钥列表 -->
            <key-table
              v-if="selectedGroup.group_type !== 'aggregate'"
              :selected-group="selectedGroup"
            />

            <!-- 聚合分组显示子分组列表 -->
            <template v-else>
              <n-alert
                v-if="subGroupLoadError"
                type="error"
                :title="t('subGroups.loadFailed')"
                class="subgroup-load-alert"
              >
                <div class="subgroup-load-alert__content">
                  <span>{{ t("subGroups.loadFailed") }}</span>
                  <n-button size="small" :loading="loadingSubGroups" @click="loadSubGroups">
                    {{ t("common.retry") }}
                  </n-button>
                </div>
              </n-alert>
              <sub-group-table
                v-else
                :selected-group="selectedGroup"
                :sub-groups="subGroups"
                :groups="groups"
                :loading="loadingSubGroups"
                @refresh="loadSubGroups"
                @group-select="handleSubGroupSelect"
              />
            </template>
          </div>
        </template>
      </div>
    </div>
  </section>
</template>

<style scoped>
.keys-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
}

.group-load-alert {
  margin-bottom: 1rem;
}

.group-load-alert__content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.subgroup-load-alert {
  margin-block: 0;
}

.subgroup-load-alert__content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.group-load-state {
  display: grid;
  min-height: min(32rem, calc(100vh - 210px));
  place-items: center;
  align-content: center;
  gap: 0.75rem;
  color: var(--text-secondary);
}

.sidebar {
  width: 100%;
  flex-shrink: 0;
}

.main-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}

.group-empty {
  display: grid;
  min-height: min(32rem, calc(100vh - 210px));
  place-items: center;
  padding: 2rem;
  text-align: center;
}

.group-info {
  flex-shrink: 0;
}

.key-table-section {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

@media (min-width: 768px) {
  .keys-container {
    flex-direction: row;
  }

  .sidebar {
    position: sticky;
    top: 82px;
    width: 264px;
    height: calc(100vh - 190px);
  }
}
</style>
