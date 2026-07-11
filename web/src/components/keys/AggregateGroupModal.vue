<script setup lang="ts">
import { keysApi } from "@/api/keys";
import ProxyKeysInput from "@/components/common/ProxyKeysInput.vue";
import { useChannelCatalog } from "@/composables/useChannelCatalog";
import { type Group } from "@/types/models";
import { Close } from "@vicons/ionicons5";
import { useMediaQuery } from "@vueuse/core";
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  useDialog,
  useMessage,
  type FormRules,
} from "naive-ui";
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { onBeforeRouteLeave } from "vue-router";

interface Props {
  show: boolean;
  group?: Group | null;
}

interface Emits {
  (e: "update:show", value: boolean): void;
  (e: "success", value: Group): void;
}

const props = withDefaults(defineProps<Props>(), {
  group: null,
});

const emit = defineEmits<Emits>();

const { t } = useI18n();
const message = useMessage();
const dialog = useDialog();
const loading = ref(false);
const initializing = ref(false);
const formRef = ref();
const savedSnapshot = ref("");
const formInitialized = ref(false);
const isMobile = useMediaQuery("(max-width: 768px)");
const { catalog, channelTypes, loadCatalog } = useChannelCatalog();
const aggregateChannelTypes = computed(() =>
  channelTypes.value.filter(channel => channel.capabilities.aggregate)
);
const channelTypeOptions = computed(() => {
  const options = aggregateChannelTypes.value.map(channel => ({
    label: channel.display_name,
    value: channel.id,
    disabled: false,
  }));
  if (
    props.group?.channel_type &&
    !options.some(item => item.value === props.group?.channel_type)
  ) {
    options.push({
      label: `${props.group.channel_type} (${t("channels.unavailable")})`,
      value: props.group.channel_type,
      disabled: true,
    });
  }
  return options;
});

// 默认表单数据
function createDefaultFormData() {
  const defaultChannel = aggregateChannelTypes.value.find(
    channel => channel.id === catalog.value.default_channel_type
  );
  return {
    name: "",
    display_name: "",
    description: "",
    channel_type: defaultChannel?.id || aggregateChannelTypes.value[0]?.id || "openai",
    sort: 1,
    proxy_keys: "",
  };
}

// 表单数据
const formData = reactive(createDefaultFormData());
const serializeForm = () => JSON.stringify(formData);
const isDirty = computed(
  () =>
    props.show &&
    formInitialized.value &&
    Boolean(savedSnapshot.value) &&
    serializeForm() !== savedSnapshot.value
);

let initializationRun = 0;
let beforeUnloadRegistered = false;
let discardConfirmation: Promise<boolean> | null = null;

function addBeforeUnloadListener() {
  if (!beforeUnloadRegistered) {
    globalThis.addEventListener("beforeunload", handleBeforeUnload);
    beforeUnloadRegistered = true;
  }
}

function removeBeforeUnloadListener() {
  if (beforeUnloadRegistered) {
    globalThis.removeEventListener("beforeunload", handleBeforeUnload);
    beforeUnloadRegistered = false;
  }
}

function clearDirtyTracking() {
  formInitialized.value = false;
  savedSnapshot.value = "";
  removeBeforeUnloadListener();
}

function captureSavedSnapshot() {
  savedSnapshot.value = serializeForm();
  formInitialized.value = true;
}

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!isDirty.value) {
    return;
  }
  event.preventDefault();
  event.returnValue = t("keys.groupFormLeaveWarning");
}

watch(isDirty, dirty => {
  if (dirty) {
    addBeforeUnloadListener();
  } else {
    removeBeforeUnloadListener();
  }
});

onBeforeUnmount(removeBeforeUnloadListener);

// 表单验证规则
const rules: FormRules = {
  name: [
    {
      required: true,
      message: t("keys.enterGroupName"),
      trigger: ["blur", "input"],
    },
    {
      pattern: /^[a-z0-9_-]{1,100}$/,
      message: t("keys.groupNamePattern"),
      trigger: ["blur", "input"],
    },
  ],
  channel_type: [
    {
      required: true,
      message: t("keys.selectChannelType"),
      trigger: ["blur", "change"],
    },
  ],
};

// 监听弹窗显示状态
watch(
  () => props.show,
  async show => {
    const run = ++initializationRun;
    if (!show) {
      initializing.value = false;
      clearDirtyTracking();
      return;
    }

    initializing.value = true;
    clearDirtyTracking();
    try {
      try {
        await loadCatalog();
      } catch {
        message.warning(t("channels.catalogLoadFailed"));
      }
      if (run !== initializationRun || !props.show) {
        return;
      }
      // 新建模式重置表单，编辑模式加载数据
      if (props.group) {
        loadGroupData();
      } else {
        resetForm();
      }
      await nextTick();
      if (run === initializationRun && props.show) {
        captureSavedSnapshot();
      }
    } finally {
      if (run === initializationRun) {
        initializing.value = false;
      }
    }
  },
  { immediate: true }
);

// 重置表单
function resetForm() {
  Object.assign(formData, createDefaultFormData());
}

// 加载分组数据（编辑模式）
function loadGroupData() {
  if (!props.group) {
    return;
  }

  Object.assign(formData, {
    name: props.group.name || "",
    display_name: props.group.display_name || "",
    description: props.group.description || "",
    channel_type: props.group.channel_type || "openai",
    sort: props.group.sort || 1,
    proxy_keys: props.group.proxy_keys || "",
  });
}

function closeWithoutPrompt() {
  clearDirtyTracking();
  emit("update:show", false);
}

function confirmDiscard(): Promise<boolean> {
  if (!isDirty.value) {
    return Promise.resolve(true);
  }
  if (discardConfirmation) {
    return discardConfirmation;
  }

  discardConfirmation = new Promise(resolve => {
    let settled = false;
    const finish = (discard: boolean) => {
      if (settled) {
        return;
      }
      settled = true;
      discardConfirmation = null;
      resolve(discard);
    };
    dialog.warning({
      title: t("keys.discardGroupChangesTitle"),
      content: t("keys.discardGroupChangesDescription"),
      positiveText: t("keys.discardGroupChanges"),
      negativeText: t("keys.continueEditingGroup"),
      positiveButtonProps: { type: "warning" },
      onPositiveClick: () => finish(true),
      onNegativeClick: () => finish(false),
      onClose: () => finish(false),
      onMaskClick: () => finish(false),
    });
  });
  return discardConfirmation;
}

async function requestClose() {
  if (await confirmDiscard()) {
    closeWithoutPrompt();
  }
}

function handleModalShowChange(show: boolean) {
  if (!show) {
    void requestClose();
  }
}

onBeforeRouteLeave(async () => {
  if (!(await confirmDiscard())) {
    return false;
  }
  clearDirtyTracking();
  return true;
});

// 提交表单
async function handleSubmit() {
  if (loading.value || initializing.value) {
    return;
  }

  try {
    if (typeof formRef.value?.validate === "function") {
      await formRef.value.validate();
    }

    loading.value = true;

    // 构建提交数据
    const submitData = {
      name: formData.name,
      display_name: formData.display_name,
      description: formData.description,
      channel_type: formData.channel_type,
      sort: formData.sort,
      proxy_keys: formData.proxy_keys,
      group_type: "aggregate" as const,
    };

    let result: Group;
    if (props.group) {
      // 编辑模式
      if (!props.group?.id) {
        message.error(t("keys.invalidGroup"));
        return;
      }
      result = await keysApi.updateGroup(props.group.id, submitData);
    } else {
      // 新建模式
      result = await keysApi.createGroup(submitData);
    }

    clearDirtyTracking();
    emit("success", result);
    closeWithoutPrompt();
  } finally {
    loading.value = false;
  }
}

defineExpose({ formData, handleSubmit, initializing, isDirty, requestClose });
</script>

<template>
  <n-modal :show="show" @update:show="handleModalShowChange" class="aggregate-group-modal">
    <n-card
      class="aggregate-group-card"
      :title="group ? t('keys.editAggregateGroup') : t('keys.createAggregateGroup')"
      :bordered="false"
      size="huge"
      role="dialog"
      aria-modal="true"
      :aria-busy="initializing"
    >
      <template #header-extra>
        <n-button quaternary circle :aria-label="t('common.close')" @click="requestClose">
          <template #icon>
            <n-icon :component="Close" />
          </template>
        </n-button>
      </template>

      <n-form
        ref="formRef"
        :model="formData"
        :rules="rules"
        :label-placement="isMobile ? 'top' : 'left'"
        :label-width="isMobile ? 'auto' : '120px'"
        :disabled="initializing"
      >
        <!-- 基础信息 -->
        <div class="form-section">
          <h4 class="section-title">{{ t("keys.basicInfo") }}</h4>

          <n-form-item :label="t('keys.groupName')" path="name">
            <n-input
              v-model:value="formData.name"
              :placeholder="t('keys.groupNamePlaceholder')"
              clearable
            />
          </n-form-item>

          <n-form-item :label="t('keys.displayName')">
            <n-input
              v-model:value="formData.display_name"
              :placeholder="t('keys.displayNamePlaceholder')"
              clearable
            />
          </n-form-item>

          <n-form-item :label="t('keys.channelType')" path="channel_type">
            <n-select
              v-model:value="formData.channel_type"
              :options="channelTypeOptions"
              :placeholder="t('keys.selectChannelType')"
              :disabled="!!props.group"
            />
          </n-form-item>

          <n-alert
            v-if="formData.channel_type === 'generic-http'"
            type="info"
            class="compatibility-alert"
          >
            {{ t("channels.aggregate.parentDerived") }}
          </n-alert>

          <n-form-item :label="t('keys.sortOrder')">
            <n-input-number
              v-model:value="formData.sort"
              :placeholder="t('keys.sortValue')"
              style="width: 100%"
            />
          </n-form-item>

          <n-form-item :label="t('keys.proxyKeys')">
            <proxy-keys-input v-model="formData.proxy_keys" />
          </n-form-item>

          <n-form-item :label="t('common.description')">
            <n-input
              v-model:value="formData.description"
              type="textarea"
              placeholder=""
              :rows="1"
              :autosize="{ minRows: 1, maxRows: 5 }"
              style="resize: none"
            />
          </n-form-item>
        </div>
      </n-form>

      <template #footer>
        <div class="modal-footer">
          <n-button @click="requestClose">{{ t("common.cancel") }}</n-button>
          <n-button
            type="primary"
            @click="handleSubmit"
            :loading="loading || initializing"
            :disabled="initializing"
          >
            {{ group ? t("common.update") : t("common.create") }}
          </n-button>
        </div>
      </template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.aggregate-group-card {
  display: flex;
  width: min(600px, calc(100vw - 2rem));
  max-height: calc(100dvh - 2rem);
  flex-direction: column;
  overflow: hidden;
}

.aggregate-group-card :deep(.n-card-header),
.aggregate-group-card :deep(.n-card__footer) {
  flex-shrink: 0;
}

.aggregate-group-card :deep(.n-card__content) {
  min-height: 0;
  overflow-y: auto;
}

.form-section {
  margin-top: var(--space-5);
}

.form-section:first-child {
  margin-top: 0;
}

.section-title {
  margin-bottom: var(--space-4);
  padding-bottom: var(--space-2);
  border-bottom: 1px solid var(--border-color-light);
  color: var(--text-primary);
  font-size: 1rem;
  font-weight: 600;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
}

.compatibility-alert {
  margin-bottom: var(--space-4);
}

@media (max-width: 768px) {
  .aggregate-group-card {
    width: min(600px, calc(100vw - 2rem));
    max-height: calc(100dvh - 1rem);
  }

  .aggregate-group-card :deep(.n-card-header),
  .aggregate-group-card :deep(.n-card__content),
  .aggregate-group-card :deep(.n-card__footer) {
    padding-inline: var(--space-3);
    padding-bottom: max(var(--space-3), env(safe-area-inset-bottom));
  }

  .modal-footer {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .modal-footer :deep(.n-button) {
    min-height: 44px;
  }
}
</style>
