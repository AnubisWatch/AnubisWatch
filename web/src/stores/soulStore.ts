import { create } from 'zustand'

export interface Soul {
  id: string
  name: string
  type: 'http' | 'tcp' | 'udp' | 'dns' | 'icmp' | 'smtp' | 'grpc' | 'websocket' | 'tls'
  target: string
  weight: number
  timeout: number
  enabled: boolean
  tags: string[]
  workspace_id: string
  created_at: string
  updated_at: string
  http_config?: {
    method: string
    valid_status: number[]
    headers: Record<string, string>
    body: string
  }
  tcp_config?: {
    expect_banner: string
  }
}

interface SoulStore {
  souls: Soul[]
  loading: boolean
  error: string | null
  selectedSoul: Soul | null
  fetchSouls: () => Promise<void>
  createSoul: (soul: Omit<Soul, 'id' | 'created_at' | 'updated_at'>) => Promise<void>
  updateSoul: (id: string, soul: Partial<Soul>) => Promise<void>
  deleteSoul: (id: string) => Promise<void>
  selectSoul: (soul: Soul | null) => void
}

export const useSoulStore = create<SoulStore>((set) => ({
  // Note: 'get' is available for future use when we need to access current state
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  souls: [],
  loading: false,
  error: null,
  selectedSoul: null,

  fetchSouls: async () => {
    set({ loading: true, error: null })
    try {
      const response = await fetch('/api/v1/souls')
      if (!response.ok) throw new Error('Failed to fetch souls')
      const souls = await response.json()
      set({ souls, loading: false })
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
    }
  },

  createSoul: async (soul) => {
    set({ loading: true, error: null })
    try {
      const response = await fetch('/api/v1/souls', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(soul),
      })
      if (!response.ok) throw new Error('Failed to create soul')
      const newSoul = await response.json()
      set((state) => ({ souls: [...state.souls, newSoul], loading: false }))
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
    }
  },

  updateSoul: async (id, soul) => {
    set({ loading: true, error: null })
    try {
      const response = await fetch(`/api/v1/souls/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(soul),
      })
      if (!response.ok) throw new Error('Failed to update soul')
      const updatedSoul = await response.json()
      set((state) => ({
        souls: state.souls.map((s) => (s.id === id ? updatedSoul : s)),
        loading: false,
      }))
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
    }
  },

  deleteSoul: async (id) => {
    set({ loading: true, error: null })
    try {
      const response = await fetch(`/api/v1/souls/${id}`, { method: 'DELETE' })
      if (!response.ok) throw new Error('Failed to delete soul')
      set((state) => ({
        souls: state.souls.filter((s) => s.id !== id),
        loading: false,
      }))
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Unknown error', loading: false })
    }
  },

  selectSoul: (soul) => set({ selectedSoul: soul }),
}))
