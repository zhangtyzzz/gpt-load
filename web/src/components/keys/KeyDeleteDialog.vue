<script setup lang="ts">
import { keysApi } from "@/api/keys";
import { appState } from "@/utils/app-state";
import { Close } from "@vicons/ionicons5";
import { NButton, NCard, NIcon, NInput, NModal } from "naive-ui";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";

interface Props {
  show: boolean;
  groupId: number;
  groupName?: string;
}

interface Emits {
  (e: "update:show", value: boolean): void;
  (e: "success"): void;
}

const props = defineProps<Props>();

const emit = defineEmits<Emits>();

const { t } = useI18n();

const loading = ref(false);
const keysText = ref("");

// 监听弹窗显示状态
watch(
  () => props.show,
  show => {
    if (show) {
      resetForm();
    }
  }
);

// 重置表单
function resetForm() {
  keysText.value = "";
}

// 关闭弹窗
function handleClose() {
  emit("update:show", false);
}

// 提交表单
async function handleSubmit() {
  if (loading.value || !keysText.value.trim()) {
    return;
  }

  try {
    loading.value = true;

    await keysApi.deleteKeysAsync(props.groupId, keysText.value);
    resetForm();

    handleClose();
    window.$message.success(t("keys.deleteTaskStarted"));
    appState.taskPollingTrigger++;
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <n-modal :show="show" @update:show="handleClose" class="form-modal">
    <n-card
      class="form-modal-card"
      :title="t('keys.deleteKeysFromGroup', { group: groupName || t('keys.currentGroup') })"
      :bordered="false"
      size="huge"
      role="dialog"
      aria-modal="true"
    >
      <template #header-extra>
        <n-button quaternary circle :aria-label="t('common.close')" @click="handleClose">
          <template #icon>
            <n-icon :component="Close" />
          </template>
        </n-button>
      </template>

      <n-input
        v-model:value="keysText"
        type="textarea"
        :placeholder="t('keys.enterKeysToDeletePlaceholder')"
        :rows="8"
        class="modal-input"
      />

      <template #footer>
        <div class="modal-footer">
          <n-button @click="handleClose">{{ t("common.cancel") }}</n-button>
          <n-button type="error" @click="handleSubmit" :loading="loading" :disabled="!keysText">
            {{ t("common.delete") }}
          </n-button>
        </div>
      </template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.form-modal-card {
  display: flex;
  width: min(800px, calc(100vw - 2rem));
  max-height: calc(100dvh - 2rem);
  flex-direction: column;
  overflow: hidden;
}

:deep(.n-input) {
  --n-border-radius: var(--border-radius-sm);
}

:deep(.n-card-header) {
  flex-shrink: 0;
  padding: var(--space-3) var(--space-5);
  border-bottom: 1px solid var(--border-color-light);
}

:deep(.n-card__content) {
  min-height: 0;
  overflow-y: auto;
}

:deep(.n-card__footer) {
  flex-shrink: 0;
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color-light);
}

.modal-input {
  margin-top: var(--space-5);
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
}

@media (max-width: 768px) {
  .form-modal-card {
    width: min(800px, calc(100vw - 2rem));
    max-height: calc(100dvh - 1rem);
  }

  :deep(.n-card-header),
  :deep(.n-card__content),
  :deep(.n-card__footer) {
    padding-inline: var(--space-3);
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
