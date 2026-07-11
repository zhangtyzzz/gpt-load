<script setup lang="ts">
import { keysApi } from "@/api/keys";
import { normalizeGenericHttpConfig, useChannelCatalog } from "@/composables/useChannelCatalog";
import ErrorPolicyEditor from "@/components/common/ErrorPolicyEditor.vue";
import ProxyKeysInput from "@/components/common/ProxyKeysInput.vue";
import GenericHttpChannelFields from "@/components/keys/GenericHttpChannelFields.vue";
import type { ChannelConfig, ChannelDescriptor } from "@/types/channels";
import type { AffinityRule, Group, GroupConfigOption, UpstreamInfo } from "@/types/models";
import {
  areValidHttpUpstreams,
  isHttpHeaderToken,
  isReservedProxyHeaderName,
  isValidHttpUpstreamUrl,
  sanitizeChannelSpecificFields,
} from "@/utils/channel-form";
import { Add, Close, HelpCircleOutline, Remove } from "@vicons/ionicons5";
import { useMediaQuery } from "@vueuse/core";
import {
  NButton,
  NCard,
  NCollapse,
  NCollapseItem,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSwitch,
  NTooltip,
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
  (e: "switchToGroup", groupId: number): void;
}

// 配置项类型
interface ConfigItem {
  key: string;
  value: number | string | boolean;
}

// Header规则类型
interface HeaderRuleItem {
  key: string;
  value: string;
  action: "set" | "remove";
}

// 亲和性规则类型
interface AffinityRuleItem {
  name: string;
  match: {
    path_regex: string;
    model_regex: string;
  };
  key_source: {
    type: "header" | "body_json" | "body_regex";
    key: string;
    path: string;
    pattern: string;
  };
  ttl_seconds: number | null;
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
const genericHttpConfigValid = ref(true);
const savedSnapshot = ref("");
const formInitialized = ref(false);
const isMobile = useMediaQuery("(max-width: 768px)");
const { catalog, channelTypes, loadCatalog } = useChannelCatalog();
const modelRedirectTip = `{
  "gpt-5": "gpt-5-2025-08-07",
  "gemini-2.5-flash": "gemini-2.5-flash-preview-09-2025"
}`;

// 表单数据接口
interface GroupFormData {
  name: string;
  display_name: string;
  description: string;
  upstreams: UpstreamInfo[];
  channel_type: string;
  channel_config?: ChannelConfig;
  sort: number;
  test_model: string;
  validation_endpoint: string;
  param_overrides: string;
  model_redirect_rules: string;
  model_redirect_strict: boolean;
  config: Record<string, number | string | boolean>;
  configItems: ConfigItem[];
  header_rules: HeaderRuleItem[];
  affinity_rules: AffinityRuleItem[];
  proxy_keys: string;
  group_type?: string;
}

// 表单数据
const formData = reactive<GroupFormData>({
  name: "",
  display_name: "",
  description: "",
  upstreams: [
    {
      url: "",
      weight: 1,
    },
  ] as UpstreamInfo[],
  channel_type: "openai",
  channel_config: undefined,
  sort: 1,
  test_model: "",
  validation_endpoint: "",
  param_overrides: "",
  model_redirect_rules: "",
  model_redirect_strict: false,
  config: {},
  configItems: [] as ConfigItem[],
  header_rules: [] as HeaderRuleItem[],
  affinity_rules: [] as AffinityRuleItem[],
  proxy_keys: "",
  group_type: "standard",
});

const configOptions = ref<GroupConfigOption[]>([]);
const configOptionsFetched = ref(false);
const hiddenConfigKeys = new Set(["failover_status_codes"]);

const currentChannelDescriptor = computed<ChannelDescriptor | undefined>(() =>
  channelTypes.value.find(item => item.id === formData.channel_type)
);
const isGenericHttp = computed(() => formData.channel_type === "generic-http");
const genericHttpPresets = computed(() => currentChannelDescriptor.value?.presets || []);
const channelTypeOptions = computed(() => {
  const options = channelTypes.value.map(item => ({
    label: item.display_name,
    value: item.id,
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

const defaultTestModel = computed(() => currentChannelDescriptor.value?.defaults.test_model || "");
const defaultUpstream = computed(() => currentChannelDescriptor.value?.defaults.upstream_url || "");
const testModelPlaceholder = computed(() => defaultTestModel.value || t("keys.enterModelName"));
const upstreamPlaceholder = computed(() => defaultUpstream.value || t("keys.enterUpstreamUrl"));
const validationEndpointPlaceholder = computed(
  () =>
    currentChannelDescriptor.value?.defaults.validation_endpoint || t("keys.enterValidationPath")
);
const showTestModel = computed(
  () => currentChannelDescriptor.value?.capabilities.test_model !== "hidden"
);
const showValidationEndpoint = computed(
  () => currentChannelDescriptor.value?.capabilities.validation_endpoint !== false
);
const showModelRedirect = computed(
  () => currentChannelDescriptor.value?.capabilities.model_redirect !== false
);
const showParamOverrides = computed(
  () => currentChannelDescriptor.value?.capabilities.param_overrides !== false
);
const showHeaderRules = computed(
  () => currentChannelDescriptor.value?.capabilities.header_rules !== false
);
const showAffinity = computed(
  () => currentChannelDescriptor.value?.capabilities.affinity !== false
);
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
  test_model: [
    {
      validator: (_rule, value: unknown) => {
        if (
          currentChannelDescriptor.value?.capabilities.test_model === "required" &&
          (typeof value !== "string" || !value.trim())
        ) {
          return new Error(t("keys.enterTestModel"));
        }
        return true;
      },
      trigger: ["blur", "input"],
    },
  ],
  upstreams: [
    {
      type: "array",
      min: 1,
      message: t("keys.atLeastOneUpstream"),
      trigger: ["blur", "change"],
    },
  ],
};

function upstreamUrlRule() {
  return {
    required: true,
    validator: (_rule: unknown, value: unknown) => {
      if (typeof value !== "string" || !value.trim()) {
        return new Error(t("keys.enterUpstreamUrl"));
      }
      if (isGenericHttp.value && !isValidHttpUpstreamUrl(value)) {
        return new Error(t("channels.validation.upstreamInvalid"));
      }
      return true;
    },
    trigger: ["blur", "input"],
  };
}

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
      if (!configOptionsFetched.value) {
        try {
          await fetchGroupConfigOptions();
        } catch {
          message.warning(t("keys.configOptionsLoadFailed"));
        }
      }
      if (run !== initializationRun || !props.show) {
        return;
      }
      resetForm();
      if (props.group) {
        loadGroupData();
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
  }
);

function handleChannelTypeChange(channelType: string) {
  const descriptor = channelTypes.value.find(item => item.id === channelType);
  const defaults = descriptor?.defaults || {
    upstream_url: "",
    test_model: "",
    validation_endpoint: "",
  };
  Object.assign(formData, {
    channel_type: channelType,
    upstreams: [{ url: defaults.upstream_url, weight: 1 }],
    test_model: defaults.test_model,
    validation_endpoint: defaults.validation_endpoint,
    channel_config:
      channelType === "generic-http" ? normalizeGenericHttpConfig(undefined) : undefined,
    param_overrides:
      descriptor?.capabilities.param_overrides === false ? "" : formData.param_overrides,
    model_redirect_rules:
      descriptor?.capabilities.model_redirect === false ? "" : formData.model_redirect_rules,
    model_redirect_strict:
      descriptor?.capabilities.model_redirect === false ? false : formData.model_redirect_strict,
  });
  genericHttpConfigValid.value = true;
}

// 重置表单
function resetForm() {
  const isCreateMode = !props.group;
  const defaultChannelType = catalog.value.default_channel_type || "openai";
  const descriptor = channelTypes.value.find(item => item.id === defaultChannelType);
  const defaults = descriptor?.defaults || {
    upstream_url: "",
    test_model: "",
    validation_endpoint: "",
  };

  Object.assign(formData, {
    name: "",
    display_name: "",
    description: "",
    upstreams: [
      {
        url: isCreateMode ? defaults.upstream_url : "",
        weight: 1,
      },
    ],
    channel_type: defaultChannelType,
    channel_config:
      isCreateMode && defaultChannelType === "generic-http"
        ? normalizeGenericHttpConfig(undefined)
        : undefined,
    sort: 1,
    test_model: isCreateMode ? defaults.test_model : "",
    validation_endpoint: isCreateMode ? defaults.validation_endpoint : "",
    param_overrides: "",
    model_redirect_rules: "",
    model_redirect_strict: false,
    config: {},
    configItems: [],
    header_rules: [],
    affinity_rules: [],
    proxy_keys: "",
    group_type: "standard",
  });
  genericHttpConfigValid.value = true;
}

// 加载分组数据（编辑模式）
function loadGroupData() {
  if (!props.group) {
    return;
  }

  const configItems = Object.entries(props.group.config || {})
    .filter(([key]) => !hiddenConfigKeys.has(key))
    .map(([key, value]) => {
      return {
        key,
        value,
      };
    });
  Object.assign(formData, {
    name: props.group.name || "",
    display_name: props.group.display_name || "",
    description: props.group.description || "",
    upstreams: props.group.upstreams?.length
      ? [...props.group.upstreams]
      : [{ url: "", weight: 1 }],
    channel_type: props.group.channel_type || "openai",
    channel_config: props.group.channel_config
      ? JSON.parse(JSON.stringify(props.group.channel_config))
      : undefined,
    sort: props.group.sort || 1,
    test_model: props.group.test_model || "",
    validation_endpoint: props.group.validation_endpoint || "",
    param_overrides: JSON.stringify(props.group.param_overrides || {}, null, 2),
    model_redirect_rules: JSON.stringify(props.group.model_redirect_rules || {}, null, 2),
    model_redirect_strict: props.group.model_redirect_strict || false,
    config: {},
    configItems,
    header_rules: (props.group.header_rules || []).map((rule: HeaderRuleItem) => ({
      key: rule.key || "",
      value: rule.value || "",
      action: (rule.action as "set" | "remove") || "set",
    })),
    affinity_rules: (props.group.affinity_rules || []).map((rule: AffinityRule) => ({
      name: rule.name || "",
      match: {
        path_regex: rule.match?.path_regex || "",
        model_regex: rule.match?.model_regex || "",
      },
      key_source: {
        type: rule.key_source?.type || "header",
        key: rule.key_source?.key || "",
        path: rule.key_source?.path || "",
        pattern: rule.key_source?.pattern || "",
      },
      ttl_seconds: rule.ttl_seconds || null,
    })),
    proxy_keys: props.group.proxy_keys || "",
    group_type: props.group.group_type || "standard",
  });
}

// 添加上游地址
function addUpstream() {
  formData.upstreams.push({
    url: "",
    weight: 1,
  });
  markGenericPresetCustom();
}

// 删除上游地址
function removeUpstream(index: number) {
  if (formData.upstreams.length > 1) {
    formData.upstreams.splice(index, 1);
    markGenericPresetCustom();
  } else {
    message.warning(t("keys.atLeastOneUpstream"));
  }
}

function replaceGenericUpstreams(upstreams: UpstreamInfo[]) {
  formData.upstreams = upstreams.map(upstream => ({ ...upstream }));
}

function applyGenericPreset(payload: { config: ChannelConfig; upstreams: UpstreamInfo[] }) {
  Object.assign(formData, {
    channel_config: JSON.parse(JSON.stringify(payload.config)),
    upstreams: payload.upstreams.map(upstream => ({ ...upstream })),
  });
}

function markGenericPresetCustom() {
  if (!isGenericHttp.value || !formData.channel_config) {
    return;
  }
  const normalized = normalizeGenericHttpConfig(formData.channel_config);
  if (normalized.preset_id !== "custom") {
    formData.channel_config = { ...normalized, preset_id: "custom" };
  }
}

async function fetchGroupConfigOptions() {
  const options = await keysApi.getGroupConfigOptions();
  configOptions.value = options || [];
  configOptionsFetched.value = true;
}

// 添加配置项
function addConfigItem() {
  formData.configItems.push({
    key: "",
    value: "",
  });
}

// 删除配置项
function removeConfigItem(index: number) {
  formData.configItems.splice(index, 1);
}

// 添加Header规则
function addHeaderRule() {
  formData.header_rules.push({
    key: "",
    value: "",
    action: "set",
  });
}

// 删除Header规则
function removeHeaderRule(index: number) {
  formData.header_rules.splice(index, 1);
}

// 亲和性规则 source type 选项
const affinitySourceTypeOptions = [
  { label: "Header", value: "header" },
  { label: "Body JSON", value: "body_json" },
  { label: "Body Regex", value: "body_regex" },
];

// 添加亲和性规则
function addAffinityRule() {
  formData.affinity_rules.push({
    name: "",
    match: {
      path_regex: "",
      model_regex: "",
    },
    key_source: {
      type: "header",
      key: "",
      path: "",
      pattern: "",
    },
    ttl_seconds: null,
  });
}

// 删除亲和性规则
function removeAffinityRule(index: number) {
  formData.affinity_rules.splice(index, 1);
}

// 规范化Header Key到Canonical格式（模拟HTTP标准）
function canonicalHeaderKey(key: string): string {
  if (!key) {
    return key;
  }
  return key
    .split("-")
    .map(part => part.charAt(0).toUpperCase() + part.slice(1).toLowerCase())
    .join("-");
}

// 验证Header Key唯一性（使用Canonical格式对比）
function validateHeaderKeyUniqueness(
  rules: HeaderRuleItem[],
  currentIndex: number,
  key: string
): boolean {
  if (!key.trim()) {
    return true;
  }

  const canonicalKey = canonicalHeaderKey(key.trim());
  return !rules.some(
    (rule, index) => index !== currentIndex && canonicalHeaderKey(rule.key.trim()) === canonicalKey
  );
}

function getHeaderRuleError(index: number, key: string): string {
  const name = key.trim();
  if (!name) {
    return "";
  }
  if (!validateHeaderKeyUniqueness(formData.header_rules, index, name)) {
    return t("keys.duplicateHeader");
  }
  if (!isHttpHeaderToken(name) || isReservedProxyHeaderName(name)) {
    return t("channels.validation.headersProtected");
  }
  if (isGenericHttp.value) {
    const genericConfig = normalizeGenericHttpConfig(formData.channel_config);
    const lower = name.toLowerCase();
    if (genericConfig.auth.name.toLowerCase() === lower) {
      return t("channels.validation.headersProtected");
    }
  }
  return "";
}

// 当配置项的key改变时，设置默认值
function handleConfigKeyChange(index: number, key: string) {
  const option = configOptions.value.find(opt => opt.key === key);
  if (option) {
    formData.configItems[index].value =
      key === "error_policy" ? '{\n  "rules": []\n}' : option.default_value;
  }
}

const getConfigOption = (key: string) => {
  return configOptions.value.find(opt => opt.key === key);
};

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

    if (isGenericHttp.value && !genericHttpConfigValid.value) {
      message.error(t("channels.validation.fixErrors"));
      return;
    }
    if (isGenericHttp.value && !areValidHttpUpstreams(formData.upstreams)) {
      message.error(t("channels.validation.allUpstreamsInvalid"));
      return;
    }
    const genericConfig = isGenericHttp.value
      ? normalizeGenericHttpConfig(formData.channel_config)
      : undefined;
    const invalidHeaderRule = formData.header_rules.findIndex((rule, index) =>
      Boolean(getHeaderRuleError(index, rule.key))
    );
    if (invalidHeaderRule >= 0) {
      message.error(
        getHeaderRuleError(invalidHeaderRule, formData.header_rules[invalidHeaderRule].key)
      );
      return;
    }

    loading.value = true;

    // 验证 JSON 格式
    let paramOverrides = {};
    if (showParamOverrides.value && formData.param_overrides) {
      try {
        paramOverrides = JSON.parse(formData.param_overrides);
      } catch {
        message.error(t("keys.invalidJsonFormat"));
        return;
      }
    }

    // 验证模型重定向规则 JSON 格式
    let modelRedirectRules = {};
    if (showModelRedirect.value && formData.model_redirect_rules) {
      try {
        modelRedirectRules = JSON.parse(formData.model_redirect_rules);

        // Validate rule format
        for (const [key, value] of Object.entries(modelRedirectRules)) {
          if (typeof key !== "string" || typeof value !== "string") {
            message.error(t("keys.modelRedirectInvalidFormat"));
            return;
          }
          if (key.trim() === "" || (value as string).trim() === "") {
            message.error(t("keys.modelRedirectEmptyModel"));
            return;
          }
        }
      } catch {
        message.error(t("keys.modelRedirectInvalidJson"));
        return;
      }
    }

    // 将configItems转换为config对象，保留不再展示的兼容配置，避免编辑分组时静默丢失。
    const config: Record<string, number | string | boolean> = {};
    Object.entries(props.group?.config || {}).forEach(([key, value]) => {
      if (
        hiddenConfigKeys.has(key) &&
        (typeof value === "number" || typeof value === "string" || typeof value === "boolean")
      ) {
        config[key] = value;
      }
    });
    formData.configItems.forEach((item: ConfigItem) => {
      if (item.key && item.key.trim()) {
        const option = configOptions.value.find(opt => opt.key === item.key);
        if (option && typeof option.default_value === "number" && typeof item.value === "string") {
          const numValue = Number(item.value);
          config[item.key] = isNaN(numValue) ? 0 : numValue;
        } else {
          config[item.key] = item.value;
        }
      }
    });

    const channelSpecificFields = sanitizeChannelSpecificFields(
      currentChannelDescriptor.value?.capabilities,
      {
        param_overrides: paramOverrides,
        model_redirect_rules: modelRedirectRules as Record<string, string>,
        model_redirect_strict: formData.model_redirect_strict,
      }
    );

    // 构建提交数据
    const submitData = {
      name: formData.name,
      display_name: formData.display_name,
      description: formData.description,
      upstreams: formData.upstreams.filter((upstream: UpstreamInfo) => upstream.url.trim()),
      channel_type: formData.channel_type,
      channel_config: isGenericHttp.value ? genericConfig : undefined,
      sort: formData.sort,
      test_model: showTestModel.value ? formData.test_model : "",
      validation_endpoint: showValidationEndpoint.value ? formData.validation_endpoint : "",
      ...channelSpecificFields,
      config,
      header_rules: formData.header_rules
        .filter((rule: HeaderRuleItem) => rule.key.trim())
        .map((rule: HeaderRuleItem) => ({
          key: rule.key.trim(),
          value: rule.value,
          action: rule.action,
        })),
      affinity_rules: formData.affinity_rules
        .filter((rule: AffinityRuleItem) => rule.name.trim())
        .map((rule: AffinityRuleItem) => ({
          name: rule.name.trim(),
          match: {
            path_regex: rule.match.path_regex || undefined,
            model_regex: rule.match.model_regex || undefined,
          },
          key_source: {
            type: rule.key_source.type,
            key: rule.key_source.type === "header" ? rule.key_source.key : undefined,
            path: rule.key_source.type === "body_json" ? rule.key_source.path : undefined,
            pattern: rule.key_source.type === "body_regex" ? rule.key_source.pattern : undefined,
          },
          ttl_seconds: rule.ttl_seconds || undefined,
        })),
      proxy_keys: formData.proxy_keys,
    };

    let res: Group;
    if (props.group?.id) {
      // 编辑模式
      res = await keysApi.updateGroup(props.group.id, submitData);
    } else {
      // 新建模式
      res = await keysApi.createGroup(submitData);
    }

    clearDirtyTracking();
    emit("success", res);
    // 如果是新建模式，发出切换到新分组的事件
    if (!props.group?.id && res.id) {
      emit("switchToGroup", res.id);
    }
    closeWithoutPrompt();
  } finally {
    loading.value = false;
  }
}

defineExpose({
  formData,
  handleChannelTypeChange,
  handleSubmit,
  initializing,
  isDirty,
  requestClose,
});
</script>

<template>
  <n-modal :show="show" @update:show="handleModalShowChange" class="group-form-modal">
    <n-card
      class="group-form-card"
      :title="group ? t('keys.editGroup') : t('keys.createGroup')"
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
        require-mark-placement="right-hanging"
        class="group-form"
        :disabled="initializing"
      >
        <!-- 基础信息 -->
        <div class="form-section">
          <h4 class="section-title">{{ t("keys.basicInfo") }}</h4>

          <!-- Group name and display name on the same row -->
          <div class="form-row">
            <n-form-item :label="t('keys.groupName')" path="name" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.groupName") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.groupNameTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-input v-model:value="formData.name" placeholder="gemini" />
            </n-form-item>

            <n-form-item :label="t('keys.displayName')" path="display_name" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.displayName") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.displayNameTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-input v-model:value="formData.display_name" placeholder="Google Gemini" />
            </n-form-item>
          </div>

          <!-- Channel type and sort order on the same row -->
          <div class="form-row">
            <n-form-item :label="t('keys.channelType')" path="channel_type" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.channelType") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.channelTypeTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-select
                :value="formData.channel_type"
                :options="channelTypeOptions"
                :placeholder="t('keys.selectChannelType')"
                @update:value="handleChannelTypeChange"
              />
            </n-form-item>

            <n-form-item :label="t('keys.sortOrder')" path="sort" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.sortOrder") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.sortOrderTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-input-number
                v-model:value="formData.sort"
                :min="0"
                :placeholder="t('keys.sortValue')"
                style="width: 100%"
              />
            </n-form-item>
          </div>

          <generic-http-channel-fields
            v-if="isGenericHttp"
            v-model="formData.channel_config"
            :presets="genericHttpPresets"
            :upstreams="formData.upstreams"
            :group-name="formData.name"
            :catalog-loading="initializing"
            @update:upstreams="replaceGenericUpstreams"
            @apply:preset="applyGenericPreset"
            @validity="genericHttpConfigValid = $event"
          />

          <!-- Test model and test path on the same row -->
          <div v-if="showTestModel || showValidationEndpoint" class="form-row">
            <n-form-item
              v-if="showTestModel"
              :label="t('keys.testModel')"
              path="test_model"
              class="form-item-half"
            >
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.testModel") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.testModelTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-input v-model:value="formData.test_model" :placeholder="testModelPlaceholder" />
            </n-form-item>

            <n-form-item
              v-if="showValidationEndpoint"
              :label="t('keys.testPath')"
              path="validation_endpoint"
              class="form-item-half"
            >
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.testPath") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    <div>
                      {{ t("keys.testPathTooltip1") }}
                      <br />
                      • OpenAI: /v1/chat/completions
                      <br />
                      • OpenAI Response: /v1/responses
                      <br />
                      • Anthropic: /v1/messages
                      <br />
                      {{ t("keys.testPathTooltip2") }}
                    </div>
                  </n-tooltip>
                </div>
              </template>
              <n-input
                v-model:value="formData.validation_endpoint"
                :placeholder="
                  validationEndpointPlaceholder || t('keys.optionalCustomValidationPath')
                "
              />
            </n-form-item>

            <div v-if="!showTestModel || !showValidationEndpoint" class="form-item-half" />
          </div>

          <!-- Proxy keys -->
          <n-form-item :label="t('keys.proxyKeys')" path="proxy_keys">
            <template #label>
              <div class="form-label-with-tooltip">
                {{ t("keys.proxyKeys") }}
                <n-tooltip trigger="hover" placement="top">
                  <template #trigger>
                    <n-icon :component="HelpCircleOutline" class="help-icon" />
                  </template>
                  {{ t("keys.proxyKeysTooltip") }}
                </n-tooltip>
              </div>
            </template>
            <proxy-keys-input
              v-model="formData.proxy_keys"
              :placeholder="t('keys.multiKeysPlaceholder')"
              size="medium"
            />
          </n-form-item>

          <!-- Description takes full row -->
          <n-form-item :label="t('common.description')" path="description">
            <template #label>
              <div class="form-label-with-tooltip">
                {{ t("common.description") }}
                <n-tooltip trigger="hover" placement="top">
                  <template #trigger>
                    <n-icon :component="HelpCircleOutline" class="help-icon" />
                  </template>
                  {{ t("keys.descriptionTooltip") }}
                </n-tooltip>
              </div>
            </template>
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

        <!-- Upstream addresses -->
        <div class="form-section" style="margin-top: 10px">
          <h4 class="section-title">{{ t("keys.upstreamAddresses") }}</h4>
          <n-form-item
            v-for="(upstream, index) in formData.upstreams"
            :key="index"
            :label="`${t('keys.upstream')} ${index + 1}`"
            :path="`upstreams[${index}].url`"
            :rule="upstreamUrlRule()"
          >
            <template #label>
              <div class="form-label-with-tooltip">
                {{ t("keys.upstream") }} {{ index + 1 }}
                <n-tooltip trigger="hover" placement="top">
                  <template #trigger>
                    <n-icon :component="HelpCircleOutline" class="help-icon" />
                  </template>
                  {{ t("keys.upstreamTooltip") }}
                </n-tooltip>
              </div>
            </template>
            <div class="upstream-row">
              <div class="upstream-url">
                <n-input
                  v-model:value="upstream.url"
                  :placeholder="upstreamPlaceholder"
                  @input="markGenericPresetCustom"
                />
              </div>
              <div class="upstream-weight">
                <span class="weight-label">{{ t("keys.weight") }}</span>
                <n-tooltip trigger="hover" placement="top" style="width: 100%">
                  <template #trigger>
                    <n-input-number
                      v-model:value="upstream.weight"
                      :min="0"
                      :placeholder="t('keys.weight')"
                      style="width: 100%"
                      @update:value="markGenericPresetCustom"
                    />
                  </template>
                  {{ t("keys.weightTooltip") }}
                </n-tooltip>
              </div>
              <div class="upstream-actions">
                <n-button
                  v-if="formData.upstreams.length > 1"
                  @click="removeUpstream(index)"
                  type="error"
                  quaternary
                  circle
                  size="small"
                  :aria-label="t('common.delete')"
                >
                  <template #icon>
                    <n-icon :component="Remove" />
                  </template>
                </n-button>
              </div>
            </div>
          </n-form-item>

          <n-form-item>
            <n-button @click="addUpstream" dashed style="width: 100%">
              <template #icon>
                <n-icon :component="Add" />
              </template>
              {{ t("keys.addUpstream") }}
            </n-button>
          </n-form-item>
        </div>

        <!-- Advanced configuration -->
        <div class="form-section" style="margin-top: 10px">
          <n-collapse>
            <n-collapse-item name="advanced">
              <template #header>{{ t("keys.advancedConfig") }}</template>
              <div class="config-section">
                <h5 class="config-title-with-tooltip">
                  {{ t("keys.groupConfig") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                    </template>
                    {{ t("keys.groupConfigTooltip") }}
                  </n-tooltip>
                </h5>

                <div class="config-items">
                  <n-form-item
                    v-for="(configItem, index) in formData.configItems"
                    :key="index"
                    class="config-item-row"
                    :label="`${t('keys.config')} ${index + 1}`"
                    :path="`configItems[${index}].key`"
                    :rule="{
                      required: true,
                      message: '',
                      trigger: ['blur', 'change'],
                    }"
                  >
                    <template #label>
                      <div class="form-label-with-tooltip">
                        {{ t("keys.config") }} {{ index + 1 }}
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-icon :component="HelpCircleOutline" class="help-icon" />
                          </template>
                          {{ t("keys.configTooltip") }}
                        </n-tooltip>
                      </div>
                    </template>
                    <div
                      class="config-item-content"
                      :class="{
                        'config-item-content-policy': configItem.key === 'error_policy',
                      }"
                    >
                      <div class="config-select">
                        <n-select
                          v-model:value="configItem.key"
                          :options="
                            configOptions.map(opt => ({
                              label: opt.name,
                              value: opt.key,
                              disabled:
                                formData.configItems
                                  .map((item: ConfigItem) => item.key)
                                  ?.includes(opt.key) && opt.key !== configItem.key,
                            }))
                          "
                          :placeholder="t('keys.selectConfigParam')"
                          @update:value="value => handleConfigKeyChange(index, value)"
                          clearable
                        />
                      </div>
                      <div
                        class="config-value"
                        :class="{ 'config-value-policy': configItem.key === 'error_policy' }"
                      >
                        <error-policy-editor
                          v-if="configItem.key === 'error_policy'"
                          :model-value="String(configItem.value || '')"
                          @update:model-value="value => (configItem.value = value)"
                        />
                        <n-tooltip v-else trigger="hover" placement="top">
                          <template #trigger>
                            <n-input-number
                              v-if="typeof configItem.value === 'number'"
                              v-model:value="configItem.value"
                              :placeholder="t('keys.paramValue')"
                              :precision="0"
                              style="width: 100%"
                            />
                            <n-switch
                              v-else-if="typeof configItem.value === 'boolean'"
                              v-model:value="configItem.value"
                              size="small"
                            />
                            <n-input
                              v-else
                              v-model:value="configItem.value"
                              :placeholder="t('keys.paramValue')"
                            />
                          </template>
                          {{
                            getConfigOption(configItem.key)?.description || t("keys.setConfigValue")
                          }}
                        </n-tooltip>
                      </div>
                      <div class="config-actions">
                        <n-button
                          @click="removeConfigItem(index)"
                          type="error"
                          quaternary
                          circle
                          size="small"
                          :aria-label="t('common.delete')"
                        >
                          <template #icon>
                            <n-icon :component="Remove" />
                          </template>
                        </n-button>
                      </div>
                    </div>
                  </n-form-item>
                </div>

                <div style="margin-top: 12px; padding-left: 120px">
                  <n-button
                    @click="addConfigItem"
                    dashed
                    style="width: 100%"
                    :disabled="formData.configItems.length >= configOptions.length"
                  >
                    <template #icon>
                      <n-icon :component="Add" />
                    </template>
                    {{ t("keys.addConfigParam") }}
                  </n-button>
                </div>
              </div>

              <div v-if="showHeaderRules" class="config-section">
                <h5 class="config-title-with-tooltip">
                  {{ t("keys.customHeaders") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                    </template>
                    <div>
                      {{ t("keys.headerRulesTooltip1") }}
                      <br />
                      {{ t("keys.supportedVariables") }}：
                      <br />
                      • ${CLIENT_IP} - {{ t("keys.clientIpVar") }}
                      <br />
                      • ${GROUP_NAME} - {{ t("keys.groupNameVar") }}
                      <br />
                      • ${API_KEY} - {{ t("keys.apiKeyVar") }}
                      <br />
                      • ${TIMESTAMP_MS} - {{ t("keys.timestampMsVar") }}
                      <br />
                      • ${TIMESTAMP_S} - {{ t("keys.timestampSVar") }}
                    </div>
                  </n-tooltip>
                </h5>

                <div class="header-rules-items">
                  <n-form-item
                    v-for="(headerRule, index) in formData.header_rules"
                    :key="index"
                    class="header-rule-row"
                    :label="`${t('keys.header')} ${index + 1}`"
                  >
                    <template #label>
                      <div class="form-label-with-tooltip">
                        {{ t("keys.header") }} {{ index + 1 }}
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-icon :component="HelpCircleOutline" class="help-icon" />
                          </template>
                          {{ t("keys.headerTooltip") }}
                        </n-tooltip>
                      </div>
                    </template>
                    <div class="header-rule-content">
                      <div class="header-name">
                        <n-input
                          v-model:value="headerRule.key"
                          :placeholder="t('keys.headerName')"
                          :status="getHeaderRuleError(index, headerRule.key) ? 'error' : undefined"
                        />
                        <div v-if="getHeaderRuleError(index, headerRule.key)" class="error-message">
                          {{ getHeaderRuleError(index, headerRule.key) }}
                        </div>
                      </div>
                      <div class="header-value" v-if="headerRule.action === 'set'">
                        <n-input
                          v-model:value="headerRule.value"
                          :placeholder="t('keys.headerValuePlaceholder')"
                        />
                      </div>
                      <div class="header-value removed-placeholder" v-else>
                        <span class="removed-text">{{ t("keys.willRemoveFromRequest") }}</span>
                      </div>
                      <div class="header-action">
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-switch
                              v-model:value="headerRule.action"
                              :checked-value="'remove'"
                              :unchecked-value="'set'"
                              size="small"
                            />
                          </template>
                          {{ t("keys.removeToggleTooltip") }}
                        </n-tooltip>
                      </div>
                      <div class="header-actions">
                        <n-button
                          @click="removeHeaderRule(index)"
                          type="error"
                          quaternary
                          circle
                          size="small"
                          :aria-label="t('common.delete')"
                        >
                          <template #icon>
                            <n-icon :component="Remove" />
                          </template>
                        </n-button>
                      </div>
                    </div>
                  </n-form-item>
                </div>

                <div style="margin-top: 12px; padding-left: 120px">
                  <n-button @click="addHeaderRule" dashed style="width: 100%">
                    <template #icon>
                      <n-icon :component="Add" />
                    </template>
                    {{ t("keys.addHeader") }}
                  </n-button>
                </div>
              </div>

              <!-- Key 亲和性规则 -->
              <div v-if="showAffinity" class="config-section">
                <h5 class="config-title-with-tooltip">
                  {{ t("keys.keyAffinity") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                    </template>
                    {{ t("keys.keyAffinityTooltip") }}
                  </n-tooltip>
                </h5>

                <div class="affinity-rules-items">
                  <div
                    v-for="(rule, index) in formData.affinity_rules"
                    :key="index"
                    class="affinity-rule-card"
                  >
                    <div class="affinity-rule-header">
                      <span class="affinity-rule-title">
                        {{ t("keys.addAffinityRule") }} {{ index + 1 }}
                      </span>
                      <n-button
                        @click="removeAffinityRule(index)"
                        type="error"
                        quaternary
                        circle
                        size="small"
                        :aria-label="t('common.delete')"
                      >
                        <template #icon>
                          <n-icon :component="Remove" />
                        </template>
                      </n-button>
                    </div>

                    <div class="affinity-rule-body">
                      <!-- 规则名称 -->
                      <div class="affinity-row">
                        <span class="affinity-label">{{ t("keys.affinityRuleName") }}</span>
                        <n-input
                          v-model:value="rule.name"
                          :placeholder="t('keys.affinityRuleNamePlaceholder')"
                          size="small"
                        />
                      </div>

                      <!-- 匹配条件 -->
                      <div class="affinity-row">
                        <span class="affinity-label">{{ t("keys.affinityMatchPathRegex") }}</span>
                        <n-input
                          v-model:value="rule.match.path_regex"
                          :placeholder="t('keys.affinityMatchPathRegexPlaceholder')"
                          size="small"
                        />
                      </div>
                      <div class="affinity-row">
                        <span class="affinity-label">{{ t("keys.affinityMatchModelRegex") }}</span>
                        <n-input
                          v-model:value="rule.match.model_regex"
                          :placeholder="t('keys.affinityMatchModelRegexPlaceholder')"
                          size="small"
                        />
                      </div>

                      <!-- 提取源类型 -->
                      <div class="affinity-row">
                        <span class="affinity-label">{{ t("keys.affinitySourceType") }}</span>
                        <n-select
                          v-model:value="rule.key_source.type"
                          :options="affinitySourceTypeOptions"
                          size="small"
                        />
                      </div>

                      <!-- 根据类型显示不同的输入框 -->
                      <div v-if="rule.key_source.type === 'header'" class="affinity-row">
                        <span class="affinity-label">{{ t("keys.affinitySourceKey") }}</span>
                        <n-input
                          v-model:value="rule.key_source.key"
                          :placeholder="t('keys.affinitySourceKeyPlaceholder')"
                          size="small"
                        />
                      </div>
                      <div v-if="rule.key_source.type === 'body_json'" class="affinity-row">
                        <span class="affinity-label">{{ t("keys.affinitySourcePath") }}</span>
                        <n-input
                          v-model:value="rule.key_source.path"
                          :placeholder="t('keys.affinitySourcePathPlaceholder')"
                          size="small"
                        />
                      </div>
                      <div v-if="rule.key_source.type === 'body_regex'" class="affinity-row">
                        <span class="affinity-label">{{ t("keys.affinitySourcePattern") }}</span>
                        <n-input
                          v-model:value="rule.key_source.pattern"
                          :placeholder="t('keys.affinitySourcePatternPlaceholder')"
                          size="small"
                        />
                      </div>

                      <!-- TTL -->
                      <div class="affinity-row">
                        <span class="affinity-label">{{ t("keys.affinityTTL") }}</span>
                        <n-input-number
                          v-model:value="rule.ttl_seconds"
                          :placeholder="t('keys.affinityTTLPlaceholder')"
                          :min="0"
                          size="small"
                          style="width: 100%"
                        />
                      </div>
                    </div>
                  </div>
                </div>

                <div style="margin-top: 12px; padding-left: 120px">
                  <n-button @click="addAffinityRule" dashed style="width: 100%">
                    <template #icon>
                      <n-icon :component="Add" />
                    </template>
                    {{ t("keys.addAffinityRule") }}
                  </n-button>
                </div>
              </div>

              <!-- 模型重定向配置 -->
              <div
                v-if="formData.group_type !== 'aggregate' && showModelRedirect"
                class="config-section"
              >
                <n-form-item path="model_redirect_strict">
                  <template #label>
                    <div class="form-label-with-tooltip">
                      {{ t("keys.modelRedirectPolicy") }}
                      <n-tooltip trigger="hover" placement="top">
                        <template #trigger>
                          <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                        </template>
                        {{ t("keys.modelRedirectPolicyTooltip") }}
                      </n-tooltip>
                    </div>
                  </template>
                  <div style="display: flex; align-items: center; gap: 12px">
                    <n-switch v-model:value="formData.model_redirect_strict" />
                    <span style="font-size: 14px; color: #666">
                      {{
                        formData.model_redirect_strict
                          ? t("keys.modelRedirectStrictMode")
                          : t("keys.modelRedirectLooseMode")
                      }}
                    </span>
                  </div>
                  <template #feedback>
                    <div style="font-size: 12px; color: #999; margin: 4px 0">
                      <div v-if="formData.model_redirect_strict" style="color: #f5a623">
                        ⚠️ {{ t("keys.modelRedirectStrictWarning") }}
                      </div>
                      <div v-else style="color: #52c41a">
                        ✅ {{ t("keys.modelRedirectLooseInfo") }}
                      </div>
                    </div>
                  </template>
                </n-form-item>

                <n-form-item path="model_redirect_rules">
                  <template #label>
                    <div class="form-label-with-tooltip">
                      {{ t("keys.modelRedirectRules") }}
                      <n-tooltip trigger="hover" placement="top">
                        <template #trigger>
                          <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                        </template>
                        {{ t("keys.modelRedirectRulesTooltip") }}
                      </n-tooltip>
                    </div>
                  </template>
                  <n-input
                    v-model:value="formData.model_redirect_rules"
                    type="textarea"
                    :placeholder="modelRedirectTip"
                    :rows="4"
                  />
                  <template #feedback>
                    <div style="font-size: 14px; color: #999">
                      {{ t("keys.modelRedirectRulesDescription") }}
                    </div>
                  </template>
                </n-form-item>
              </div>

              <div v-if="showParamOverrides" class="config-section">
                <n-form-item path="param_overrides">
                  <template #label>
                    <div class="form-label-with-tooltip">
                      {{ t("keys.paramOverrides") }}
                      <n-tooltip trigger="hover" placement="top">
                        <template #trigger>
                          <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                        </template>
                        {{ t("keys.paramOverridesTooltip") }}
                      </n-tooltip>
                    </div>
                  </template>
                  <n-input
                    v-model:value="formData.param_overrides"
                    type="textarea"
                    placeholder='{"temperature": 0.7}'
                    :rows="4"
                  />
                </n-form-item>
              </div>
            </n-collapse-item>
          </n-collapse>
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
.group-form-modal {
  width: min(880px, calc(100vw - 2rem));
}

.group-form-card {
  display: flex;
  max-height: calc(100dvh - 2rem);
  flex-direction: column;
  overflow: hidden;
}

.form-section {
  margin-top: 20px;
}

.section-title {
  font-size: 1rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 16px 0;
  padding-bottom: 8px;
  border-bottom: 2px solid var(--border-color);
}

:deep(.n-form-item-label) {
  font-weight: 500;
}

:deep(.n-form-item-blank) {
  flex-grow: 1;
}

:deep(.n-input) {
  --n-border-radius: 6px;
}

:deep(.n-select) {
  --n-border-radius: 6px;
}

:deep(.n-input-number) {
  --n-border-radius: 6px;
}

:deep(.n-card-header) {
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color);
  padding: 10px 20px;
}

:deep(.n-card__content) {
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
}

:deep(.n-card__footer) {
  flex-shrink: 0;
  border-top: 1px solid var(--border-color);
  padding: 10px 15px;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

:deep(.n-form-item-feedback-wrapper) {
  min-height: 10px;
}

.config-section {
  margin-top: 16px;
}

.config-title {
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 12px 0;
}

.form-label {
  margin-left: 25px;
  margin-right: 10px;
  height: 34px;
  line-height: 34px;
  font-weight: 500;
}

/* Tooltip相关样式 */
.form-label-with-tooltip {
  display: flex;
  align-items: center;
  gap: 6px;
}

.help-icon {
  color: var(--text-tertiary);
  font-size: 14px;
  cursor: help;
  transition: color 0.2s ease;
}

.help-icon:hover {
  color: var(--primary-color);
}

.section-title-with-tooltip {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
}

.section-help {
  font-size: 16px;
}

.collapse-header-with-tooltip {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
}

.collapse-help {
  font-size: 14px;
}

.config-title-with-tooltip {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 12px 0;
}

.config-help {
  font-size: 13px;
}

/* 增强表单样式 */
:deep(.n-form-item-label) {
  font-weight: 500;
  color: var(--text-primary);
}

:deep(.n-input) {
  --n-border-radius: 8px;
  --n-border: 1px solid var(--border-color);
  --n-border-hover: 1px solid var(--primary-color);
  --n-border-focus: 1px solid var(--primary-color);
  --n-box-shadow-focus: 0 0 0 2px var(--primary-color-suppl);
}

:deep(.n-select) {
  --n-border-radius: 8px;
}

:deep(.n-input-number) {
  --n-border-radius: 8px;
}

:deep(.n-button) {
  --n-border-radius: 8px;
}

/* 美化tooltip */
:deep(.n-tooltip__trigger) {
  display: inline-flex;
  align-items: center;
}

:deep(.n-tooltip) {
  --n-font-size: 13px;
  --n-border-radius: 8px;
}

:deep(.n-tooltip .n-tooltip__content) {
  max-width: 320px;
  line-height: 1.5;
}

:deep(.n-tooltip .n-tooltip__content div) {
  white-space: pre-line;
}

/* 折叠面板样式优化 */
:deep(.n-collapse-item__header) {
  font-weight: 500;
  color: var(--text-primary);
}

:deep(.n-collapse-item) {
  --n-title-padding: 16px 0;
}

:deep(.n-base-selection-label) {
  height: 40px;
}

/* 表单行布局 */
.form-row {
  display: flex;
  gap: 20px;
  align-items: flex-start;
}

.form-item-half {
  flex: 1;
  width: 50%;
}

/* 上游地址行布局 */
.upstream-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}

.upstream-url {
  flex: 1;
}

.upstream-weight {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 140px;
}

.weight-label {
  font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap;
}

.upstream-actions {
  flex: 0 0 32px;
  display: flex;
  justify-content: center;
}

/* 配置项行布局 */
.config-item-row {
  margin-bottom: 12px;
}

.config-item-content {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-width: 0;
}

.config-select {
  flex: 0 0 200px;
  min-width: 0;
}

.config-value {
  flex: 1;
  min-width: 0;
}

.config-item-content-policy {
  display: grid;
  grid-template-columns: minmax(180px, 260px) minmax(0, 1fr) 32px;
  align-items: start;
}

.config-item-content-policy .config-select {
  grid-column: 1;
  grid-row: 1;
  width: 100%;
}

.config-item-content-policy .config-value-policy {
  grid-column: 1 / -2;
  grid-row: 2;
  width: 100%;
}

.config-item-content-policy .config-actions {
  grid-column: 3;
  grid-row: 1;
  align-self: center;
}

.config-actions {
  flex: 0 0 32px;
  display: flex;
  justify-content: center;
}

@media (max-width: 768px) {
  .group-form-modal {
    width: calc(100vw - 1rem);
  }

  .group-form-card {
    width: 100% !important;
    max-height: calc(100dvh - 1rem);
  }

  .group-form-card :deep(.n-card-header),
  .group-form-card :deep(.n-card__content),
  .group-form-card :deep(.n-card__footer) {
    padding-inline: var(--space-3);
  }

  .group-form-card :deep(.n-card__footer) {
    padding-bottom: max(var(--space-3), env(safe-area-inset-bottom));
  }

  .group-form {
    width: auto !important;
  }

  .form-row {
    flex-direction: column;
    gap: 0;
  }

  .form-item-half {
    width: 100%;
  }

  .section-title {
    font-size: 0.9rem;
  }

  .upstream-row,
  .config-item-content {
    flex-direction: column;
    gap: 8px;
    align-items: stretch;
  }

  .upstream-weight {
    flex: 1;
    flex-direction: column;
    align-items: flex-start;
  }

  .config-value {
    flex: 1;
  }

  .config-item-content-policy {
    display: flex;
  }

  .upstream-actions,
  .config-actions {
    justify-content: flex-end;
  }

  .modal-footer {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .modal-footer :deep(.n-button) {
    min-height: 44px;
  }
}

/* Header规则相关样式 */
.header-rule-row {
  margin-bottom: 12px;
}

.header-rule-content {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  width: 100%;
}

.header-name {
  flex: 0 0 200px;
  position: relative;
}

.header-value {
  flex: 1;
  display: flex;
  align-items: center;
  min-height: 34px;
}

.header-value.removed-placeholder {
  justify-content: center;
  background-color: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 0 12px;
}

.removed-text {
  color: var(--text-tertiary);
  font-style: italic;
  font-size: 13px;
}

.header-action {
  flex: 0 0 50px;
  display: flex;
  justify-content: center;
  align-items: center;
  height: 34px;
}

.header-actions {
  flex: 0 0 32px;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  height: 34px;
}

.error-message {
  position: absolute;
  top: 100%;
  left: 0;
  font-size: 12px;
  color: var(--error-color);
  margin-top: 2px;
}

@media (max-width: 768px) {
  .header-rule-content {
    flex-direction: column;
    gap: 8px;
    align-items: stretch;
  }

  .header-name,
  .header-value {
    flex: 1;
  }

  .header-actions {
    justify-content: flex-end;
  }
}

/* 亲和性规则样式 */
.affinity-rules-items {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.affinity-rule-card {
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 12px;
  background-color: var(--bg-secondary);
}

.affinity-rule-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border-color);
}

.affinity-rule-title {
  font-weight: 600;
  font-size: 13px;
  color: var(--text-primary);
}

.affinity-rule-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.affinity-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.affinity-label {
  flex: 0 0 100px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary);
  text-align: right;
  white-space: nowrap;
}

.affinity-row .n-input,
.affinity-row .n-select,
.affinity-row .n-input-number {
  flex: 1;
}

@media (max-width: 768px) {
  .affinity-row {
    flex-direction: column;
    align-items: stretch;
    gap: 4px;
  }

  .affinity-label {
    text-align: left;
    flex: auto;
  }
}
</style>
