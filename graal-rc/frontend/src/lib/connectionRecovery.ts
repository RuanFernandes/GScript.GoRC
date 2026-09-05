export interface ConnectionRecoveryStatus {
  revision: number
  active: boolean
  phase: string
  attempt: number
  maxAttempts: number
  reason: string
}

export const EMPTY_RECOVERY: ConnectionRecoveryStatus = {
  revision: 0, active: false, phase: "", attempt: 0, maxAttempts: 5, reason: "",
}

export function parseRecoveryStatus(value: unknown): ConnectionRecoveryStatus | null {
  if (!value || typeof value !== "object") return null
  const status = value as Record<string, unknown>
  if (!Number.isSafeInteger(status.revision) || Number(status.revision) < 0
    || typeof status.active !== "boolean" || typeof status.phase !== "string"
    || !Number.isSafeInteger(status.attempt) || !Number.isSafeInteger(status.maxAttempts)
    || typeof status.reason !== "string") return null
  return status as unknown as ConnectionRecoveryStatus
}
