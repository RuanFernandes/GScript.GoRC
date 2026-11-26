// A status line of the attributes editor text (Male, Weapons Enabled, Spin Attack).
export function isAttrOn(value: string | undefined): boolean {
  return value === "1" || value?.toLowerCase() === "true"
}

// The properties to save: what the editor text gave, for the world of the reply.
export function withReplyWorld(saved: string, reply: string): string {
  try {
    const {world} = JSON.parse(reply)
    if (typeof world !== "string" || world === "") return saved
    return JSON.stringify({...JSON.parse(saved), world})
  } catch {
    return saved
  }
}
