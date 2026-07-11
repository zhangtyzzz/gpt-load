<script setup lang="ts">
import { normalizeGenericHttpConfig } from "@/composables/useChannelCatalog";
import type {
  ChannelPresetDescriptor,
  GenericHttpAuthConfig,
  GenericHttpChannelConfig,
  GenericHttpRetryConfig,
  GenericHttpStreamMode,
  GenericHttpValidationConfig,
  GenericHttpValidationMethod,
} from "@/types/channels";
import type { UpstreamInfo } from "@/types/models";
import {
  isHttpHeaderToken,
  isValidHttpUpstreamUrl,
  RESERVED_PROXY_HEADER_NAMES,
} from "@/utils/channel-form";
import { copy } from "@/utils/clipboard";
import {
  CheckmarkCircleOutline,
  ClipboardOutline,
  CodeSlashOutline,
  ServerOutline,
} from "@vicons/ionicons5";
import {
  NAlert,
  NButton,
  NCollapse,
  NCollapseItem,
  NFormItem,
  NIcon,
  NInput,
  NInputNumber,
  NSelect,
  NSwitch,
  NTag,
  type SelectGroupOption,
  type SelectOption,
  type SelectRenderLabel,
  type SelectRenderOption,
  useDialog,
  useMessage,
} from "naive-ui";
import { computed, h, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

interface Props {
  modelValue?: GenericHttpChannelConfig | Record<string, unknown>;
  presets?: ChannelPresetDescriptor[];
  upstreams?: UpstreamInfo[];
  groupName?: string;
  catalogLoading?: boolean;
}

interface Emits {
  (e: "update:modelValue", value: GenericHttpChannelConfig): void;
  (e: "update:upstreams", value: UpstreamInfo[]): void;
  (e: "apply:preset", value: { config: GenericHttpChannelConfig; upstreams: UpstreamInfo[] }): void;
  (e: "validity", value: boolean): void;
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: undefined,
  presets: () => [],
  upstreams: () => [],
  groupName: "",
  catalogLoading: false,
});

const emit = defineEmits<Emits>();
const { t } = useI18n();
const dialog = useDialog();
const message = useMessage();

const bodyDraft = ref("");
const headersDraft = ref("{}");
const validStatusesDraft = ref("200");
const invalidStatusesDraft = ref("401");
const safeMethodsDraft = ref("GET, HEAD");
const failoverStatusesDraft = ref("");
let lastEmittedConfig = "";

const baseProtectedHeaders = new Set<string>(RESERVED_PROXY_HEADER_NAMES);

const config = computed(() => normalizeGenericHttpConfig(props.modelValue));
const selectedPreset = computed(() =>
  props.presets.find(preset => preset.id === config.value.preset_id)
);
const isCustom = computed(() => config.value.preset_id === "custom" || !selectedPreset.value);
const hostedMcpPreset = computed(() => selectedPreset.value?.integration_kind === "hosted_mcp");
const selectedPresetValue = computed(() => (isCustom.value ? "custom" : config.value.preset_id));

type PresetCategory = "http" | "hosted-mcp" | "custom";

interface PresetSelectOption extends SelectOption {
  value: string;
  label: string;
  description: string;
  category: PresetCategory;
  categoryLabel: string;
  searchText: string;
}

const validationMethodOptions = ["GET", "HEAD", "POST"].map(value => ({ label: value, value }));
const streamModeOptions = computed(() => [
  { label: t("channels.stream.never"), value: "never" },
  { label: t("channels.stream.auto"), value: "auto" },
  { label: t("channels.stream.always"), value: "always" },
]);

function presetTranslation(id: string, part: "name" | "description", fallback: string): string {
  const key = `channels.presets.${id}.${part}`;
  const translated = t(key);
  return translated === key ? fallback : translated;
}

function categoryLabel(category: PresetCategory): string {
  if (category === "hosted-mcp") {
    return t("channels.presetGroups.hostedMcp");
  }
  if (category === "custom") {
    return t("channels.presetGroups.custom");
  }
  return t("channels.presetGroups.httpApi");
}

function presetOption(preset: ChannelPresetDescriptor): PresetSelectOption {
  const category: PresetCategory = preset.integration_kind === "hosted_mcp" ? "hosted-mcp" : "http";
  const label = presetTranslation(preset.id, "name", preset.display_name);
  const description = presetTranslation(preset.id, "description", preset.description);
  const groupLabel = categoryLabel(category);
  return {
    value: preset.id,
    label,
    description,
    category,
    categoryLabel: groupLabel,
    searchText: `${label} ${description} ${groupLabel} ${preset.id}`.toLocaleLowerCase(),
  };
}

const presetSelectOptions = computed<SelectGroupOption[]>(() => {
  const httpOptions = props.presets
    .filter(preset => preset.integration_kind !== "hosted_mcp")
    .map(presetOption);
  const hostedMcpOptions = props.presets
    .filter(preset => preset.integration_kind === "hosted_mcp")
    .map(presetOption);
  const customLabel = t("channels.presets.custom.name");
  const customDescription = t("channels.presets.custom.description");
  const customGroupLabel = categoryLabel("custom");
  const groups: SelectGroupOption[] = [];

  if (httpOptions.length > 0) {
    groups.push({
      type: "group",
      key: "http",
      label: categoryLabel("http"),
      children: httpOptions,
    });
  }
  if (hostedMcpOptions.length > 0) {
    groups.push({
      type: "group",
      key: "hosted-mcp",
      label: categoryLabel("hosted-mcp"),
      children: hostedMcpOptions,
    });
  }
  groups.push({
    type: "group",
    key: "custom",
    label: customGroupLabel,
    children: [
      {
        value: "custom",
        label: customLabel,
        description: customDescription,
        category: "custom",
        categoryLabel: customGroupLabel,
        searchText: `${customLabel} ${customDescription} ${customGroupLabel}`.toLocaleLowerCase(),
      } satisfies PresetSelectOption,
    ],
  });
  return groups;
});

const renderPresetLabel: SelectRenderLabel = option =>
  h("span", { class: "preset-selected-label" }, String(option.label || option.value || ""));

const renderPresetOption: SelectRenderOption = ({ node, option, selected }) => {
  if (option.type === "group") {
    return node;
  }
  const item = option as PresetSelectOption;
  return h(
    "div",
    {
      ...(node.props || {}),
      class: [node.props?.class, "preset-option-shell"],
    },
    [
      h("div", { class: "preset-option-copy" }, [
        h("span", { class: "preset-option-title" }, item.label),
        h("span", { class: "preset-option-description" }, item.description),
      ]),
      h(
        NTag,
        {
          round: true,
          size: "small",
          bordered: false,
          type:
            item.category === "hosted-mcp"
              ? "success"
              : item.category === "http"
                ? "info"
                : "default",
        },
        { default: () => item.categoryLabel }
      ),
      selected
        ? h(NIcon, { component: CheckmarkCircleOutline, class: "preset-option-check" })
        : null,
    ]
  );
};

function filterPresetOption(pattern: string, option: SelectOption): boolean {
  const searchText = String((option as PresetSelectOption).searchText || "");
  return searchText.includes(pattern.trim().toLocaleLowerCase());
}

function formatJson(value: unknown): string {
  if (value === null || value === undefined) {
    return "";
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return "";
  }
}

function syncDrafts(nextConfig: GenericHttpChannelConfig) {
  bodyDraft.value = formatJson(nextConfig.validation.body);
  headersDraft.value = formatJson(nextConfig.validation.headers) || "{}";
  validStatusesDraft.value = nextConfig.validation.valid_statuses.join(", ");
  invalidStatusesDraft.value = nextConfig.validation.invalid_statuses.join(", ");
  safeMethodsDraft.value = nextConfig.retry.safe_methods.join(", ");
  failoverStatusesDraft.value = nextConfig.retry.failover_statuses.join(", ");
}

watch(
  () => props.modelValue,
  value => {
    const normalized = normalizeGenericHttpConfig(value);
    if (JSON.stringify(normalized) !== lastEmittedConfig) {
      syncDrafts(normalized);
    }
  },
  { immediate: true, deep: true }
);

function emitConfig(nextConfig: GenericHttpChannelConfig) {
  const normalized = normalizeGenericHttpConfig(nextConfig);
  lastEmittedConfig = JSON.stringify(normalized);
  emit("update:modelValue", normalized);
}

function updateConfig(patch: Partial<GenericHttpChannelConfig>) {
  emitConfig({ ...config.value, ...patch, version: 1, preset_id: "custom" });
}

function updateAuth(patch: Partial<GenericHttpAuthConfig>) {
  updateConfig({ auth: { ...config.value.auth, ...patch } });
}

function updateValidation(patch: Partial<GenericHttpValidationConfig>) {
  updateConfig({ validation: { ...config.value.validation, ...patch } });
}

function toggleValidation(enabled: boolean) {
  if (!enabled) {
    updateValidation({ enabled: false });
    return;
  }
  updateValidation({
    enabled: true,
    method: "GET",
    valid_statuses: [200],
    invalid_statuses: [401],
  });
}

function updateRetry(patch: Partial<GenericHttpRetryConfig>) {
  updateConfig({ retry: { ...config.value.retry, ...patch } });
}

function replaceWithPreset(preset: ChannelPresetDescriptor) {
  const nextConfig = normalizeGenericHttpConfig(preset.channel_config, preset.id);
  nextConfig.preset_id = preset.id;
  const nextUpstreams = preset.upstreams.map(upstream => ({ ...upstream }));
  lastEmittedConfig = JSON.stringify(nextConfig);
  syncDrafts(nextConfig);
  emit("apply:preset", { config: nextConfig, upstreams: nextUpstreams });
}

function sameUpstreams(left: UpstreamInfo[], right: UpstreamInfo[]): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function applyPreset(preset: ChannelPresetDescriptor | null) {
  if (!preset) {
    updateConfig({ preset_id: "custom" });
    return;
  }

  const nextUpstreams = preset.upstreams.map(upstream => ({ ...upstream }));
  if (props.upstreams.length > 1 && !sameUpstreams(props.upstreams, nextUpstreams)) {
    dialog.warning({
      title: t("channels.presetReplace.title"),
      content: t("channels.presetReplace.description", { count: props.upstreams.length }),
      positiveText: t("channels.presetReplace.confirm"),
      negativeText: t("common.cancel"),
      onPositiveClick: () => replaceWithPreset(preset),
    });
    return;
  }
  replaceWithPreset(preset);
}

function handlePresetSelection(value: string | number | null) {
  if (value === "custom" || value === null) {
    applyPreset(null);
    return;
  }
  const preset = props.presets.find(item => item.id === String(value));
  if (preset) {
    applyPreset(preset);
  }
}

function parseJsonObject(value: string): Record<string, string> | null {
  try {
    const parsed: unknown = value.trim() ? JSON.parse(value) : {};
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return null;
    }
    const entries = Object.entries(parsed as Record<string, unknown>);
    if (entries.some(([, item]) => typeof item !== "string")) {
      return null;
    }
    return Object.fromEntries(entries) as Record<string, string>;
  } catch {
    return null;
  }
}

function parseJsonValue(value: string): { valid: boolean; value: unknown | null } {
  if (!value.trim()) {
    return { valid: true, value: null };
  }
  try {
    return { valid: true, value: JSON.parse(value) };
  } catch {
    return { valid: false, value: null };
  }
}

function parseStatuses(value: string, allowEmpty = false, minimum = 100): number[] | null {
  const tokens = value
    .split(",")
    .map(item => item.trim())
    .filter(Boolean);
  if (tokens.length === 0) {
    return allowEmpty ? [] : null;
  }
  const statuses = tokens.map(Number);
  if (statuses.some(status => !Number.isInteger(status) || status < minimum || status > 599)) {
    return null;
  }
  return [...new Set(statuses)];
}

function parseMethods(value: string, allowEmpty = false): string[] | null {
  const methods = value
    .split(",")
    .map(item => item.trim().toUpperCase())
    .filter(Boolean);
  if (methods.length === 0) {
    return allowEmpty ? [] : null;
  }
  if (methods.some(method => !/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(method))) {
    return null;
  }
  return [...new Set(methods)];
}

function handleHeadersInput(value: string) {
  headersDraft.value = value;
  const parsed = parseJsonObject(value);
  if (parsed) {
    updateValidation({ headers: parsed });
  }
}

function handleBodyInput(value: string) {
  bodyDraft.value = value;
  const parsed = parseJsonValue(value);
  if (parsed.valid) {
    updateValidation({ body: parsed.value });
  }
}

function handleValidationStatuses(kind: "valid" | "invalid", value: string) {
  if (kind === "valid") {
    validStatusesDraft.value = value;
  } else {
    invalidStatusesDraft.value = value;
  }
  const parsed = parseStatuses(value);
  if (parsed) {
    updateValidation(kind === "valid" ? { valid_statuses: parsed } : { invalid_statuses: parsed });
  }
}

function handleRetryMethods(value: string) {
  safeMethodsDraft.value = value;
  const parsed = parseMethods(value);
  if (parsed) {
    updateRetry({ safe_methods: parsed });
  }
}

function handleFailoverStatuses(value: string) {
  failoverStatusesDraft.value = value;
  const parsed = parseStatuses(value, true, 300);
  if (parsed) {
    updateRetry({ failover_statuses: parsed });
  }
}

function isRelativePath(value: string): boolean {
  return value.startsWith("/") && !value.startsWith("//") && !value.includes("://");
}

const managedHeaders = computed(() => {
  const headers = new Set(baseProtectedHeaders);
  const authName = config.value.auth.name.trim().toLowerCase();
  if (authName) {
    headers.add(authName);
  }
  return headers;
});

const headersError = computed(() => {
  const headers = parseJsonObject(headersDraft.value);
  if (!headers) {
    return t("channels.validation.headersInvalid");
  }
  for (const [name, value] of Object.entries(headers)) {
    if (
      !isHttpHeaderToken(name) ||
      managedHeaders.value.has(name.toLowerCase()) ||
      /[\r\n]/.test(value)
    ) {
      return t("channels.validation.headersProtected");
    }
  }
  return "";
});
const bodyError = computed(() => {
  const parsed = parseJsonValue(bodyDraft.value);
  if (!parsed.valid) {
    return t("channels.validation.bodyInvalid");
  }
  return new TextEncoder().encode(bodyDraft.value).length > 65536
    ? t("channels.validation.bodyTooLarge")
    : "";
});
const validStatusesError = computed(() =>
  parseStatuses(validStatusesDraft.value) ? "" : t("channels.validation.statusesInvalid")
);
const invalidStatusesError = computed(() =>
  parseStatuses(invalidStatusesDraft.value) ? "" : t("channels.validation.statusesInvalid")
);
const statusOverlap = computed(() => {
  const valid = parseStatuses(validStatusesDraft.value) || [];
  const invalid = new Set(parseStatuses(invalidStatusesDraft.value) || []);
  return valid.some(status => invalid.has(status));
});
const upstreamError = computed(() => {
  if (props.upstreams.length === 0) {
    return t("channels.validation.upstreamInvalid");
  }
  return props.upstreams.every(upstream => isValidHttpUpstreamUrl(upstream.url))
    ? ""
    : t("channels.validation.allUpstreamsInvalid");
});
const validationBaseUrlError = computed(() => {
  const value = config.value.validation.base_url;
  return !value || isValidHttpUpstreamUrl(value) ? "" : t("channels.validation.baseUrlInvalid");
});
const validationPathError = computed(() => {
  if (!config.value.validation.enabled) {
    return "";
  }
  return isRelativePath(config.value.validation.path) ? "" : t("channels.validation.pathInvalid");
});
const authNameError = computed(() => {
  const name = config.value.auth.name.trim();
  const lower = name.toLowerCase();
  if (baseProtectedHeaders.has(lower)) {
    return t("channels.validation.authNameProtected");
  }
  return isHttpHeaderToken(name) ? "" : t("channels.validation.authNameInvalid");
});
const prefixError = computed(() =>
  /[\r\n]/.test(config.value.auth.prefix) || config.value.auth.prefix.length > 128
    ? t("channels.validation.prefixInvalid")
    : ""
);
const methodBodyError = computed(() => {
  const method = config.value.validation.method;
  const hasBody = parseJsonValue(bodyDraft.value).value !== null;
  return (method === "GET" || method === "HEAD") && hasBody
    ? t("channels.validation.methodBodyInvalid")
    : "";
});
const safeMethodsError = computed(() =>
  parseMethods(safeMethodsDraft.value) ? "" : t("channels.retry.methodsInvalid")
);
const failoverStatusesError = computed(() =>
  parseStatuses(failoverStatusesDraft.value, true, 300) ? "" : t("channels.retry.statusesInvalid")
);
const isValid = computed(
  () =>
    !upstreamError.value &&
    !authNameError.value &&
    !prefixError.value &&
    !safeMethodsError.value &&
    !failoverStatusesError.value &&
    (!config.value.validation.enabled ||
      (!headersError.value &&
        !bodyError.value &&
        !validStatusesError.value &&
        !invalidStatusesError.value &&
        !statusOverlap.value &&
        !validationBaseUrlError.value &&
        !validationPathError.value &&
        !methodBodyError.value)) &&
    config.value.max_request_body_bytes >= 1 &&
    config.value.max_request_body_bytes <= 64 * 1024 * 1024 &&
    config.value.max_error_body_bytes >= 1 &&
    config.value.max_error_body_bytes <= 1024 * 1024
);

watch(isValid, value => emit("validity", value), { immediate: true });

const proxyBaseUrl = computed(() => {
  const origin =
    typeof window === "undefined" ? "https://gpt-load.example" : window.location.origin;
  return `${origin}/proxy/${props.groupName || "your-group"}`;
});
const integrationPath = computed(() => {
  const path = selectedPreset.value?.suggested_path || "/mcp";
  return path.startsWith("/") ? path : `/${path}`;
});
const integrationEndpoint = computed(() => `${proxyBaseUrl.value}${integrationPath.value}`);
const integrationConfig = computed(() =>
  JSON.stringify(
    {
      mcpServers: {
        [props.groupName || "generic-http"]: {
          type: "streamable-http",
          url: integrationEndpoint.value,
          headers: { Authorization: "Bearer ${GPT_LOAD_PROXY_KEY}" },
        },
      },
    },
    null,
    2
  )
);
const currentPresetDescription = computed(() =>
  selectedPreset.value
    ? presetTranslation(selectedPreset.value.id, "description", selectedPreset.value.description)
    : t("channels.presets.custom.description")
);
const currentUpstreamSummary = computed(() =>
  props.upstreams.length === 1
    ? props.upstreams[0]?.url || t("channels.upstreamBaseUrlPlaceholder")
    : t("channels.upstreamCount", { count: props.upstreams.length })
);

async function copyValue(value: string) {
  if (await copy(value)) {
    message.success(t("channels.integration.copied"));
  } else {
    message.error(t("keys.copyFailedManual"));
  }
}
</script>

<template>
  <section class="generic-http-fields" aria-labelledby="generic-http-heading">
    <div class="section-heading">
      <div>
        <h4 id="generic-http-heading" class="section-title">
          {{ t("channels.transparentProxy") }}
        </h4>
        <p class="section-description">{{ t("channels.transparentProxyDescription") }}</p>
      </div>
      <n-tag size="small" round type="info">Generic HTTP</n-tag>
    </div>

    <n-form-item :label="t('channels.selectPreset')" class="preset-picker-field">
      <n-select
        class="preset-select"
        data-testid="preset-select"
        :value="selectedPresetValue"
        :options="presetSelectOptions"
        :placeholder="t('channels.selectPreset')"
        :aria-label="t('channels.selectPreset')"
        :disabled="catalogLoading"
        :loading="catalogLoading"
        :filter="filterPresetOption"
        :render-label="renderPresetLabel"
        :render-option="renderPresetOption"
        filterable
        clear-filter-after-select
        @update:value="handlePresetSelection"
      />
    </n-form-item>

    <n-alert v-if="!catalogLoading && presets.length === 0" type="warning" class="inline-alert">
      {{ t("channels.catalogFallback") }}
    </n-alert>

    <div class="preset-summary" aria-live="polite">
      <p class="preset-summary-description">{{ currentPresetDescription }}</p>
      <div class="connection-row">
        <span class="connection-icon" aria-hidden="true">
          <n-icon :component="ServerOutline" />
        </span>
        <div class="connection-content">
          <span class="connection-label">{{ t("channels.upstreamBaseUrl") }}</span>
          <code class="credential-preview">{{ currentUpstreamSummary }}</code>
          <span class="connection-hint">{{ t("channels.editUpstreamBelow") }}</span>
          <span v-if="upstreamError" class="field-error">{{ upstreamError }}</span>
        </div>
      </div>
    </div>

    <n-collapse class="advanced-collapse" accordion>
      <n-collapse-item name="transport" :title="t('channels.sections.transport')">
        <p class="collapse-description">{{ t("channels.sections.transportDescription") }}</p>
        <div class="advanced-grid">
          <n-form-item :label="t('channels.stream.label')">
            <n-select
              :value="config.stream_mode"
              :options="streamModeOptions"
              @update:value="updateConfig({ stream_mode: $event as GenericHttpStreamMode })"
            />
          </n-form-item>
          <n-form-item :label="t('channels.maxRequestBody')">
            <n-input-number
              :value="config.max_request_body_bytes"
              :min="1"
              :max="64 * 1024 * 1024"
              :step="1024"
              style="width: 100%"
              @update:value="
                updateConfig({ max_request_body_bytes: Math.max(1, Number($event || 1)) })
              "
            />
          </n-form-item>
          <n-form-item :label="t('channels.maxErrorBody')">
            <n-input-number
              :value="config.max_error_body_bytes"
              :min="1"
              :max="1024 * 1024"
              :step="1024"
              style="width: 100%"
              @update:value="
                updateConfig({ max_error_body_bytes: Math.max(1, Number($event || 1)) })
              "
            />
          </n-form-item>
        </div>
      </n-collapse-item>

      <n-collapse-item name="auth" :title="t('channels.sections.authentication')">
        <p class="collapse-description">{{ t("channels.sections.authenticationDescription") }}</p>
        <div class="advanced-grid">
          <n-form-item
            :label="t('channels.auth.headerName')"
            :feedback="authNameError"
            :validation-status="authNameError ? 'error' : undefined"
          >
            <n-input
              :value="config.auth.name"
              :status="authNameError ? 'error' : undefined"
              @update:value="updateAuth({ name: $event })"
            />
          </n-form-item>
          <n-form-item
            :label="t('channels.auth.prefix')"
            :feedback="prefixError"
            :validation-status="prefixError ? 'error' : undefined"
          >
            <n-input
              :value="config.auth.prefix"
              :status="prefixError ? 'error' : undefined"
              :placeholder="t('channels.auth.prefixPlaceholder')"
              @update:value="updateAuth({ prefix: $event })"
            />
          </n-form-item>
        </div>
      </n-collapse-item>

      <n-collapse-item name="validation" :title="t('channels.sections.validation')">
        <div class="subsection-heading">
          <p class="collapse-description">{{ t("channels.validation.description") }}</p>
          <n-switch
            :value="config.validation.enabled"
            :aria-label="t('channels.validation.enabled')"
            @update:value="toggleValidation"
          />
        </div>
        <template v-if="config.validation.enabled">
          <div class="advanced-grid">
            <n-form-item
              :label="t('channels.validation.baseUrl')"
              :feedback="validationBaseUrlError"
              :validation-status="validationBaseUrlError ? 'error' : undefined"
            >
              <n-input
                :value="config.validation.base_url"
                :status="validationBaseUrlError ? 'error' : undefined"
                :placeholder="t('channels.validation.baseUrlPlaceholder')"
                @update:value="updateValidation({ base_url: $event })"
              />
            </n-form-item>
            <n-form-item :label="t('channels.validation.method')">
              <n-select
                :value="config.validation.method"
                :options="validationMethodOptions"
                @update:value="updateValidation({ method: $event as GenericHttpValidationMethod })"
              />
            </n-form-item>
            <n-form-item
              :label="t('channels.validation.path')"
              :feedback="validationPathError"
              :validation-status="validationPathError ? 'error' : undefined"
            >
              <n-input
                :value="config.validation.path"
                :status="validationPathError ? 'error' : undefined"
                placeholder="/usage"
                @update:value="updateValidation({ path: $event })"
              />
            </n-form-item>
            <n-form-item
              :label="t('channels.validation.validStatuses')"
              :feedback="validStatusesError"
              :validation-status="validStatusesError ? 'error' : undefined"
            >
              <n-input
                :value="validStatusesDraft"
                :status="validStatusesError || statusOverlap ? 'error' : undefined"
                placeholder="200, 202"
                @update:value="handleValidationStatuses('valid', $event)"
              />
            </n-form-item>
            <n-form-item
              :label="t('channels.validation.invalidStatuses')"
              :feedback="invalidStatusesError"
              :validation-status="invalidStatusesError ? 'error' : undefined"
            >
              <n-input
                :value="invalidStatusesDraft"
                :status="invalidStatusesError || statusOverlap ? 'error' : undefined"
                placeholder="401, 403"
                @update:value="handleValidationStatuses('invalid', $event)"
              />
            </n-form-item>
          </div>
          <n-alert v-if="statusOverlap" type="error" class="inline-alert">
            {{ t("channels.validation.statusOverlap") }}
          </n-alert>
          <div class="json-editors">
            <n-form-item
              :label="t('channels.validation.headers')"
              :feedback="headersError"
              :validation-status="headersError ? 'error' : undefined"
            >
              <n-input
                :value="headersDraft"
                type="textarea"
                :rows="3"
                :status="headersError ? 'error' : undefined"
                placeholder='{"Accept":"application/json"}'
                @update:value="handleHeadersInput"
              />
            </n-form-item>
            <n-form-item
              :label="t('channels.validation.body')"
              :feedback="bodyError || methodBodyError"
              :validation-status="bodyError || methodBodyError ? 'error' : undefined"
            >
              <n-input
                :value="bodyDraft"
                type="textarea"
                :rows="5"
                :status="bodyError || methodBodyError ? 'error' : undefined"
                :placeholder="t('channels.validation.bodyPlaceholder')"
                @update:value="handleBodyInput"
              />
            </n-form-item>
          </div>
        </template>
      </n-collapse-item>

      <n-collapse-item name="retry" :title="t('channels.sections.failover')">
        <p class="collapse-description">{{ t("channels.retry.description") }}</p>
        <n-alert type="warning" class="inline-alert">{{ t("channels.retry.postWarning") }}</n-alert>
        <div class="advanced-grid spaced-grid">
          <n-form-item
            :label="t('channels.retry.safeMethods')"
            :feedback="safeMethodsError"
            :validation-status="safeMethodsError ? 'error' : undefined"
          >
            <n-input
              :value="safeMethodsDraft"
              :status="safeMethodsError ? 'error' : undefined"
              placeholder="GET, HEAD"
              @update:value="handleRetryMethods"
            />
          </n-form-item>
          <n-form-item
            :label="t('channels.retry.failoverStatuses')"
            :feedback="failoverStatusesError"
            :validation-status="failoverStatusesError ? 'error' : undefined"
          >
            <n-input
              :value="failoverStatusesDraft"
              :status="failoverStatusesError ? 'error' : undefined"
              :placeholder="t('channels.retry.failoverStatusesPlaceholder')"
              @update:value="handleFailoverStatuses"
            />
          </n-form-item>
        </div>
      </n-collapse-item>
    </n-collapse>

    <div v-if="hostedMcpPreset" class="integration-card">
      <div class="integration-header">
        <div class="integration-title">
          <span class="integration-icon" aria-hidden="true">
            <n-icon :component="CodeSlashOutline" />
          </span>
          <div>
            <h5>{{ t("channels.integration.mcpTitle") }}</h5>
            <p>{{ t("channels.integration.mcpDescription") }}</p>
          </div>
        </div>
        <n-tag round size="small" type="success">{{ t("channels.integration.presetOnly") }}</n-tag>
      </div>
      <div class="endpoint-copy-row">
        <code>{{ integrationEndpoint }}</code>
        <n-button
          quaternary
          circle
          :aria-label="t('channels.integration.copyEndpoint')"
          @click="copyValue(integrationEndpoint)"
        >
          <template #icon><n-icon :component="ClipboardOutline" /></template>
        </n-button>
      </div>
      <pre class="integration-code"><code>{{ integrationConfig }}</code></pre>
      <div class="integration-actions">
        <n-button size="small" secondary @click="copyValue(integrationConfig)">
          <template #icon><n-icon :component="ClipboardOutline" /></template>
          {{ t("channels.integration.copyConfig") }}
        </n-button>
      </div>
      <n-alert type="warning" class="inline-alert integration-note">
        {{ t("channels.integration.stdioWarning") }}
      </n-alert>
    </div>
  </section>
</template>

<style scoped>
.generic-http-fields {
  display: grid;
  min-width: 0;
  gap: var(--space-4);
  margin-top: var(--space-5);
  padding-top: var(--space-5);
  border-top: 1px solid var(--border-color-light);
}

.section-heading,
.integration-header,
.subsection-heading,
.integration-title,
.connection-row,
.endpoint-copy-row,
.integration-actions,
.preset-summary .connection-row {
  display: flex;
  align-items: center;
}

.section-heading,
.integration-header,
.subsection-heading {
  justify-content: space-between;
  gap: var(--space-4);
}

.section-title,
.integration-header h5 {
  margin: 0;
  color: var(--text-primary);
  font-size: 1rem;
  font-weight: 650;
  letter-spacing: -0.012em;
}

.section-description,
.integration-header p,
.collapse-description {
  margin: var(--space-1) 0 0;
  color: var(--text-secondary);
  font-size: 0.8125rem;
  line-height: 1.5;
}

.collapse-description {
  margin: 0 0 var(--space-4);
}

.preset-picker-field {
  min-width: 0;
  margin-bottom: 0;
}

.preset-select {
  width: 100%;
}

.preset-select :deep(.preset-selected-label) {
  display: block;
  overflow: hidden;
  font-weight: 620;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.preset-option-shell) {
  display: grid;
  min-width: 0;
  min-height: 3.75rem;
  grid-template-columns: minmax(0, 1fr) auto 1.25rem;
  align-items: center;
  gap: 0.625rem;
}

:global(.preset-option-copy) {
  display: grid;
  min-width: 0;
  gap: 0.125rem;
  padding-block: 0.25rem;
}

:global(.preset-option-title) {
  overflow: hidden;
  color: var(--text-primary);
  font-size: 0.875rem;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.preset-option-description) {
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 0.75rem;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.preset-option-check) {
  color: var(--primary-color);
  font-size: 1rem;
}

.connection-icon,
.integration-icon {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 0.75rem;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-weight: 700;
}

.preset-summary,
.integration-card {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-md);
  background: var(--card-bg-solid);
}
.preset-summary-description {
  margin: 0;
  padding: var(--space-3);
  border-bottom: 1px solid var(--border-color-light);
  color: var(--text-secondary);
  font-size: 0.8125rem;
  line-height: 1.5;
}
.connection-row {
  gap: var(--space-3);
  padding: var(--space-3);
}
.connection-icon,
.integration-icon {
  width: 2rem;
  height: 2rem;
  color: var(--primary-color);
}
.connection-content {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: var(--space-1);
}
.connection-label {
  color: var(--text-secondary);
  font-size: 0.75rem;
  font-weight: 600;
}
.credential-preview,
.endpoint-copy-row code {
  min-width: 0;
  overflow: hidden;
  color: var(--text-primary);
  font-family: ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace;
  font-size: 0.78rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.field-error {
  color: var(--error-color);
  font-size: 0.75rem;
}
.connection-hint {
  color: var(--text-tertiary);
  font-size: 0.72rem;
}
.inline-alert {
  border-radius: var(--border-radius-md);
}
.advanced-collapse {
  min-width: 0;
  padding: 0 var(--space-1);
}
.advanced-grid,
.json-editors {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 var(--space-4);
}
.spaced-grid {
  margin-top: var(--space-4);
}
.subsection-heading {
  align-items: flex-start;
  margin-bottom: var(--space-4);
}
.subsection-heading .collapse-description {
  margin-bottom: 0;
}
.integration-card {
  padding: var(--space-4);
}
.integration-title {
  min-width: 0;
  gap: var(--space-3);
}
.integration-note {
  margin-top: var(--space-3);
}
.endpoint-copy-row {
  min-width: 0;
  justify-content: space-between;
  gap: var(--space-2);
  margin-top: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-sm);
  background: var(--code-bg);
}
.integration-code {
  max-width: 100%;
  margin: var(--space-3) 0 0;
  padding: var(--space-3);
  overflow-x: auto;
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-sm);
  background: var(--code-bg);
  color: var(--text-primary);
  font-family: ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace;
  font-size: 0.75rem;
  line-height: 1.55;
}
.integration-actions {
  justify-content: flex-end;
  margin-top: var(--space-2);
}

@media (max-width: 768px) {
  .advanced-grid,
  .json-editors {
    grid-template-columns: minmax(0, 1fr);
  }
  .section-heading,
  .integration-header,
  .subsection-heading {
    align-items: flex-start;
  }
  .section-heading,
  .integration-header {
    flex-direction: column;
  }
  .integration-card {
    padding: var(--space-3);
  }
  .integration-code {
    white-space: pre;
  }

  :global(.preset-option-shell) {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  :global(.preset-option-check) {
    display: none;
  }
}

@media (prefers-contrast: more) {
  .preset-summary,
  .integration-card,
  .endpoint-copy-row,
  .integration-code {
    border-color: var(--text-secondary);
  }
}
</style>
