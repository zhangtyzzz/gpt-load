<script setup lang="ts">
import { Add, Remove } from "@vicons/ionicons5";
import { NButton, NIcon, NInput, NInputNumber, NSelect, NSpace } from "naive-ui";
import { computed, nextTick, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

type RequestAction = "return" | "retry_other_key";
type HealthAction = "noop" | "fail_count_inc" | "cooldown" | "blacklist_now";

interface ErrorPolicyDecision {
  on_request: RequestAction;
  health: HealthAction;
  params?: {
    cooldown_seconds?: number;
  };
}

interface ErrorPolicyRule extends ErrorPolicyDecision {
  match: {
    status: number[];
  };
}

interface ErrorPolicy {
  rules?: ErrorPolicyRule[];
  default?: ErrorPolicyDecision;
}

interface EditorRule {
  statuses: string;
  on_request: RequestAction;
  health: HealthAction;
  cooldown_seconds: number | null;
}

const props = withDefaults(
  defineProps<{
    modelValue: string;
    showDefault?: boolean;
  }>(),
  {
    showDefault: false,
  }
);

const emit = defineEmits<{
  (e: "update:modelValue", value: string): void;
}>();

const { t } = useI18n();
const syncingFromProp = ref(false);
const rules = ref<EditorRule[]>([]);
const hiddenDefaultDecision = ref<ErrorPolicyDecision | null>(null);
const defaultDecision = reactive<EditorRule>({
  statuses: "",
  on_request: "retry_other_key",
  health: "fail_count_inc",
  cooldown_seconds: null,
});

const requestActionOptions = computed(() => [
  { label: t("errorPolicy.request.return"), value: "return" },
  { label: t("errorPolicy.request.retryOtherKey"), value: "retry_other_key" },
]);

const healthActionOptions = computed(() => [
  { label: t("errorPolicy.health.noop"), value: "noop" },
  { label: t("errorPolicy.health.failCountInc"), value: "fail_count_inc" },
  { label: t("errorPolicy.health.cooldown"), value: "cooldown" },
  { label: t("errorPolicy.health.blacklistNow"), value: "blacklist_now" },
]);

watch(
  () => props.modelValue,
  value => {
    parsePolicy(value);
  },
  { immediate: true }
);

watch(
  [rules, defaultDecision],
  () => {
    if (syncingFromProp.value) {
      return;
    }
    emit("update:modelValue", JSON.stringify(buildPolicy(), null, 2));
  },
  { deep: true }
);

function parsePolicy(value: string) {
  syncingFromProp.value = true;
  try {
    const policy = value?.trim() ? (JSON.parse(value) as ErrorPolicy) : {};
    rules.value = (policy.rules || []).map(rule => ({
      statuses: formatStatusList(rule.match?.status || []),
      on_request: rule.on_request || "retry_other_key",
      health: rule.health || "fail_count_inc",
      cooldown_seconds: rule.params?.cooldown_seconds ?? null,
    }));

    if (policy.default) {
      defaultDecision.on_request = policy.default.on_request || "retry_other_key";
      defaultDecision.health = policy.default.health || "fail_count_inc";
      defaultDecision.cooldown_seconds = policy.default.params?.cooldown_seconds ?? null;
      hiddenDefaultDecision.value = {
        on_request: defaultDecision.on_request,
        health: defaultDecision.health,
        ...(defaultDecision.health === "cooldown" && defaultDecision.cooldown_seconds
          ? { params: { cooldown_seconds: defaultDecision.cooldown_seconds } }
          : {}),
      };
    } else {
      defaultDecision.on_request = "retry_other_key";
      defaultDecision.health = "fail_count_inc";
      defaultDecision.cooldown_seconds = null;
      hiddenDefaultDecision.value = null;
    }
  } catch {
    rules.value = [];
    hiddenDefaultDecision.value = null;
  } finally {
    nextTick(() => {
      syncingFromProp.value = false;
    });
  }
}

function buildPolicy(): ErrorPolicy {
  const policy: ErrorPolicy = {
    rules: rules.value
      .map(rule => ({
        match: { status: parseStatusList(rule.statuses) },
        ...buildDecision(rule),
      }))
      .filter(rule => rule.match.status.length > 0),
  };

  if (props.showDefault) {
    policy.default = buildDecision(defaultDecision);
  } else if (hiddenDefaultDecision.value) {
    policy.default = hiddenDefaultDecision.value;
  }

  return policy;
}

function buildDecision(rule: EditorRule): ErrorPolicyDecision {
  const decision: ErrorPolicyDecision = {
    on_request: rule.on_request,
    health: rule.health,
  };

  if (rule.health === "cooldown" && rule.cooldown_seconds && rule.cooldown_seconds > 0) {
    decision.params = {
      cooldown_seconds: rule.cooldown_seconds,
    };
  }

  return decision;
}

function parseStatusList(value: string): number[] {
  const seen = new Set<number>();
  value
    .replace(/(\d)\s*[-~～]\s*(\d)/g, "$1-$2")
    .split(/[\s,，]+/)
    .map(item => item.trim())
    .filter(Boolean)
    .forEach(item => {
      const rangeMatch = item.match(/^(\d{3})-(\d{3})$/);
      if (rangeMatch) {
        const start = Number(rangeMatch[1]);
        const end = Number(rangeMatch[2]);
        const lower = Math.min(start, end);
        const upper = Math.max(start, end);
        for (let status = lower; status <= upper; status += 1) {
          if (status >= 100 && status <= 999) {
            seen.add(status);
          }
        }
        return;
      }

      const status = Number(item);
      if (Number.isInteger(status) && status >= 100 && status <= 999) {
        seen.add(status);
      }
    });
  return Array.from(seen).sort((a, b) => a - b);
}

function formatStatusList(statuses: number[]): string {
  const sorted = Array.from(new Set(statuses))
    .filter(status => Number.isInteger(status) && status >= 100 && status <= 999)
    .sort((a, b) => a - b);
  const parts: string[] = [];

  for (let index = 0; index < sorted.length; index += 1) {
    const start = sorted[index];
    let end = start;
    while (index + 1 < sorted.length && sorted[index + 1] === end + 1) {
      index += 1;
      end = sorted[index];
    }
    parts.push(start === end ? `${start}` : `${start}-${end}`);
  }

  return parts.join(", ");
}

function addRule() {
  rules.value.push({
    statuses: "",
    on_request: "retry_other_key",
    health: "fail_count_inc",
    cooldown_seconds: null,
  });
}

function removeRule(index: number) {
  rules.value.splice(index, 1);
}
</script>

<template>
  <div class="error-policy-editor">
    <div v-if="showDefault" class="policy-default-row">
      <span class="policy-label">{{ t("errorPolicy.defaultRule") }}</span>
      <n-select
        v-model:value="defaultDecision.on_request"
        :options="requestActionOptions"
        size="small"
      />
      <n-select
        v-model:value="defaultDecision.health"
        :options="healthActionOptions"
        size="small"
      />
      <n-input-number
        v-if="defaultDecision.health === 'cooldown'"
        v-model:value="defaultDecision.cooldown_seconds"
        :min="1"
        :precision="0"
        size="small"
        :placeholder="t('errorPolicy.cooldownSeconds')"
      />
    </div>

    <n-space vertical :size="8">
      <div v-for="(rule, index) in rules" :key="index" class="policy-rule-row">
        <n-input
          v-model:value="rule.statuses"
          size="small"
          :placeholder="t('errorPolicy.statusPlaceholder')"
        />
        <n-select v-model:value="rule.on_request" :options="requestActionOptions" size="small" />
        <n-select v-model:value="rule.health" :options="healthActionOptions" size="small" />
        <n-input-number
          v-if="rule.health === 'cooldown'"
          v-model:value="rule.cooldown_seconds"
          :min="1"
          :precision="0"
          size="small"
          :placeholder="t('errorPolicy.cooldownSeconds')"
        />
        <div v-else class="policy-cooldown-placeholder" />
        <n-button type="error" quaternary circle size="small" @click="removeRule(index)">
          <template #icon>
            <n-icon :component="Remove" />
          </template>
        </n-button>
      </div>
    </n-space>

    <n-button dashed size="small" class="policy-add-button" @click="addRule">
      <template #icon>
        <n-icon :component="Add" />
      </template>
      {{ t("errorPolicy.addRule") }}
    </n-button>
  </div>
</template>

<style scoped>
.error-policy-editor {
  width: 100%;
}

.policy-default-row,
.policy-rule-row {
  display: grid;
  grid-template-columns:
    minmax(120px, 1.2fr) minmax(128px, 1fr) minmax(128px, 1fr) minmax(112px, 0.8fr)
    32px;
  gap: 8px;
  align-items: center;
}

.policy-default-row {
  grid-template-columns: minmax(120px, 1.2fr) minmax(128px, 1fr) minmax(128px, 1fr) minmax(
      112px,
      0.8fr
    );
  margin-bottom: 8px;
}

.policy-default-row > *,
.policy-rule-row > * {
  min-width: 0;
}

.policy-label {
  color: var(--text-secondary);
  font-size: 13px;
}

.policy-cooldown-placeholder {
  min-height: 1px;
}

.policy-add-button {
  margin-top: 8px;
  width: 100%;
}

@media (max-width: 768px) {
  .policy-default-row,
  .policy-rule-row {
    grid-template-columns: 1fr;
  }
}
</style>
