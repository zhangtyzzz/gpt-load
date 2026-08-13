import type { Group, SubGroupInfo } from "@/types/models";

/**
 * Formats a string from camelCase, snake_case, or kebab-case
 * into a more readable format with spaces and capitalized words.
 *
 * @param name The input string.
 * @returns The formatted string.
 *
 * @example
 * formatDisplayName("myGroupName")      // "My Group Name"
 * formatDisplayName("my_group_name")    // "My Group Name"
 * formatDisplayName("my-group-name")    // "My Group Name"
 * formatDisplayName("MyGroup")          // "My Group"
 */
export function formatDisplayName(name: string): string {
  if (!name) {
    return "";
  }

  // Replace snake_case and kebab-case with spaces, and add a space before uppercase letters in camelCase.
  const result = name.replace(/[_-]/g, " ").replace(/([a-z])([A-Z])/g, "$1 $2");

  // Capitalize the first letter of each word.
  return result
    .split(" ")
    .filter(word => word.length > 0)
    .map(word => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

/**
 * Gets the display name for a group or subgroup, falling back to a formatted version of its name.
 * @param item The group or subgroup object.
 * @returns The display name for the group.
 */
export function getGroupDisplayName(item: Group | SubGroupInfo): string {
  if ("group" in item && item.group) {
    const group = item.group as Group;
    return group.display_name || formatDisplayName(group.name);
  }
  const group = item as Group;
  return group.display_name || formatDisplayName(group.name);
}

/**
 * The separator used between the retained head and tail of a masked key.
 * Must stay in sync with utils.KeyMaskMarker in the Go backend: the request-log
 * key identifier is masked server-side, and the two screens are only comparable
 * if both render the same shape.
 */
export const KEY_MASK_MARKER = "****";

/**
 * Masks a long key string for display.
 *
 * Mirrors utils.MaskKeyIdentifier in the Go backend exactly: the first four and
 * last four characters are kept, and a key too short for that window is reduced
 * to the marker alone rather than shown in full.
 *
 * @param key The key string.
 * @returns The masked key.
 */
export function maskKey(key: string): string {
  if (!key) {
    return "";
  }
  if (key.length <= 8) {
    return KEY_MASK_MARKER;
  }
  return `${key.substring(0, 4)}${KEY_MASK_MARKER}${key.substring(key.length - 4)}`;
}

/**
 * Masks a comma-separated string of keys.
 * @param keys The comma-separated keys string.
 * @returns The masked keys string.
 */
export function maskProxyKeys(keys: string): string {
  if (!keys) {
    return "";
  }
  return keys
    .split(",")
    .map(key => maskKey(key.trim()))
    .join(", ");
}
