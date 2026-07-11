import { channelsApi } from "@/api/channels";
import type {
  ChannelCapabilities,
  ChannelCatalog,
  ChannelDefaults,
  ChannelDescriptor,
  ChannelPresetDescriptor,
  GenericHttpAuthConfig,
  GenericHttpChannelConfig,
  GenericHttpStreamMode,
  GenericHttpValidationConfig,
} from "@/types/channels";
import { computed, readonly, ref } from "vue";

const BUILTIN_CHANNEL_ORDER = ["openai", "openai-response", "gemini", "anthropic", "generic-http"];

const LEGACY_DEFAULTS: Record<string, ChannelDefaults> = {
  openai: {
    upstream_url: "https://api.openai.com",
    test_model: "gpt-4.1-nano",
    validation_endpoint: "/v1/chat/completions",
  },
  "openai-response": {
    upstream_url: "https://api.openai.com",
    test_model: "gpt-4.1-nano",
    validation_endpoint: "/v1/responses",
  },
  gemini: {
    upstream_url: "https://generativelanguage.googleapis.com",
    test_model: "gemini-2.0-flash-lite",
    validation_endpoint: "",
  },
  anthropic: {
    upstream_url: "https://api.anthropic.com",
    test_model: "claude-3-haiku-20240307",
    validation_endpoint: "/v1/messages",
  },
  "generic-http": {
    upstream_url: "",
    test_model: "",
    validation_endpoint: "",
  },
};

const DEFAULT_CAPABILITIES: ChannelCapabilities = {
  test_model: "required",
  validation_endpoint: true,
  model_redirect: true,
  param_overrides: true,
  header_rules: true,
  affinity: true,
  aggregate: true,
};

const GENERIC_CAPABILITIES: ChannelCapabilities = {
  test_model: "hidden",
  validation_endpoint: false,
  model_redirect: false,
  param_overrides: false,
  header_rules: true,
  affinity: true,
  aggregate: true,
};

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : {};
}

function asString(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function asNumber(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function asBoolean(value: unknown, fallback: boolean): boolean {
  return typeof value === "boolean" ? value : fallback;
}

function asStringArray(value: unknown, fallback: string[] = []): string[] {
  if (!Array.isArray(value)) {
    return fallback;
  }
  return value.filter((item): item is string => typeof item === "string");
}

function asStatusArray(
  value: unknown,
  fallback: number[],
  allowEmpty = false,
  minimum = 100
): number[] {
  if (!Array.isArray(value)) {
    return fallback;
  }
  const statuses = value.filter(
    (item): item is number => Number.isInteger(item) && item >= minimum && item <= 599
  );
  return statuses.length > 0 || allowEmpty ? [...new Set(statuses)] : fallback;
}

function normalizeStreamMode(value: unknown): GenericHttpStreamMode {
  if (value === "never" || value === "auto" || value === "always") {
    return value;
  }
  return "auto";
}

function normalizeAuth(value: unknown): GenericHttpAuthConfig {
  const auth = asRecord(value);
  const { placement: _legacyPlacement, ...currentAuth } = auth;
  return {
    ...currentAuth,
    placement: "header",
    name: asString(auth.name, "Authorization"),
    prefix: asString(auth.prefix, "Bearer "),
  };
}

function normalizeValidation(value: unknown): GenericHttpValidationConfig {
  const validation = asRecord(value);
  const enabled = asBoolean(validation.enabled, false);
  if (!enabled) {
    return {
      ...validation,
      enabled: false,
      base_url: "",
      method: "",
      path: "",
      headers: {},
      body: null,
      valid_statuses: [],
      invalid_statuses: [],
    };
  }
  const headers = Object.fromEntries(
    Object.entries(asRecord(validation.headers)).filter(
      (entry): entry is [string, string] => typeof entry[1] === "string"
    )
  );
  const rawMethod = asString(validation.method, "GET");
  const method = rawMethod === "HEAD" || rawMethod === "POST" ? rawMethod : "GET";

  return {
    ...validation,
    enabled,
    base_url: asString(validation.base_url),
    method,
    path: asString(validation.path),
    headers,
    body: validation.body ?? null,
    valid_statuses: asStatusArray(validation.valid_statuses, [200]),
    invalid_statuses: asStatusArray(validation.invalid_statuses, [401]),
  };
}

function normalizeMethods(value: unknown, fallback: string[] = []): string[] {
  const methods = asStringArray(value, fallback)
    .map(method => method.trim().toUpperCase())
    .filter(method => /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(method));
  return methods.length > 0 ? [...new Set(methods)] : fallback;
}

export function normalizeGenericHttpConfig(
  value: unknown,
  presetId = "custom"
): GenericHttpChannelConfig {
  const config = asRecord(value);
  const {
    protocol: _legacyProtocol,
    mcp_session_ttl_seconds: _legacyMcpSessionTtl,
    ...currentConfig
  } = config;
  const retry = asRecord(config.retry);
  const { safe_statuses_for_any_method: _legacySafeStatuses, ...currentRetry } = retry;
  const safeMethods = normalizeMethods(retry.safe_methods, ["GET", "HEAD"]);
  const failoverStatuses = asStatusArray(retry.failover_statuses, [], true, 300);

  return {
    ...currentConfig,
    version: 1,
    preset_id: asString(config.preset_id, presetId),
    auth: normalizeAuth(config.auth),
    validation: normalizeValidation(config.validation),
    stream_mode: normalizeStreamMode(config.stream_mode),
    retry: {
      ...currentRetry,
      safe_methods: safeMethods,
      failover_statuses: failoverStatuses,
    },
    max_request_body_bytes: Math.max(
      1,
      Math.min(
        64 * 1024 * 1024,
        Math.round(asNumber(config.max_request_body_bytes, 16 * 1024 * 1024))
      )
    ),
    max_error_body_bytes: Math.max(
      1,
      Math.min(1024 * 1024, Math.round(asNumber(config.max_error_body_bytes, 64 * 1024)))
    ),
  };
}

function normalizeUpstreams(value: unknown): Array<{ url: string; weight: number }> {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .map(item => {
      const upstream = asRecord(item);
      return {
        url: asString(upstream.url),
        weight: Math.max(0, Math.round(asNumber(upstream.weight, 1))),
      };
    })
    .filter(upstream => upstream.url.length > 0);
}

function normalizePreset(value: unknown): ChannelPresetDescriptor | null {
  const raw = asRecord(value);
  const id = asString(raw.id || raw.preset_id);
  const channelType = asString(raw.channel_type, "generic-http");
  if (!id || channelType !== "generic-http") {
    return null;
  }
  const config = normalizeGenericHttpConfig(raw.channel_config || raw.config, id);
  return {
    id,
    channel_type: channelType,
    display_name: asString(raw.display_name || raw.label, id),
    description: asString(raw.description),
    upstreams: normalizeUpstreams(raw.upstreams),
    integration_kind: asString(raw.integration_kind) || undefined,
    suggested_path: asString(raw.suggested_path),
    channel_config: { ...config, preset_id: id },
  };
}

function descriptorFor(id: string, presets: ChannelPresetDescriptor[] = []): ChannelDescriptor {
  const knownOrder = BUILTIN_CHANNEL_ORDER.indexOf(id);
  return {
    id,
    display_name: id === "generic-http" ? "Generic HTTP" : id,
    description: "",
    order: knownOrder === -1 ? 1000 : knownOrder,
    capabilities:
      id === "generic-http"
        ? GENERIC_CAPABILITIES
        : { ...DEFAULT_CAPABILITIES, validation_endpoint: id !== "gemini" },
    defaults: LEGACY_DEFAULTS[id] || { upstream_url: "", test_model: "", validation_endpoint: "" },
    presets,
  };
}

function catalogItems(rawCatalog: unknown): unknown[] {
  if (Array.isArray(rawCatalog)) {
    return rawCatalog;
  }
  const record = asRecord(rawCatalog);
  for (const candidate of [record.items, record.presets, record.channels, record.data]) {
    if (Array.isArray(candidate)) {
      return candidate;
    }
  }
  return [];
}

export function normalizeChannelCatalog(
  rawCatalog: unknown,
  legacyChannelTypes: string[] = []
): ChannelCatalog {
  const rawItems = catalogItems(rawCatalog);
  const presetItems = rawItems.map(normalizePreset).filter(item => item !== null);
  const descriptorItems = rawItems
    .map(item => {
      const raw = asRecord(item);
      const id = asString(raw.channel_type || raw.id || raw.value);
      if (!id || normalizePreset(item)) {
        return null;
      }
      const capabilities = asRecord(raw.capabilities);
      const defaults = asRecord(raw.defaults);
      return {
        ...descriptorFor(id),
        display_name: asString(raw.display_name || raw.label, id),
        description: asString(raw.description),
        order: asNumber(raw.order, descriptorFor(id).order),
        capabilities: {
          test_model:
            capabilities.test_model === "hidden" || capabilities.test_model === "optional"
              ? capabilities.test_model
              : "required",
          validation_endpoint: asBoolean(capabilities.validation_endpoint, id !== "gemini"),
          model_redirect: asBoolean(capabilities.model_redirect, id !== "generic-http"),
          param_overrides: asBoolean(capabilities.param_overrides, id !== "generic-http"),
          header_rules: asBoolean(capabilities.header_rules, true),
          affinity: asBoolean(capabilities.affinity, true),
          aggregate: asBoolean(capabilities.aggregate, true),
        },
        defaults: {
          upstream_url: asString(defaults.upstream_url, descriptorFor(id).defaults.upstream_url),
          test_model: asString(defaults.test_model, descriptorFor(id).defaults.test_model),
          validation_endpoint: asString(
            defaults.validation_endpoint,
            descriptorFor(id).defaults.validation_endpoint
          ),
        },
      } satisfies ChannelDescriptor;
    })
    .filter(item => item !== null);

  const channelIds = new Set<string>(legacyChannelTypes);
  if (legacyChannelTypes.length === 0) {
    ["openai", "openai-response", "gemini", "anthropic"].forEach(id => channelIds.add(id));
  }
  descriptorItems.forEach(item => channelIds.add(item.id));
  presetItems.forEach(item => channelIds.add(item.channel_type));

  const descriptors = new Map<string, ChannelDescriptor>();
  channelIds.forEach(id => descriptors.set(id, descriptorFor(id)));
  descriptorItems.forEach(item => descriptors.set(item.id, item));
  if (presetItems.length > 0) {
    const generic = descriptors.get("generic-http") || descriptorFor("generic-http");
    descriptors.set("generic-http", { ...generic, presets: presetItems });
  }

  return {
    schema_version: 1,
    default_channel_type: descriptors.has("openai") ? "openai" : [...descriptors.keys()][0] || "",
    items: [...descriptors.values()].sort(
      (left, right) =>
        left.order - right.order || left.display_name.localeCompare(right.display_name)
    ),
  };
}

const catalog = ref<ChannelCatalog>(normalizeChannelCatalog([], []));
const loading = ref(false);
const loaded = ref(false);
const error = ref<unknown>(null);
let inFlight: Promise<ChannelCatalog> | null = null;

async function loadCatalog(force = false): Promise<ChannelCatalog> {
  if (loaded.value && !force) {
    return catalog.value;
  }
  if (inFlight && !force) {
    return inFlight;
  }

  loading.value = true;
  error.value = null;
  inFlight = channelsApi
    .getCatalogAndTypes()
    .then(({ catalog: rawCatalog, channelTypes }) => {
      catalog.value = normalizeChannelCatalog(rawCatalog, channelTypes);
      loaded.value = true;
      return catalog.value;
    })
    .catch(reason => {
      error.value = reason;
      throw reason;
    })
    .finally(() => {
      loading.value = false;
      inFlight = null;
    });
  return inFlight;
}

export function useChannelCatalog() {
  return {
    catalog: readonly(catalog),
    channelTypes: computed(() => catalog.value.items),
    loading: readonly(loading),
    loaded: readonly(loaded),
    error: readonly(error),
    loadCatalog,
  };
}

export function resetChannelCatalogForTests() {
  catalog.value = normalizeChannelCatalog([], []);
  loading.value = false;
  loaded.value = false;
  error.value = null;
  inFlight = null;
}
