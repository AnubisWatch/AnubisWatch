import { create } from 'zustand'
import { persist } from 'zustand/middleware'

type Theme = 'dark' | 'light' | 'system'
type EffectiveTheme = 'dark' | 'light'

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

export function getEffectiveTheme(theme: Theme): EffectiveTheme {
  if (theme !== 'system') {
    return theme
  }

  const systemDark = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-color-scheme: dark)').matches
    : true

  return systemDark ? 'dark' : 'light'
}

// Apply theme to document
export function applyTheme(theme: Theme) {
  const root = document.documentElement
  root.classList.remove('dark', 'light')
  const effectiveTheme = getEffectiveTheme(theme)

  root.classList.add(effectiveTheme)
  root.style.colorScheme = effectiveTheme
}
