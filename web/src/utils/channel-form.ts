import type { ChannelCapabilities } from "@/types/channels";
import type { UpstreamInfo } from "@/types/models";

interface ChannelSpecificFields {
  param_overrides: Record<string, unknown>;
  model_redirect_rules: Record<string, string>;
  model_redirect_strict: boolean;
}

export const RESERVED_PROXY_HEADER_NAMES = [
  "connection",
  "content-length",
  "host",
  "keep-alive",
  "last-event-id",
  "proxy-authenticate",
  "proxy-authorization",
  "proxy-connection",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
  "x-gpt-load-key",
] as const;

const reservedProxyHeaders = new Set<string>(RESERVED_PROXY_HEADER_NAMES);

export function isHttpHeaderToken(value: string): boolean {
  return /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(value);
}

export function isReservedProxyHeaderName(value: string): boolean {
  return reservedProxyHeaders.has(value.trim().toLowerCase());
}

export function sanitizeChannelSpecificFields(
  capabilities: Pick<ChannelCapabilities, "model_redirect" | "param_overrides"> | undefined,
  fields: ChannelSpecificFields
): ChannelSpecificFields {
  return {
    param_overrides: capabilities?.param_overrides === false ? {} : fields.param_overrides,
    model_redirect_rules: capabilities?.model_redirect === false ? {} : fields.model_redirect_rules,
    model_redirect_strict:
      capabilities?.model_redirect === false ? false : fields.model_redirect_strict,
  };
}

export function isValidHttpUpstreamUrl(value: string): boolean {
  const candidate = value.trim();
  if (!/^https?:\/\//i.test(candidate)) {
    return false;
  }
  try {
    const url = new URL(candidate);
    return (
      (url.protocol === "http:" || url.protocol === "https:") &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash
    );
  } catch {
    return false;
  }
}

export function areValidHttpUpstreams(upstreams: UpstreamInfo[]): boolean {
  return upstreams.length > 0 && upstreams.every(upstream => isValidHttpUpstreamUrl(upstream.url));
}

function sortJsonValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(sortJsonValue);
  }
  if (typeof value !== "object" || value === null) {
    return value;
  }
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, item]) => [key, sortJsonValue(item)])
  );
}

export function channelConfigsExactlyMatch(left: unknown, right: unknown): boolean {
  return JSON.stringify(sortJsonValue(left)) === JSON.stringify(sortJsonValue(right));
}
