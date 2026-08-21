import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import App from './App'

const user = {
  id: 'user-1',
  email: 'admin@anubis.watch',
  name: 'Admin',
  role: 'admin',
  workspace: 'default',
}

const soul = {
  id: 'soul-1',
  name: 'Route Test Soul',
  type: 'http',
  target: 'https://api.example.com/health',
  enabled: true,
  weight: '30s',
  timeout: '5s',
  status: 'alive',
  region: 'default',
  created_at: '2026-05-03T00:00:00Z',
  updated_at: '2026-05-03T00:00:00Z',
}

const judgment = {
  id: 'judgment-1',
  soul_id: soul.id,
  soul_name: soul.name,
  status: 'alive',
  duration: '125ms',
  timestamp: '2026-05-03T00:00:00Z',
  region: 'default',
  purity: 100,
}

const dashboard = {
  id: 'dashboard-1',
  name: 'Route Test Dashboard',
  description: 'Dashboard route fixture',
  refresh_sec: 0,
  widgets: [],
  created_at: '2026-05-03T00:00:00Z',
  updated_at: '2026-05-03T00:00:00Z',
}

class MockWebSocket {
  static OPEN = 1
  readyState = MockWebSocket.OPEN
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null

  constructor() {
    setTimeout(() => this.onopen?.(), 0)
  }

  send() {}
  close() {
    this.onclose?.()
  }
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function apiBody(path: string, method: string) {
  if (path === '/api/v1/auth/me') return user
  if (path === '/api/v1/souls') return { data: [soul], pagination: { total: 1, offset: 0, limit: 20, has_more: false } }
  if (path === `/api/v1/souls/${soul.id}`) return soul
  if (path === `/api/v1/souls/${soul.id}/judgments`) return [judgment]
  if (path === '/api/v1/judgments') return [judgment]
  if (path === '/api/v1/channels') return { data: [], pagination: { total: 0, offset: 0, limit: 20, has_more: false } }
  if (path === '/api/v1/rules') return { data: [], pagination: { total: 0, offset: 0, limit: 20, has_more: false } }
  if (path === '/api/v1/incidents') return []
  if (path === '/api/v1/maintenance') return []
  if (path === '/api/v1/journeys') return []
  if (path === '/api/v1/status-pages') return []
  if (path === '/api/v1/dashboards') return [dashboard]
  if (path === `/api/v1/dashboards/${dashboard.id}`) return dashboard
  if (path === `/api/v1/dashboards/${dashboard.id}/query` && method === 'POST') return { count: 1 }
  if (path === '/api/v1/stats/overview') {
    return {
      souls: { total: 1, healthy: 1, degraded: 0, dead: 0 },
      judgments: { today: 1, failures: 0, avg_latency_ms: 125 },
      alerts: { channels: 0, rules: 0, active_incidents: 0 },
    }
  }
  if (path === '/api/v1/cluster/status') {
    return { is_clustered: false, node_id: 'standalone', state: 'standalone', peer_count: 0, term: 0 }
  }
  if (path === '/api/v1/config') {
    return {
      instance_name: 'AnubisWatch',
      timezone: 'UTC',
      language: 'en',
      theme: 'dark',
      retention_days: 30,
      storage_path: '/var/lib/anubis',
      auth_enabled: true,
      mcp_enabled: false,
      websocket_enabled: true,
      host: '127.0.0.1',
      port: 8080,
      grpc_port: 9090,
      tls_enabled: false,
      auth_type: 'local',
    }
  }
  return {}
}

async function renderRoute(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>
  )
}

describe('App route smoke coverage', () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost')
    return jsonResponse(apiBody(url.pathname, init?.method || 'GET'))
  })

  beforeEach(() => {
    localStorage.setItem('auth_token', 'test-token')
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('WebSocket', MockWebSocket)
    fetchMock.mockClear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  const routes = [
    { path: '/', heading: 'Hall of Judgment' },
    { path: '/souls', heading: 'Essence' },
    { path: `/souls/${soul.id}`, heading: soul.name },
    { path: `/souls/${soul.id}/edit`, heading: 'Edit Soul' },
    { path: '/judgments', heading: 'Weighings' },
    { path: '/alerts', heading: 'Divine Warnings' },
    { path: '/incidents', heading: 'Cries of Chaos' },
    { path: '/maintenance', heading: 'Sacred Rest' },
    { path: '/journeys', heading: 'Voyages' },
    { path: '/cluster', heading: 'Necropolis' },
    { path: '/status-pages', heading: 'Temple Squares' },
    { path: '/dashboards', heading: 'Custom Dashboards' },
    { path: '/dashboards/new', heading: 'New Dashboard' },
    { path: `/dashboards/${dashboard.id}`, heading: dashboard.name },
    { path: '/settings', heading: "Pharaoh's Chamber" },
  ]

  it.each(routes)('renders $path', async ({ path, heading }) => {
    await renderRoute(path)

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: heading, exact: true })).toBeInTheDocument()
    }, { timeout: 5000 })
  })
})
