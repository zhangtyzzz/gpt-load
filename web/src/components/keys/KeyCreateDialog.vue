<script setup lang="ts">
import { keysApi } from "@/api/keys";
import { appState } from "@/utils/app-state";
import { Close, CloudUploadOutline } from "@vicons/ionicons5";
import { NButton, NCard, NIcon, NInput, NModal, NUpload, type UploadFileInfo } from "naive-ui";
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
const inputMode = ref<"text" | "file">("text");
const fileList = ref<UploadFileInfo[]>([]);

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
  inputMode.value = "text";
  fileList.value = [];
}

// 关闭弹窗
function handleClose() {
  emit("update:show", false);
}

// 切换输入模式
function toggleInputMode() {
  if (inputMode.value === "text") {
    inputMode.value = "file";
    keysText.value = "";
  } else {
    inputMode.value = "text";
    fileList.value = [];
  }
}

// 文件上传前的检查
function beforeUpload(data: { file: UploadFileInfo; fileList: UploadFileInfo[] }) {
  if (!data.file.name?.endsWith(".txt")) {
    window.$message.error(t("keys.onlyTxtFileSupported"));
    return false;
  }
  return true;
}

// 文件变化处理
function handleFileChange(options: { fileList: UploadFileInfo[] }) {
  fileList.value = options.fileList;
}

// 提交表单
async function handleSubmit() {
  if (loading.value) {
    return;
  }

  if (inputMode.value === "text") {
    if (!keysText.value.trim()) {
      return;
    }
  } else {
    if (fileList.value.length === 0) {
      return;
    }
  }

  try {
    loading.value = true;

    if (inputMode.value === "text") {
      await keysApi.addKeysAsync(props.groupId, keysText.value);
    } else {
      const file = fileList.value[0].file as File;
      await keysApi.addKeysAsync(props.groupId, undefined, file);
    }

    resetForm();
    handleClose();
    window.$message.success(t("keys.importTaskStarted"));
    appState.taskPollingTrigger++;
  } finally {
    loading.value = false;
  }
}

// 计算提交按钮是否可用
function isSubmitDisabled() {
  if (inputMode.value === "text") {
    return !keysText.value.trim();
  } else {
    return fileList.value.length === 0;
  }
}
</script>

<template>
  <n-modal :show="show" @update:show="handleClose" class="form-modal">
    <n-card
      class="form-modal-card"
      :title="t('keys.addKeysToGroup', { group: groupName || t('keys.currentGroup') })"
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

      <!-- 文本输入模式 -->
      <n-input
        v-if="inputMode === 'text'"
        v-model:value="keysText"
        type="textarea"
        :placeholder="t('keys.enterKeysPlaceholder')"
        :rows="8"
        class="modal-input"
      />

      <!-- 文件上传模式 -->
      <n-upload
        v-else
        v-model:file-list="fileList"
        :max="1"
        accept=".txt"
        :before-upload="beforeUpload"
        @change="handleFileChange"
        class="modal-upload"
      >
        <div class="upload-area">
          <n-icon size="48" :component="CloudUploadOutline" class="upload-icon" />
          <div class="upload-text">{{ t("keys.clickOrDragFile") }}</div>
          <div class="upload-hint">{{ t("keys.onlyTxtFileSupported") }}</div>
        </div>
      </n-upload>

      <template #footer>
        <div class="modal-footer">
          <n-button @click="toggleInputMode" secondary>
            {{ inputMode === "text" ? t("keys.uploadFile") : t("keys.manualInput") }}
          </n-button>
          <div class="footer-actions">
            <n-button @click="handleClose">{{ t("common.cancel") }}</n-button>
            <n-button
              type="primary"
              @click="handleSubmit"
              :loading="loading"
              :disabled="isSubmitDisabled()"
            >
              {{ t("common.add") }}
            </n-button>
          </div>
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

.modal-input,
.modal-upload {
  margin-top: var(--space-5);
}

.upload-area {
  display: flex;
  min-height: 15rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: var(--space-10);
  border: 2px dashed var(--border-color);
  border-radius: var(--border-radius-md);
  background: var(--bg-secondary);
  cursor: pointer;
  transition:
    border-color var(--motion-fast) var(--ease-out),
    background var(--motion-fast) var(--ease-out);
}

.upload-area:hover {
  border-color: var(--success-color);
  background: var(--success-bg);
}

.upload-icon {
  color: var(--success-color);
}

.upload-text {
  margin-top: var(--space-3);
  color: var(--text-primary);
  font-size: 16px;
}

.upload-hint {
  margin-top: var(--space-2);
  color: var(--text-secondary);
  font-size: 14px;
}

.modal-footer,
.footer-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.modal-footer {
  justify-content: space-between;
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

  .upload-area {
    min-height: 11rem;
    padding: var(--space-6) var(--space-3);
  }

  .modal-footer {
    align-items: stretch;
    flex-direction: column;
  }

  .modal-footer > :deep(.n-button),
  .footer-actions :deep(.n-button) {
    min-height: 44px;
  }

  .footer-actions {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (prefers-reduced-motion: reduce) {
  .upload-area {
    transition: none;
  }
}
</style>
