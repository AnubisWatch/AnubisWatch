import { render, screen } from '@testing-library/react'
import { MemoryRouter, Outlet } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ auth: { isAuthenticated: true, loading: false }, theme: 'dark', applyTheme: vi.fn() }))
vi.mock('./api/hooks', () => ({ useAuth: () => mocks.auth }))
vi.mock('./stores/themeStore', () => ({ useThemeStore: (pick: (s: { theme: string }) => unknown) => pick({ theme: mocks.theme }), applyTheme: mocks.applyTheme }))
vi.mock('./hooks/useWebSocket', () => ({ WebSocketProvider: ({ children }: { children: React.ReactNode }) => children }))
vi.mock('./components/ErrorBoundary', () => ({ ErrorBoundary: ({ children }: { children: React.ReactNode }) => children }))
vi.mock('./components/Layout', () => ({ Layout: () => <Outlet /> }))
vi.mock('./pages/Dashboard', () => ({ Dashboard: () => <h1>Dashboard</h1> }))
vi.mock('./pages/Souls', () => ({ Souls: () => <h1>Souls</h1> }))
vi.mock('./pages/SoulDetail', () => ({ SoulDetail: () => <h1>SoulDetail</h1> }))
vi.mock('./pages/SoulEdit', () => ({ SoulEdit: () => <h1>SoulEdit</h1> }))
vi.mock('./pages/Judgments', () => ({ Judgments: () => <h1>Judgments</h1> }))
vi.mock('./pages/Alerts', () => ({ Alerts: () => <h1>Alerts</h1> }))
vi.mock('./pages/Journeys', () => ({ Journeys: () => <h1>Journeys</h1> }))
vi.mock('./pages/Cluster', () => ({ Cluster: () => <h1>Cluster</h1> }))
vi.mock('./pages/StatusPages', () => ({ StatusPages: () => <h1>StatusPages</h1> }))
vi.mock('./pages/Dashboards', () => ({ Dashboards: () => <h1>Dashboards</h1> }))
vi.mock('./pages/DashboardDetail', () => ({ DashboardDetail: () => <h1>DashboardDetail</h1> }))
vi.mock('./pages/Incidents', () => ({ Incidents: () => <h1>Incidents</h1> }))
vi.mock('./pages/Maintenance', () => ({ Maintenance: () => <h1>Maintenance</h1> }))
vi.mock('./pages/Settings', () => ({ Settings: () => <h1>Settings</h1> }))
vi.mock('./pages/Login', () => ({ Login: () => <h1>Login</h1> }))
vi.mock('./pages/NotFound', () => ({ NotFound: () => <h1>NotFound</h1> }))
import App from './App'

function mount(path = '/') { return render(<MemoryRouter initialEntries={[path]}><App /></MemoryRouter>) }

describe('App control flow', () => {
  afterEach(() => { mocks.auth = { isAuthenticated: true, loading: false }; mocks.theme = 'dark'; vi.restoreAllMocks() })

  it('shows the protected loading state', () => {
    mocks.auth = { isAuthenticated: false, loading: true }
    mount()
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument()
  })

  it('redirects anonymous users to login', async () => {
    mocks.auth = { isAuthenticated: false, loading: false }
    mount('/souls')
    expect(await screen.findByRole('heading', { name: 'Login' })).toBeInTheDocument()
  })

  it('renders the wildcard route and skip link', async () => {
    mount('/missing')
    expect(await screen.findByRole('heading', { name: 'NotFound' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Skip to main content' })).toHaveAttribute('href', '#main-content')
  })

  it('tracks system theme changes and removes the listener', () => {
    mocks.theme = 'system'
    const callbacks = new Map<string, EventListener>()
    const media = { matches: true, addEventListener: vi.fn((n: string, cb: EventListener) => callbacks.set(n, cb)), removeEventListener: vi.fn() }
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn(() => media) })
    const view = mount()
    callbacks.get('change')?.(new Event('change'))
    expect(mocks.applyTheme).toHaveBeenCalledWith('system')
    view.unmount()
    expect(media.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('does not subscribe for explicit themes or missing matchMedia', () => {
    mount().unmount()
    mocks.theme = 'system'
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: undefined })
    expect(() => mount().unmount()).not.toThrow()
  })
})
