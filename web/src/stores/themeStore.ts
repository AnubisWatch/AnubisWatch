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

  if (theme === 'system') {
    if (systemDark) {
      root.classList.add('dark')
    } else {
      root.classList.add('light')
    }
  } else {
    root.classList.add(theme)
  }
}
