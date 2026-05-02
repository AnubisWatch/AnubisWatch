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

export const useSoulStore = create<SoulStore>((set) => ({
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
          set((state) => ({
            initialChecks: { ...state.initialChecks, [result.id]: 'running' },
          }))

          void api.post<Judgment>(`/souls/${result.id}/check`).then((judgment) => {
            set((state) => ({
              souls: state.souls.map((s) => (s.id === result.id ? mergeJudgmentStatus(s, judgment) : s)),
              initialChecks: Object.fromEntries(
                Object.entries(state.initialChecks).filter(([id]) => id !== result.id)
              ),
            }))
          }).catch(() => {
            set((state) => ({
              initialChecks: { ...state.initialChecks, [result.id]: 'failed' },
            }))
          })
        }
      }
      return result ?? null
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
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
