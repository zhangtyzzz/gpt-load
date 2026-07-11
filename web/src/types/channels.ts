export type ChannelTypeId = string;

export type GenericHttpAuthPlacement = "header";
export type GenericHttpValidationMethod = "GET" | "HEAD" | "POST";
export type GenericHttpStreamMode = "never" | "auto" | "always";

export interface GenericHttpAuthConfig {
  [key: string]: unknown;
  placement: GenericHttpAuthPlacement;
  name: string;
  prefix: string;
}

export interface GenericHttpValidationConfig {
  [key: string]: unknown;
  enabled: boolean;
  base_url: string;
  method: GenericHttpValidationMethod | "";
  path: string;
  headers: Record<string, string>;
  body: unknown | null;
  valid_statuses: number[];
  invalid_statuses: number[];
}

export interface GenericHttpRetryConfig {
  [key: string]: unknown;
  safe_methods: string[];
  failover_statuses: number[];
}

export interface GenericHttpChannelConfig {
  [key: string]: unknown;
  version: 1;
  preset_id: string;
  auth: GenericHttpAuthConfig;
  validation: GenericHttpValidationConfig;
  stream_mode: GenericHttpStreamMode;
  retry: GenericHttpRetryConfig;
  max_request_body_bytes: number;
  max_error_body_bytes: number;
}

export type ChannelConfig = GenericHttpChannelConfig | Record<string, unknown>;

export interface ChannelPresetDescriptor {
  id: string;
  channel_type: ChannelTypeId;
  display_name: string;
  description: string;
  upstreams: Array<{ url: string; weight: number }>;
  integration_kind?: string;
  suggested_path: string;
  channel_config: GenericHttpChannelConfig;
}

export interface ChannelCapabilities {
  test_model: "required" | "optional" | "hidden";
  validation_endpoint: boolean;
  model_redirect: boolean;
  param_overrides: boolean;
  header_rules: boolean;
  affinity: boolean;
  aggregate: boolean;
}

export interface ChannelDefaults {
  upstream_url: string;
  test_model: string;
  validation_endpoint: string;
}

export interface ChannelDescriptor {
  id: ChannelTypeId;
  display_name: string;
  description: string;
  order: number;
  capabilities: ChannelCapabilities;
  defaults: ChannelDefaults;
  presets: ChannelPresetDescriptor[];
}

export interface ChannelCatalog {
  schema_version: 1;
  default_channel_type: ChannelTypeId;
  items: ChannelDescriptor[];
}
