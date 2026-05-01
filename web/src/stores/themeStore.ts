import { create } from 'zustand'
import { persist } from 'zustand/middleware'

type Theme = 'dark' | 'light' | 'system'

interface ThemeStore {
  theme: Theme
  setTheme: (theme: Theme) => void
}

export const useThemeStore = create<ThemeStore>()(
  persist(
    (set) => ({
      theme: 'dark',
      setTheme: (theme) => set({ theme }),
    }),
    {
      name: 'anubis-theme',
    }
  )
)

// Apply theme to document
export function applyTheme(theme: Theme) {
  const root = document.documentElement
  const systemDark = typeof window !== 'undefined' && window.matchMedia
    ? window.matchMedia('(prefers-color-scheme: dark)').matches
    : true // Default to dark if matchMedia is not available

  root.classList.remove('dark', 'light')
  const effectiveTheme = theme === 'system'
    ? (systemDark ? 'dark' : 'light')
    : theme

  root.classList.add(effectiveTheme)
  root.style.colorScheme = effectiveTheme
}
