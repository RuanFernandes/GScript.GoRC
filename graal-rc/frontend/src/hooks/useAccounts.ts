// useAccounts owns the saved-account list (read from the encrypted vault via
// the backend). It loads on mount and exposes refresh/add/remove intents.
// Adding/removing always re-fetches the list from the backend so the UI never
// holds a stale copy. Errors surface as toasts.
import {useCallback, useEffect, useMemo, useState} from "react"
import {toast} from "sonner"

import type {RcService} from "@/services/rcService"
import type {AccountSummary} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

export interface UseAccountsResult {
  accounts: AccountSummary[]
  loading: boolean
  refresh: () => Promise<void>
}

export function useAccounts(service: RcService): UseAccountsResult {
  const {t} = useLanguage()
  const [accounts, setAccounts] = useState<AccountSummary[]>([])
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async (): Promise<void> => {
    setLoading(true)
    try {
      setAccounts((await service.listAccounts()) ?? [])
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      toast.error(t("session.accountsLoadFailed"), {description: message})
    } finally {
      setLoading(false)
    }
  }, [service, t])

  useEffect(() => {
    refresh()
  }, [refresh])

  return useMemo(() => ({accounts, loading, refresh}), [accounts, loading, refresh])
}
