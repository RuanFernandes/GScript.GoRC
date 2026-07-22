// scriptCompare sorts script entries symbols-first then alphabetically:
// non-alphanumeric-leading names come first (ordered by their own characters),
// then A-Z (case-insensitive). Used for the weapon/class/NPC lists.
//
// Example order: "!foo", "@bar", "Apple", "banana", "Zebra".
export function scriptCompare(a: string, b: string): number {
  const ax = sortKey(a)
  const bx = sortKey(b)
  if (ax.group !== bx.group) return ax.group - bx.group
  return ax.norm.localeCompare(bx.norm, undefined, {sensitivity: "base"})
}

// sortKey classifies a name: group 0 = symbol-led (starts with non-letter/
// non-digit), group 1 = alphanumeric-led. norm is the lowercased name used for
// the alphabetical tiebreak.
function sortKey(name: string): {group: number; norm: string} {
  const norm = (name ?? "").toLowerCase()
  const first = norm.charAt(0)
  const isAlphaNum = /[a-z0-9]/.test(first)
  return {group: isAlphaNum ? 1 : 0, norm}
}
