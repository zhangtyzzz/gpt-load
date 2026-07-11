<script setup lang="ts">
import { settingsApi, type Setting, type SettingCategory } from "@/api/settings";
import ErrorPolicyEditor from "@/components/common/ErrorPolicyEditor.vue";
import ProxyKeysInput from "@/components/common/ProxyKeysInput.vue";
import { AlertCircleOutline, CheckmarkCircleOutline, HelpCircle, Save } from "@vicons/ionicons5";
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NGrid,
  NGridItem,
  NIcon,
  NInput,
  NInputNumber,
  NSkeleton,
  NSpace,
  NSwitch,
  NTooltip,
  useDialog,
  useMessage,
  type FormItemRule,
} from "naive-ui";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
const message = useMessage();
const dialog = useDialog();

const settingList = ref<SettingCategory[]>([]);
const formRef = ref();
const form = ref<Record<string, string | number | boolean>>({});
const savedSnapshot = ref("");
const isSaving = ref(false);
const isLoading = ref(true);
const loadError = ref(false);

const serialize = (value: Record<string, string | number | boolean>) => JSON.stringify(value);
const isDirty = computed(
  () => Boolean(savedSnapshot.value) && serialize(form.value) !== savedSnapshot.value
);

void fetchSettings();

async function fetchSettings(): Promise<boolean> {
  try {
    isLoading.value = true;
    loadError.value = false;
    const data = await settingsApi.getSettings();
    settingList.value = data || [];
    initForm();
    return true;
  } catch (_error) {
    loadError.value = true;
    message.error(t("settings.loadFailed"));
    return false;
  } finally {
    isLoading.value = false;
  }
}

function initForm() {
  form.value = settingList.value.reduce(
    (acc: Record<string, string | number | boolean>, category) => {
      category.settings?.forEach(setting => {
        acc[setting.key] = setting.value;
      });
      return acc;
    },
    {}
  );
  savedSnapshot.value = serialize(form.value);
}

async function handleSubmit() {
  if (isSaving.value || !isDirty.value) {
    return;
  }

  try {
    await formRef.value.validate();
    isSaving.value = true;
    await settingsApi.updateSettings(form.value);
    const reloaded = await fetchSettings();
    if (reloaded) {
      message.success(t("settings.settingsSaved"));
    }
  } finally {
    isSaving.value = false;
  }
}

function restoreSavedValues() {
  if (savedSnapshot.value) {
    form.value = JSON.parse(savedSnapshot.value) as Record<string, string | number | boolean>;
  }
}

function generateValidationRules(item: Setting): FormItemRule[] {
  const rules: FormItemRule[] = [];
  if (item.required) {
    const rule: FormItemRule = {
      required: true,
      message: t("settings.pleaseInput", { field: item.name }),
      trigger: ["input", "blur"],
    };
    if (item.type === "int") {
      rule.type = "number";
    }
    rules.push(rule);
  }
  if (item.type === "int" && item.min_value !== undefined && item.min_value !== null) {
    rules.push({
      validator: (_rule: FormItemRule, value: number) => {
        if (value === null || value === undefined) {
          return true;
        }
        if (item.min_value !== undefined && item.min_value !== null && value < item.min_value) {
          return new Error(t("settings.minValueError", { value: item.min_value }));
        }
        return true;
      },
      trigger: ["input", "blur"],
    });
  }
  return rules;
}

function confirmDiscard(): Promise<boolean> {
  if (!isDirty.value) {
    return Promise.resolve(true);
  }

  return new Promise(resolve => {
    dialog.warning({
      title: t("settings.discardChangesTitle"),
      content: t("settings.discardChangesDescription"),
      positiveText: t("settings.discardAndLeave"),
      negativeText: t("settings.continueEditing"),
      positiveButtonProps: { type: "warning" },
      onPositiveClick: () => resolve(true),
      onNegativeClick: () => resolve(false),
      onClose: () => resolve(false),
      onMaskClick: () => resolve(false),
    });
  });
}

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!isDirty.value) {
    return;
  }
  event.preventDefault();
  event.returnValue = t("settings.leaveWarning");
}

onBeforeRouteLeave(confirmDiscard);
onMounted(() => globalThis.addEventListener("beforeunload", handleBeforeUnload));
onBeforeUnmount(() => globalThis.removeEventListener("beforeunload", handleBeforeUnload));
</script>

<template>
  <section class="page-shell settings-page" aria-labelledby="settings-title">
    <header class="page-heading">
      <div>
        <h1 id="settings-title" class="page-title">{{ t("settings.title") }}</h1>
        <p class="page-description">{{ t("settings.description") }}</p>
      </div>
      <div
        class="save-state"
        :class="{
          'save-state--dirty': isDirty && !loadError,
          'save-state--error': loadError,
        }"
        role="status"
        aria-live="polite"
      >
        <n-icon :component="loadError ? AlertCircleOutline : CheckmarkCircleOutline" />
        <span v-if="isLoading">{{ t("common.loading") }}</span>
        <span v-else-if="loadError">{{ t("settings.loadFailed") }}</span>
        <span v-else>
          {{ isDirty ? t("settings.unsavedChanges") : t("settings.changesSaved") }}
        </span>
      </div>
    </header>

    <n-alert v-if="loadError" type="error" :title="t('settings.loadFailed')" class="load-alert">
      <div class="load-alert__content">
        <span>{{ t("settings.loadFailed") }}</span>
        <n-button size="small" :loading="isLoading" @click="fetchSettings">
          {{ t("common.retry") }}
        </n-button>
      </div>
    </n-alert>

    <div v-if="isLoading" class="settings-skeleton surface-card">
      <n-skeleton text :repeat="8" />
    </div>

    <n-form
      v-if="!isLoading && settingList.length > 0"
      ref="formRef"
      :model="form"
      label-placement="top"
      class="settings-form"
    >
      <n-space vertical :size="16">
        <n-card
          v-for="category in settingList"
          :key="category.category_name"
          size="small"
          class="settings-section"
          :bordered="false"
        >
          <template #header>
            <h2 class="section-title">{{ category.category_name }}</h2>
          </template>

          <n-grid :x-gap="24" :y-gap="4" responsive="screen" cols="1 s:2 m:2 l:4 xl:4">
            <n-grid-item
              v-for="item in category.settings"
              :key="item.key"
              :span="item.key === 'proxy_keys' || item.key === 'error_policy' ? 4 : 1"
            >
              <n-form-item :path="item.key" :rule="generateValidationRules(item)">
                <template #label>
                  <span class="field-label">
                    <span>{{ item.name }}</span>
                    <n-tooltip trigger="hover" placement="top">
                      <template #trigger>
                        <button
                          class="help-button"
                          type="button"
                          :aria-label="`${item.name}: ${item.description}`"
                        >
                          <n-icon :component="HelpCircle" :size="15" />
                        </button>
                      </template>
                      {{ item.description }}
                    </n-tooltip>
                  </span>
                </template>

                <n-input-number
                  v-if="item.type === 'int'"
                  v-model:value="form[item.key] as number"
                  :min="
                    item.min_value !== undefined && item.min_value >= 0 ? item.min_value : undefined
                  "
                  :placeholder="t('settings.inputNumber')"
                  clearable
                  style="width: 100%"
                />
                <n-switch
                  v-else-if="item.type === 'bool'"
                  v-model:value="form[item.key] as boolean"
                />
                <proxy-keys-input
                  v-else-if="item.key === 'proxy_keys'"
                  v-model="form[item.key] as string"
                  :placeholder="t('settings.inputContent')"
                />
                <error-policy-editor
                  v-else-if="item.key === 'error_policy'"
                  :model-value="String(form[item.key] || '')"
                  :show-default="true"
                  @update:model-value="value => (form[item.key] = value)"
                />
                <n-input
                  v-else
                  v-model:value="form[item.key] as string"
                  :placeholder="t('settings.inputContent')"
                  clearable
                />
              </n-form-item>
            </n-grid-item>
          </n-grid>
        </n-card>
      </n-space>
    </n-form>

    <div v-if="!isLoading && settingList.length > 0" class="save-bar material-chrome">
      <div class="save-summary">
        <strong>{{ isDirty ? t("settings.unsavedChanges") : t("settings.changesSaved") }}</strong>
        <span>{{ t("settings.description") }}</span>
      </div>
      <div class="save-actions">
        <n-button :disabled="!isDirty || isSaving || loadError" @click="restoreSavedValues">
          {{ t("common.reset") }}
        </n-button>
        <n-button
          type="primary"
          :loading="isSaving"
          :disabled="!isDirty || isSaving || loadError"
          @click="handleSubmit"
        >
          <template #icon><n-icon :component="Save" /></template>
          {{ isSaving ? t("settings.saving") : t("settings.saveSettings") }}
        </n-button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.settings-page {
  padding-bottom: 5.75rem;
}

.save-state {
  display: inline-flex;
  min-height: 32px;
  padding: 0.35rem 0.7rem;
  align-items: center;
  gap: 0.4rem;
  border-radius: 999px;
  background: var(--success-bg);
  color: var(--success-color);
  font-size: 0.78rem;
  font-weight: 650;
}

.save-state--dirty {
  background: rgba(255, 159, 10, 0.1);
  color: var(--warning-color);
}

.save-state--error {
  background: var(--error-bg);
  color: var(--error-color);
}

.load-alert {
  margin-bottom: 1rem;
}

.load-alert__content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.settings-skeleton {
  padding: 2rem;
}

.settings-section {
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-lg);
  background: var(--card-bg-solid);
  box-shadow: var(--shadow-sm);
}

.section-title {
  font-size: 1.05rem;
  font-weight: 650;
  letter-spacing: -0.012em;
}

.field-label {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  color: var(--text-primary);
  font-weight: 590;
}

.help-button {
  display: inline-grid;
  width: 1.5rem;
  height: 1.5rem;
  padding: 0;
  place-items: center;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--text-tertiary);
  cursor: help;
}

.help-button:hover {
  background: var(--hover-bg);
  color: var(--primary-color);
}

.save-bar {
  position: fixed;
  right: max(1rem, calc((100vw - 1440px) / 2 + 1.5rem));
  bottom: 1rem;
  left: max(1rem, calc((100vw - 1440px) / 2 + 1.5rem));
  z-index: 90;
  display: flex;
  min-height: 66px;
  padding: 0.75rem 0.875rem 0.75rem 1rem;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  border-radius: 16px;
  box-shadow: var(--shadow-lg);
}

.save-summary {
  display: grid;
  min-width: 0;
}

.save-summary strong {
  color: var(--text-primary);
  font-size: 0.86rem;
}

.save-summary span {
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 0.76rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.save-actions {
  display: flex;
  flex-shrink: 0;
  gap: 0.5rem;
}

@media (max-width: 640px) {
  .settings-page {
    padding-bottom: 7rem;
  }

  .save-bar {
    align-items: stretch;
    flex-direction: column;
  }

  .save-actions :deep(.n-button) {
    flex: 1;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .save-bar {
    background: var(--card-bg-solid);
    backdrop-filter: none;
  }
}
</style>
