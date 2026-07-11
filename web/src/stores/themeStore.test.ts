import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyTheme, getEffectiveTheme, useThemeStore } from './themeStore'

describe('themeStore', () => {
  const originalMatchMedia = window.matchMedia

  afterEach(() => {
    document.documentElement.classList.remove('dark', 'light')
    document.documentElement.style.colorScheme = ''
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      writable: true,
      value: originalMatchMedia,
    })
    vi.restoreAllMocks()
  })

  it('applies explicit light and dark themes to the document root', () => {
    applyTheme('light')
    expect(document.documentElement.classList.contains('light')).toBe(true)
    expect(document.documentElement.style.colorScheme).toBe('light')

    applyTheme('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.documentElement.classList.contains('light')).toBe(false)
    expect(document.documentElement.style.colorScheme).toBe('dark')
  })

  it('updates the persisted theme store', () => {
    useThemeStore.getState().setTheme('system')
    expect(useThemeStore.getState().theme).toBe('system')
  })

  it('uses dark when system media queries are unavailable', () => {
    Object.defineProperty(window, 'matchMedia', { configurable: true, writable: true, value: undefined })
    expect(getEffectiveTheme('system')).toBe('dark')
  })

  it('resolves both system media preferences', () => {
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      writable: true,
      value: vi.fn().mockReturnValue({
        matches: false,
        media: '(prefers-color-scheme: dark)',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    })

    expect(getEffectiveTheme('system')).toBe('light')
    applyTheme('system')
    expect(document.documentElement.classList.contains('light')).toBe(true)

    vi.mocked(window.matchMedia).mockReturnValueOnce({
      matches: true,
      media: '(prefers-color-scheme: dark)',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })
    expect(getEffectiveTheme('system')).toBe('dark')
  })
})
