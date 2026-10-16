import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './client'

describe('status page round trip', () => {
  afterEach(() => vi.restoreAllMocks())

  // Saving a status page used to reset custom colours to the dark preset.
  it('keeps a custom theme when the page is saved unchanged', async () => {
    const custom = { primary_color: '#ff0000', background_color: '#123456', text_color: '#eeeeee', accent_color: '#00ff00', font_family: 'Inter', custom_css: '.x{}' }
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ id: 'p1', name: 'P', slug: 'p', enabled: true, souls: [], theme: custom, groups: [{ id: 'g1' }], logo_url: '/logo.png' }),
    })
    const page = await api.get<Record<string, unknown>>('/status-pages/p1')
    expect(page.theme).toBe('custom')

    await api.put('/status-pages/p1', page)
    const body = JSON.parse(String((vi.mocked(fetch).mock.calls.at(-1)?.[1] as RequestInit).body))
    expect(body.theme).toEqual(custom)
    expect(body).not.toHaveProperty('theme_config')
    expect(body.groups).toEqual([{ id: 'g1' }])
    expect(body.logo_url).toBe('/logo.png')
  })

  it('applies a preset when the theme name changes', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve({}) })
    await api.put('/status-pages/p1', { slug: 'p', souls: [], theme: 'light', theme_config: { background_color: '#123456' } })
    const body = JSON.parse(String((vi.mocked(fetch).mock.calls.at(-1)?.[1] as RequestInit).body))
    expect(body.theme.background_color).toBe('#ffffff')
  })
})
