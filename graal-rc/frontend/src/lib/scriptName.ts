// Client-side guard for stale or malformed native script-list entries. The
// backend applies the same rule before any server request; this second check
// keeps a transient cached item from becoming a blank/garbled table row.
export function isUsableScriptName(name: string | null | undefined): name is string {
  const value = name?.trim() ?? ""
  if (!value) return false
  let placeholderOnly = true
  for (const character of value) {
    const code = character.codePointAt(0) ?? 0
    if (code === 0xfffd || code <= 0x1f || (code >= 0x7f && code <= 0x9f) || (code >= 0x200b && code <= 0x200f) || (code >= 0x202a && code <= 0x202e) || (code >= 0x2060 && code <= 0x2064)) {
      return false
    }
    if (![0x25a0, 0x25a1, 0x25aa, 0x25ab, 0x25fb, 0x25fc].includes(code)) placeholderOnly = false
  }
  return !placeholderOnly
}
