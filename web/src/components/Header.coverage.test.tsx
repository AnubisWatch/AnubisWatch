import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Header } from './Header'

const mocks = vi.hoisted(() => ({ theme: 'system', effective: 'light', setTheme: vi.fn(), applyTheme: vi.fn(), logout: vi.fn(), navigate: vi.fn(), user: null as null | { name?: string; email?: string } }))
vi.mock('../api/hooks', () => ({ useAuth: () => ({ user: mocks.user, logout: mocks.logout }) }))
vi.mock('../stores/themeStore', () => ({ useThemeStore: () => ({ theme: mocks.theme, setTheme: mocks.setTheme }), applyTheme: mocks.applyTheme, getEffectiveTheme: () => mocks.effective }))
vi.mock('react-router-dom', async (original) => ({ ...(await original()), useNavigate: () => mocks.navigate }))
vi.mock('./WorkspaceSwitcher', () => ({ WorkspaceSwitcher: () => null }))

const mount = () => render(<MemoryRouter><Header /></MemoryRouter>)

describe('Header edge behavior', () => {
  afterEach(() => { mocks.theme = 'system'; mocks.effective = 'light'; mocks.user = null; vi.clearAllMocks(); vi.restoreAllMocks() })

  it('uses fallback identity and toggles light to dark', () => {
    mount()
    expect(screen.getByText('High Priest')).toBeInTheDocument()
    expect(screen.getByText('priest@anubis.watch')).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('Switch to dark mode'))
    expect(mocks.setTheme).toHaveBeenCalledWith('dark')
    expect(mocks.applyTheme).toHaveBeenCalledWith('dark')
  })

  it('updates sticky styling on scroll', () => {
    const view = mount()
    Object.defineProperty(window, 'scrollY', { configurable: true, value: 20 })
    fireEvent.scroll(window)
    expect(view.container.querySelector('header')).toHaveClass('bg-gray-950/90')
    view.unmount()
  })

  it('tracks system media changes and cleans up', () => {
    const listeners = new Map<string, EventListener>()
    const media = { addEventListener: vi.fn((n: string, cb: EventListener) => listeners.set(n, cb)), removeEventListener: vi.fn() }
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn(() => media) })
    const view = mount()
    act(() => listeners.get('change')?.(new Event('change')))
    view.unmount()
    expect(media.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('skips media subscription for explicit theme or missing API', () => {
    mocks.theme = 'dark'
    mount().unmount()
    mocks.theme = 'system'
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: undefined })
    expect(() => mount().unmount()).not.toThrow()
  })

  it('awaits logout before navigation', async () => {
    let finish!: () => void
    mocks.logout.mockReturnValue(new Promise<void>((resolve) => { finish = resolve }))
    mount()
    fireEvent.click(screen.getByLabelText('Log out'))
    expect(mocks.navigate).not.toHaveBeenCalled()
    await act(async () => finish())
    expect(mocks.navigate).toHaveBeenCalledWith('/login')
  })
})
