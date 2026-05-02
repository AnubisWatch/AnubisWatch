import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyTheme, getEffectiveTheme } from './themeStore'

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

  it('resolves system theme without assuming matchMedia is always available', () => {
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
  })
})
