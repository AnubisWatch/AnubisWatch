import { create } from 'zustand'

export interface Judgment {
  id: string
  soul_id: string
  soul_name?: string
  status: 'passed' | 'failed' | 'unknown'
  latency_ms: number
  timestamp: string
  region: string
  jackal_id: string
  purity_score: number
  error?: string
  metadata?: Record<string, unknown>
}

interface JudgmentStore {
  judgments: Judgment[]
  loading: boolean
  error: string | null
  fetchJudgments: (soulId?: string, limit?: number) => Promise<void>
  fetchJudgmentHistory: (soulId: string, start: Date, end: Date) => Promise<void>
  addJudgment: (judgment: Judgment) => void
}

export const useJudgmentStore = create<JudgmentStore>((set) => ({
  judgments: [],
  loading: false,
  error: null,

  fetchJudgments: async (soulId?: string, limit = 50) => {
    set({ loading: true, error: null })
    try {
      const url = soulId
        ? `/api/v1/souls/${soulId}/judgments?limit=${limit}`
        : `/api/v1/judgments?limit=${limit}`
      const response = await fetch(url)
      if (!response.ok) throw new Error('Failed to fetch judgments')
      const judgments = await response.json()
      set({ judgments, loading: false })
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
    }
  },

  fetchJudgmentHistory: async (soulId: string, start: Date, end: Date) => {
    set({ loading: true, error: null })
    try {
      const response = await fetch(
        `/api/v1/souls/${soulId}/judgments/history?start=${start.toISOString()}&end=${end.toISOString()}`
      )
      if (!response.ok) throw new Error('Failed to fetch judgment history')
      const judgments = await response.json()
      set({ judgments, loading: false })
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
    }
  },

  addJudgment: (judgment) => {
    set((state) => ({
      judgments: [judgment, ...state.judgments.slice(0, 99)],
    }))
  },
}))
