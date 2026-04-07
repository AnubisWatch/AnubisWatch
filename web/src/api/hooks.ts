import { useState, useEffect, useCallback } from 'react'
import { api, ApiResponse, Soul, Judgment, AlertChannel, AlertRule, Workspace, Stats, ClusterStatus, StatusPage, User } from './client'

// Generic hook for API calls
function useApi<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = []
) {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    const fetch = async () => {
      setLoading(true)
      setError(null)

      try {
        const result = await fetcher()
        if (!cancelled) {
          setData(result)
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Unknown error')
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }

    fetch()

    return () => {
      cancelled = true
    }
  }, deps)

  return { data, loading, error, refetch: () => setData(null) }
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
    const result = await api.put<Soul>(`/souls/${id}`, soul)
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
    const result = await api.put<Soul>(`/souls/${id}`, soul)
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
  return useApi<ApiResponse<Judgment[]>>(() => api.get<ApiResponse<Judgment[]>>('/judgments'))
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
    const result = await api.put<AlertChannel>(`/channels/${id}`, channel)
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
    const result = await api.put<AlertRule>(`/rules/${id}`, rule)
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

// Cluster API hook
export function useClusterStatus() {
  return useApi<ClusterStatus>(() => api.get<ClusterStatus>('/cluster/status'))
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

// Workspaces API hooks
export function useWorkspaces() {
  return useApi<Workspace[]>(() => api.get<Workspace[]>('/workspaces'))
}

// Auth API hooks
export function useAuth() {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const token = localStorage.getItem('auth_token')
    if (token) {
      api.get<User>('/auth/me')
        .then(setUser)
        .catch(() => {
          api.clearToken()
        })
        .finally(() => setLoading(false))
    } else {
      setLoading(false)
    }
  }, [])

  const login = async (email: string, password: string) => {
    const result = await api.post<{ user: User; token: string }>('/auth/login', { email, password })
    api.setToken(result.token)
    setUser(result.user)
    return result
  }

  const logout = async () => {
    await api.post('/auth/logout')
    api.clearToken()
    setUser(null)
  }

  return { user, loading, login, logout, isAuthenticated: !!user }
}
