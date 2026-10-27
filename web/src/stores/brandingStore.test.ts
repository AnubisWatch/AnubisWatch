import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const get = vi.fn()
vi.mock('../api/client', () => ({ api: { get: (...args: unknown[]) => get(...args) } }))

import { DEFAULT_BRANDING, resetBrandingForTests, useBrandingStore } from './brandingStore'
import { useThemeStore } from './themeStore'

describe('brandingStore', () => {
  const originalTitle = document.title

  beforeEach(() => {
    resetBrandingForTests()
    window.localStorage.clear()
    useThemeStore.setState({ theme: 'dark' })
    window.localStorage.clear()
    get.mockReset()
  })

  afterEach(() => {
    document.title = originalTitle
  })

  it('loads branding once and applies the title', async () => {
    get.mockResolvedValue({ title: 'Acme Status', logo: 'https://cdn.example.com/a.svg', theme: 'auto' })
    await Promise.all([useBrandingStore.getState().load(), useBrandingStore.getState().load()])
    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/branding')
    expect(useBrandingStore.getState().branding.title).toBe('Acme Status')
    expect(useBrandingStore.getState().branding.logo).toBe('https://cdn.example.com/a.svg')
    expect(document.title).toBe('Acme Status')
  })

  it('applies the default theme to a viewer who never chose one', async () => {
    get.mockResolvedValue({ title: 'AnubisWatch', logo: '', theme: 'light' })
    await useBrandingStore.getState().load()
    expect(useThemeStore.getState().theme).toBe('light')
  })

  it('maps auto to the system theme', async () => {
    get.mockResolvedValue({ title: 'AnubisWatch', logo: '', theme: 'auto' })
    await useBrandingStore.getState().load()
    expect(useThemeStore.getState().theme).toBe('system')
  })

  it('keeps a theme the viewer chose', async () => {
    useThemeStore.getState().setTheme('dark')
    expect(window.localStorage.getItem('anubis-theme')).not.toBeNull()
    get.mockResolvedValue({ title: 'AnubisWatch', logo: '', theme: 'light' })
    await useBrandingStore.getState().load()
    expect(useThemeStore.getState().theme).toBe('dark')
  })

  it('keeps the defaults when the request fails', async () => {
    get.mockRejectedValue(new Error('offline'))
    await useBrandingStore.getState().load()
    expect(useBrandingStore.getState().branding).toEqual(DEFAULT_BRANDING)
    expect(useBrandingStore.getState().loaded).toBe(true)
    expect(document.title).toBe(originalTitle)
  })
})
