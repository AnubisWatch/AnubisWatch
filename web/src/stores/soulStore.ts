import { create } from 'zustand'
import { api } from '../api/client'
import type { Soul, Judgment, ApiResponse } from '../api/client'

interface SoulStore {
  souls: Soul[]
  pagination: { total: number; has_more: boolean } | null
  loading: boolean
  error: string | null
  initialChecks: Record<string, 'running' | 'failed'>
  fetchSouls: () => Promise<void>
  createSoul: (soul: Omit<Soul, 'id' | 'created_at' | 'updated_at'>) => Promise<Soul | null>
  retryInitialCheck: (id: string) => Promise<Judgment | null>
  updateSoul: (id: string, soul: Partial<Soul>) => Promise<Soul | null>
  deleteSoul: (id: string) => Promise<void>
}

function statusFromJudgment(status: Judgment['status']): Soul['status'] {
  switch (status) {
    case 'passed':
      return 'healthy'
    case 'failed':
      return 'unhealthy'
    default:
      return 'unknown'
  }
}

function mergeJudgmentStatus(soul: Soul, judgment: Judgment): Soul {
  return {
    ...soul,
    status: statusFromJudgment(judgment.status),
    latency: judgment.latency,
    last_check: judgment.timestamp,
  }
}

function removeInitialCheck(
  initialChecks: Record<string, 'running' | 'failed'>,
  soulID: string
): Record<string, 'running' | 'failed'> {
  return Object.fromEntries(Object.entries(initialChecks).filter(([id]) => id !== soulID))
}

export const useSoulStore = create<SoulStore>((set, get) => ({
  souls: [],
  pagination: null,
  loading: false,
  error: null,
  initialChecks: {},

  fetchSouls: async () => {
    set({ loading: true, error: null })
    try {
      const result = await api.get<ApiResponse<Soul[]>>('/souls')
      if (result) {
        set({ souls: result.data, pagination: result.pagination ?? null, loading: false })
      }
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
    }
  },

  createSoul: async (soul) => {
    set({ loading: true, error: null })
    try {
      const result = await api.post<Soul>('/souls', soul)
      if (result) {
        set((state) => ({ souls: [...state.souls, result], loading: false }))

        if (result.enabled) {
          void get().retryInitialCheck(result.id)
        }
      }
      return result ?? null
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
      return null
    }
  },

  retryInitialCheck: async (id) => {
    set((state) => ({
      initialChecks: { ...state.initialChecks, [id]: 'running' },
    }))

    try {
      const judgment = await api.post<Judgment>(`/souls/${id}/check`)
      set((state) => ({
        souls: state.souls.map((s) => (s.id === id ? mergeJudgmentStatus(s, judgment) : s)),
        initialChecks: removeInitialCheck(state.initialChecks, id),
      }))
      return judgment
    } catch {
      set((state) => ({
        initialChecks: { ...state.initialChecks, [id]: 'failed' },
      }))
      return null
    }
  },

  updateSoul: async (id, soul) => {
    set({ loading: true, error: null })
    try {
      const result = await api.put<Soul>(`/souls/${id}`, soul)
      if (result) {
        set((state) => ({
          souls: state.souls.map((s) => (s.id === id ? result : s)),
          loading: false,
        }))
      }
      return result ?? null
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
      return null
    }
  },

  deleteSoul: async (id) => {
    set({ loading: true, error: null })
    try {
      await api.delete(`/souls/${id}`)
      set((state) => ({
        souls: state.souls.filter((s) => s.id !== id),
        loading: false,
      }))
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
    }
  },
}))
