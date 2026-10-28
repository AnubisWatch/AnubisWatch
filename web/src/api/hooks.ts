import { useState, useEffect, useCallback, useRef } from 'react'
import { api, ApiResponse, Soul, Judgment, SoulUptime, UptimeRange, AlertChannel, AlertRule, Incident, Stats, ClusterStatus, StatusPage, User, CustomDashboard } from './client'
import { fetchAllPages } from './pagination'
import { useRealtimeRefresh } from '../hooks/useRealtimeRefresh'
import {
  AUTH_SESSION_CHANGED_EVENT,
  dispatchAuthSessionChanged,
  type AuthSessionChange,
} from './authEvents'
import type { components, paths } from './schema.generated'

/** GET /souls/:id/judgments, with the judgments normalised for the UI. */
type SoulJudgmentsPage = Omit<
  paths['/api/v1/souls/{id}/judgments']['get']['responses'][200]['content']['application/json'],
  'data'
> & { data: Judgment[] }

// Generic hook for API calls
function useApi<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = []
) {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const mountedRef = useRef(true)
  const requestSequence = useRef(0)

  useEffect(() => {
    const sequenceRef = requestSequence
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      ++sequenceRef.current
    }
  }, [])

  const fetchData = useCallback(async () => {
    const sequence = ++requestSequence.current
    setLoading(true)
    setError(null)

    try {
      const result = await fetcher()
      if (mountedRef.current && sequence === requestSequence.current) {
        setData(result)
      }
      return result
    } catch (err) {
      if (mountedRef.current && sequence === requestSequence.current) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      }
      throw err
    } finally {
      if (mountedRef.current && sequence === requestSequence.current) {
        setLoading(false)
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(() => {
    fetchData().catch(() => {
      // Error state is set by fetchData.
    })
  }, [fetchData])

  return { data, loading, error, refetch: fetchData }
}

// Souls API hooks
export function useSouls() {
  const { data, loading, error, refetch: refetchSouls } = useApi<ApiResponse<Soul[]>>(
    () => fetchAllPages<Soul>('/souls')
  )

  // Keep the list live: refresh on soul lifecycle and judgment events. useApi
  // owns the request sequence, so a slow response resolving after a newer one
  // can no longer overwrite fresher state with a stale list.
  useRealtimeRefresh(['soul_update', 'judgment', 'status'], refetchSouls)

  // The mutation paths await a refresh. A failed refresh is already surfaced
  // through the hook's error state, so swallow it here rather than letting
  // create/update/delete reject after the mutation itself succeeded.
  const refreshSouls = useCallback(async () => {
    await refetchSouls().catch(() => {})
  }, [refetchSouls])

  const createSoul = async (soul: Omit<Soul, 'id'>) => {
    const result = await api.post<Soul>('/souls', soul)
    await refreshSouls()
    return result
  }

  const updateSoul = async (id: string, soul: Partial<Soul>) => {
    const existing = data?.data.find((s) => s.id === id)
    const payload = existing ? { ...existing, ...soul, id } : soul
    const result = await api.put<Soul>(`/souls/${id}`, payload)
    await refreshSouls()
    return result
  }

  const deleteSoul = async (id: string) => {
    await api.delete(`/souls/${id}`)
    await refreshSouls()
  }

  const forceCheck = async (id: string) => {
    return api.post<Judgment>(`/souls/${id}/check`)
  }

  return {
    souls: data?.data || [],
    pagination: data?.pagination,
    loading,
    error,
    refetch: refreshSouls,
    createSoul,
    updateSoul,
    deleteSoul,
    forceCheck,
  }
}

export function useSoul(id: string | undefined) {
  const { data, loading, error, refetch } = useApi<Soul>(
    () => api.get<Soul>(`/souls/${id}`),
    [id]
  )

  const updateSoul = async (soul: Partial<Soul>) => {
    if (!id) return
    const payload = { ...soul, id }
    const result = await api.put<Soul>(`/souls/${id}`, payload)
    refetch()
    return result
  }

  const deleteSoul = async () => {
    if (!id) return
    await api.delete(`/souls/${id}`)
  }

  const forceCheck = async () => {
    if (!id) return
    return api.post<Judgment>(`/souls/${id}/check`)
  }

  return {
    soul: data,
    loading,
    error,
    refetch,
    updateSoul,
    deleteSoul,
    forceCheck,
  }
}

export function useSoulJudgments(soulId: string | undefined) {
  return useApi<Judgment[]>(
    async () => {
      const resp = await api.get<SoulJudgmentsPage>(`/souls/${soulId}/judgments`)
      return resp.data
    },
    [soulId]
  )
}

export function useSoulUptime(soulId: string | undefined, range: UptimeRange = '7d') {
  return useApi<SoulUptime>(
    async () => {
      // Buckets align to the browser's time zone; getTimezoneOffset is
      // minutes west of UTC, the API wants minutes east.
      const offset = -new Date().getTimezoneOffset()
      return api.get<SoulUptime>(`/souls/${soulId}/uptime?range=${range}&tz_offset_minutes=${offset}`)
    },
    [soulId, range]
  )
}

// Judgments API hooks
export function useJudgments() {
  const result = useApi<Judgment[]>(() => api.get<Judgment[]>('/judgments'))
  // Keep charts and recent-judgment lists live
  useRealtimeRefresh(['judgment'], result.refetch)
  return result
}

// Alerts API hooks
export function useChannels() {
  // useApi owns the request sequence, so a slow response resolving after a
  // newer one can no longer overwrite fresher state with a stale list.
  const { data, loading, error, refetch: refetchChannels } = useApi<ApiResponse<AlertChannel[]>>(
    () => fetchAllPages<AlertChannel>('/channels')
  )

  // The mutation paths await a refresh. A failed refresh is already surfaced
  // through the hook's error state, so swallow it here rather than letting a
  // create/update/delete reject after the mutation itself succeeded.
  const refreshChannels = useCallback(async () => {
    await refetchChannels().catch(() => {})
  }, [refetchChannels])

  const createChannel = async (channel: Omit<AlertChannel, 'id'>) => {
    const result = await api.post<AlertChannel>('/channels', channel)
    await refreshChannels()
    return result
  }

  const updateChannel = async (id: string, channel: Partial<AlertChannel>) => {
    const existing = data?.data.find((c) => c.id === id)
    const payload = existing ? { ...existing, ...channel, id } : channel
    const result = await api.put<AlertChannel>(`/channels/${id}`, payload)
    await refreshChannels()
    return result
  }

  const deleteChannel = async (id: string) => {
    await api.delete(`/channels/${id}`)
    await refreshChannels()
  }

  const testChannel = async (id: string) => {
    return api.post<{ status: string }>(`/channels/${id}/test`)
  }

  return {
    channels: data?.data || [],
    loading,
    error,
    refetch: refreshChannels,
    createChannel,
    updateChannel,
    deleteChannel,
    testChannel,
  }
}

export function useRules() {
  // useApi owns the request sequence, so a slow response resolving after a
  // newer one can no longer overwrite fresher state with a stale list.
  const { data, loading, error, refetch: refetchRules } = useApi<ApiResponse<AlertRule[]>>(
    () => fetchAllPages<AlertRule>('/rules')
  )

  // The mutation paths await a refresh. A failed refresh is already surfaced
  // through the hook's error state, so swallow it here rather than letting a
  // create/update/delete reject after the mutation itself succeeded.
  const refreshRules = useCallback(async () => {
    await refetchRules().catch(() => {})
  }, [refetchRules])

  const createRule = async (rule: Omit<AlertRule, 'id'>) => {
    const result = await api.post<AlertRule>('/rules', rule)
    await refreshRules()
    return result
  }

  const updateRule = async (id: string, rule: Partial<AlertRule>) => {
    const existing = data?.data.find((r) => r.id === id)
    const payload = existing ? { ...existing, ...rule, id } : rule
    const result = await api.put<AlertRule>(`/rules/${id}`, payload)
    await refreshRules()
    return result
  }

  const deleteRule = async (id: string) => {
    await api.delete(`/rules/${id}`)
    await refreshRules()
  }

  return {
    rules: data?.data || [],
    loading,
    error,
    refetch: refreshRules,
    createRule,
    updateRule,
    deleteRule,
  }
}

// Stats API hook
export function useStats() {
  const result = useApi<Stats>(() => api.get<Stats>('/stats/overview'))
  // Keep overview numbers live as judgments and soul states stream in
  useRealtimeRefresh(['judgment', 'stats', 'status', 'soul_update'], result.refetch)
  return result
}

// Incidents API hooks
export function useIncidents() {
  // useApi owns the request sequence, so a stale response can no longer
  // overwrite a newer one; it also surfaces a null body as [] for us.
  const { data, loading, error, refetch: fetchIncidents } = useApi<Incident[]>(
    () => api.get<Incident[] | null>('/incidents').then((r) => r ?? [])
  )

  // Keep incident list live: refresh when alerts fire or incidents change
  useRealtimeRefresh(['incident', 'alert'], fetchIncidents)

  const acknowledgeIncident = async (id: string) => {
    await api.post(`/incidents/${id}/acknowledge`)
    await fetchIncidents().catch(() => {})
  }

  const resolveIncident = async (id: string) => {
    await api.post(`/incidents/${id}/resolve`)
    await fetchIncidents().catch(() => {})
  }

  return {
    incidents: data || [],
    loading,
    error,
    refetch: fetchIncidents,
    acknowledgeIncident,
    resolveIncident,
  }
}

// Cluster API hook
export function useClusterStatus() {
  return useApi<ClusterStatus>(() => api.get<ClusterStatus>('/cluster/status'))
}

/** GET /cluster/peers item. */
export type ClusterPeer = components['schemas']['Peer']

export function useClusterPeers() {
  return useApi<ClusterPeer[]>(() => api.get<ClusterPeer[]>('/cluster/peers'))
}

// Status Pages API hooks
export function useStatusPages() {
  // useApi owns the request sequence, so a slow response resolving after a
  // newer one can no longer overwrite fresher state with a stale list.
  const { data, loading, error, refetch: refetchPages } = useApi<StatusPage[]>(
    () => api.get<StatusPage[]>('/status-pages')
  )

  // The mutation paths await a refresh. A failed refresh is already surfaced
  // through the hook's error state, so swallow it here rather than letting a
  // create/update/delete reject after the mutation itself succeeded.
  const refreshPages = useCallback(async () => {
    await refetchPages().catch(() => {})
  }, [refetchPages])

  const createPage = async (page: Omit<StatusPage, 'id' | 'subscriber_count'>) => {
    const result = await api.post<StatusPage>('/status-pages', page)
    await refreshPages()
    return result
  }

  const updatePage = async (id: string, page: Partial<StatusPage>) => {
    const result = await api.put<StatusPage>(`/status-pages/${id}`, page)
    await refreshPages()
    return result
  }

  const deletePage = async (id: string) => {
    await api.delete(`/status-pages/${id}`)
    await refreshPages()
  }

  return {
    pages: data || [],
    loading,
    error,
    refetch: refreshPages,
    createPage,
    updatePage,
    deletePage,
  }
}

// Dashboards API hooks
export function useDashboards() {
  // useApi owns the request sequence, so a slow response resolving after a
  // newer one can no longer overwrite fresher state with a stale list.
  const { data, loading, error, refetch: refetchDashboards } = useApi<CustomDashboard[]>(
    () => api.get<CustomDashboard[]>('/dashboards')
  )

  // The mutation paths await a refresh. A failed refresh is already surfaced
  // through the hook's error state, so swallow it here rather than letting a
  // create/update/delete reject after the mutation itself succeeded.
  const refreshDashboards = useCallback(async () => {
    await refetchDashboards().catch(() => {})
  }, [refetchDashboards])

  const createDashboard = async (dashboard: Omit<CustomDashboard, 'id'>) => {
    const result = await api.post<CustomDashboard>('/dashboards', dashboard)
    await refreshDashboards()
    return result
  }

  const updateDashboard = async (id: string, dashboard: Partial<CustomDashboard>) => {
    const result = await api.put<CustomDashboard>(`/dashboards/${id}`, dashboard)
    await refreshDashboards()
    return result
  }

  const deleteDashboard = async (id: string) => {
    await api.delete(`/dashboards/${id}`)
    await refreshDashboards()
  }

  return {
    dashboards: data || [],
    loading,
    error,
    refetch: refreshDashboards,
    createDashboard,
    updateDashboard,
    deleteDashboard,
  }
}

// App, Header and Sidebar each mount useAuth. Concurrent session checks share
// one /auth/me request, and a result stays valid for a few seconds so the
// components that mount right after the first check reuse it: a page load
// then costs one request, not two (the per-IP budget counts every one).
// A session event (login, logout, 401) drops the cached result, and so does
// the last consumer unmounting.
const CURRENT_USER_TTL_MS = 5000
let mountedAuthConsumers = 0
let currentUserRequest: Promise<User> | null = null
let recentUser: { user: User; at: number } | null = null

function fetchCurrentUser(): Promise<User> {
  if (recentUser && Date.now() - recentUser.at < CURRENT_USER_TTL_MS) {
    return Promise.resolve(recentUser.user)
  }
  if (!currentUserRequest) {
    currentUserRequest = api
      .get<User>('/auth/me')
      .then((user) => {
        recentUser = { user, at: Date.now() }
        return user
      })
      .finally(() => {
        currentUserRequest = null
      })
  }
  return currentUserRequest
}

// Auth API hooks
export function useAuth() {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const syncSequence = useRef(0)

  const syncSession = useCallback(async () => {
    const sequence = ++syncSequence.current
    setLoading(true)
    try {
      const currentUser = await fetchCurrentUser()
      if (sequence === syncSequence.current) {
        setUser(currentUser)
        dispatchAuthSessionChanged({ state: 'authenticated', user: currentUser })
      }
      return currentUser
    } catch (error) {
      // A cached identity is only presentation data; authorization always
      // comes from a successful server-side session validation.
      if (sequence === syncSequence.current) {
        setUser(null)
        dispatchAuthSessionChanged({ state: 'anonymous' })
      }
      throw error
    }
  }, [])

  useEffect(() => {
    const sequenceRef = syncSequence
    const handleSessionChange = (event: Event) => {
      const change = (event as CustomEvent<AuthSessionChange>).detail
      if (change?.state !== 'authenticated') recentUser = null
      if (change?.state === 'anonymous') {
        ++syncSequence.current
        setUser(null)
        setLoading(false)
        return
      }
      if (change?.state === 'authenticated') {
        ++syncSequence.current
        setUser(change.user)
        setLoading(false)
        return
      }
      syncSession().catch(() => {
        // syncSession clears any identity that the server did not validate.
      })
    }

    window.addEventListener(AUTH_SESSION_CHANGED_EVENT, handleSessionChange)
    mountedAuthConsumers++
    syncSession().catch(() => {
      // Initial anonymous and unavailable-server states are both unauthorized.
    })
    return () => {
      window.removeEventListener(AUTH_SESSION_CHANGED_EVENT, handleSessionChange)
      if (--mountedAuthConsumers <= 0) recentUser = null
      ++sequenceRef.current
    }
  }, [syncSession])

  const login = async (email: string, password: string) => {
    const result = await api.post('/auth/login', { email, password })
    dispatchAuthSessionChanged({ state: 'resync' })
    return result
  }

  const logout = async () => {
    // Propagate first so every mounted auth consumer and the socket stop
    // authorizing immediately, even if the server request is slow or fails.
    dispatchAuthSessionChanged({ state: 'anonymous' })
    api.clearToken(false)
    await api.post('/auth/logout')
  }

  return { user, loading, login, logout, isAuthenticated: !!user }
}
