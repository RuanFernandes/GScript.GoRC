// A status line of the attributes editor text (Male, Weapons Enabled, Spin Attack).
export function isAttrOn(value: string | undefined): boolean {
  return value === "1" || value?.toLowerCase() === "true"
}
