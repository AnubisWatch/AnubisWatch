import { useState, useEffect, useCallback, useRef } from 'react'
import { api, ApiResponse, Soul, Judgment, AlertChannel, AlertRule, Incident, Stats, ClusterStatus, StatusPage, User, CustomDashboard } from './client'
import {
  AUTH_SESSION_CHANGED_EVENT,
  dispatchAuthSessionChanged,
  type AuthSessionChange,
} from './authEvents'

// Generic hook for API calls
function useApi<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = []
) {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)

    try {
      const result = await fetcher()
      if (mountedRef.current) {
        setData(result)
      }
      return result
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      }
      throw err
    } finally {
      if (mountedRef.current) {
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
  const [data, setData] = useState<ApiResponse<Soul[]> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchSouls = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.get<ApiResponse<Soul[]>>('/souls')
      setData(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchSouls()
  }, [fetchSouls])

  const createSoul = async (soul: Omit<Soul, 'id'>) => {
    const result = await api.post<Soul>('/souls', soul)
    await fetchSouls()
    return result
  }

  const updateSoul = async (id: string, soul: Partial<Soul>) => {
    const existing = data?.data.find((s) => s.id === id)
    const payload = existing ? { ...existing, ...soul, id } : soul
    const result = await api.put<Soul>(`/souls/${id}`, payload)
    await fetchSouls()
    return result
  }

  const deleteSoul = async (id: string) => {
    await api.delete(`/souls/${id}`)
    await fetchSouls()
  }

  const forceCheck = async (id: string) => {
    return api.post<Judgment>(`/souls/${id}/check`)
  }

  return {
    souls: data?.data || [],
    pagination: data?.pagination,
    loading,
    error,
    refetch: fetchSouls,
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
    () => api.get<Judgment[]>(`/souls/${soulId}/judgments`),
    [soulId]
  )
}

// Judgments API hooks
export function useJudgments() {
  return useApi<Judgment[]>(() => api.get<Judgment[]>('/judgments'))
}

// Alerts API hooks
export function useChannels() {
  const [data, setData] = useState<ApiResponse<AlertChannel[]> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchChannels = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.get<ApiResponse<AlertChannel[]>>('/channels')
      setData(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchChannels()
  }, [fetchChannels])

  const createChannel = async (channel: Omit<AlertChannel, 'id'>) => {
    const result = await api.post<AlertChannel>('/channels', channel)
    await fetchChannels()
    return result
  }

  const updateChannel = async (id: string, channel: Partial<AlertChannel>) => {
    const existing = data?.data.find((c) => c.id === id)
    const payload = existing ? { ...existing, ...channel, id } : channel
    const result = await api.put<AlertChannel>(`/channels/${id}`, payload)
    await fetchChannels()
    return result
  }

  const deleteChannel = async (id: string) => {
    await api.delete(`/channels/${id}`)
    await fetchChannels()
  }

  const testChannel = async (id: string) => {
    return api.post<{ status: string }>(`/channels/${id}/test`)
  }

  return {
    channels: data?.data || [],
    loading,
    error,
    refetch: fetchChannels,
    createChannel,
    updateChannel,
    deleteChannel,
    testChannel,
  }
}

export function useRules() {
  const [data, setData] = useState<ApiResponse<AlertRule[]> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchRules = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.get<ApiResponse<AlertRule[]>>('/rules')
      setData(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchRules()
  }, [fetchRules])

  const createRule = async (rule: Omit<AlertRule, 'id'>) => {
    const result = await api.post<AlertRule>('/rules', rule)
    await fetchRules()
    return result
  }

  const updateRule = async (id: string, rule: Partial<AlertRule>) => {
    const existing = data?.data.find((r) => r.id === id)
    const payload = existing ? { ...existing, ...rule, id } : rule
    const result = await api.put<AlertRule>(`/rules/${id}`, payload)
    await fetchRules()
    return result
  }

  const deleteRule = async (id: string) => {
    await api.delete(`/rules/${id}`)
    await fetchRules()
  }

  return {
    rules: data?.data || [],
    loading,
    error,
    refetch: fetchRules,
    createRule,
    updateRule,
    deleteRule,
  }
}

// Stats API hook
export function useStats() {
  return useApi<Stats>(() => api.get<Stats>('/stats/overview'))
}

// Incidents API hooks
export function useIncidents() {
  const [data, setData] = useState<Incident[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchIncidents = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.get<Incident[]>('/incidents')
      setData(result ?? [])
      setError(null)
      return result ?? []
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchIncidents().catch(() => {
      // Error state is set by fetchIncidents.
    })
  }, [fetchIncidents])

  const acknowledgeIncident = async (id: string) => {
    await api.post(`/incidents/${id}/acknowledge`)
    await fetchIncidents()
  }

  const resolveIncident = async (id: string) => {
    await api.post(`/incidents/${id}/resolve`)
    await fetchIncidents()
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

export interface ClusterPeer {
  id: string
  name: string
  address: string
  state: string
  last_contact: string
}

export function useClusterPeers() {
  return useApi<ClusterPeer[]>(() => api.get<ClusterPeer[]>('/cluster/peers'))
}

// Status Pages API hooks
export function useStatusPages() {
  const [data, setData] = useState<StatusPage[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchPages = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.get<StatusPage[]>('/status-pages')
      setData(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchPages()
  }, [fetchPages])

  const createPage = async (page: Omit<StatusPage, 'id'>) => {
    const result = await api.post<StatusPage>('/status-pages', page)
    await fetchPages()
    return result
  }

  const updatePage = async (id: string, page: Partial<StatusPage>) => {
    const result = await api.put<StatusPage>(`/status-pages/${id}`, page)
    await fetchPages()
    return result
  }

  const deletePage = async (id: string) => {
    await api.delete(`/status-pages/${id}`)
    await fetchPages()
  }

  return {
    pages: data || [],
    loading,
    error,
    refetch: fetchPages,
    createPage,
    updatePage,
    deletePage,
  }
}

// Dashboards API hooks
export function useDashboards() {
  const [data, setData] = useState<CustomDashboard[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchDashboards = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.get<CustomDashboard[]>('/dashboards')
      setData(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchDashboards()
  }, [fetchDashboards])

  const createDashboard = async (dashboard: Omit<CustomDashboard, 'id'>) => {
    const result = await api.post<CustomDashboard>('/dashboards', dashboard)
    await fetchDashboards()
    return result
  }

  const updateDashboard = async (id: string, dashboard: Partial<CustomDashboard>) => {
    const result = await api.put<CustomDashboard>(`/dashboards/${id}`, dashboard)
    await fetchDashboards()
    return result
  }

  const deleteDashboard = async (id: string) => {
    await api.delete(`/dashboards/${id}`)
    await fetchDashboards()
  }

  return {
    dashboards: data || [],
    loading,
    error,
    refetch: fetchDashboards,
    createDashboard,
    updateDashboard,
    deleteDashboard,
  }
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
      const currentUser = await api.get<User>('/auth/me')
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
    syncSession().catch(() => {
      // Initial anonymous and unavailable-server states are both unauthorized.
    })
    return () => {
      window.removeEventListener(AUTH_SESSION_CHANGED_EVENT, handleSessionChange)
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
