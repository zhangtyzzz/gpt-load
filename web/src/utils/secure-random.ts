const tokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";

export function generateSecureRandomString(length: number): string {
  if (!Number.isSafeInteger(length) || length <= 0) {
    throw new RangeError("length must be a positive integer");
  }
  if (!globalThis.crypto?.getRandomValues) {
    throw new Error("secure random generation is unavailable");
  }

  const bytes = new Uint8Array(length);
  globalThis.crypto.getRandomValues(bytes);
  return Array.from(bytes, byte => tokenAlphabet[byte & 63]).join("");
}
