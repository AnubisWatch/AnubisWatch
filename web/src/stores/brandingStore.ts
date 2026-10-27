import { create } from 'zustand'
import { api, type DashboardBranding } from '../api/client'
import { useThemeStore } from './themeStore'

export const DEFAULT_BRANDING: DashboardBranding = { title: 'AnubisWatch', logo: '', theme: 'auto' }
export const DEFAULT_LOGO = '/jackal-logo.svg'

// The theme store persists under this key; its absence means the viewer has
// never picked a theme, so the operator's default applies.
const THEME_STORAGE_KEY = 'anubis-theme'

interface BrandingStore {
  branding: DashboardBranding
  loaded: boolean
  load: () => Promise<void>
}

let pending: Promise<void> | null = null

/** dashboard.branding from GET /api/v1/branding (public); defaults until it loads or if it fails. */
export const useBrandingStore = create<BrandingStore>()((set) => ({
  branding: DEFAULT_BRANDING,
  loaded: false,
  load: () => {
    if (!pending) {
      pending = api
        .get<DashboardBranding>('/branding')
        .then((branding) => {
          set({ branding: { ...DEFAULT_BRANDING, ...branding }, loaded: true })
          applyDefaultTheme(branding.theme)
          if (branding.title && branding.title !== DEFAULT_BRANDING.title) {
            document.title = branding.title
          }
        })
        .catch(() => {
          set({ loaded: true })
        })
    }
    return pending
  },
}))

/** Applies the operator's default theme for a viewer who never chose one. */
function applyDefaultTheme(theme: DashboardBranding['theme']) {
  let chosen: boolean
  try {
    chosen = window.localStorage.getItem(THEME_STORAGE_KEY) !== null
  } catch {
    chosen = true // storage unavailable: leave the built-in default
  }
  if (chosen) return
  useThemeStore.getState().setTheme(theme === 'auto' ? 'system' : theme)
}

/** Test hook: forget the cached request. */
export function resetBrandingForTests() {
  pending = null
  useBrandingStore.setState({ branding: DEFAULT_BRANDING, loaded: false })
}
