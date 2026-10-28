import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, type CustomDashboard } from './client'

// An older server sent widgets: null for a dashboard saved without widgets,
// and the dashboard pages crashed iterating it.
describe('custom dashboard widgets', () => {
  afterEach(() => vi.restoreAllMocks())

  const legacy = { id: 'd1', name: 'Legacy', workspace_id: 'default', widgets: null, refresh_sec: 0, created_at: '', updated_at: '' }

  it('reads widgets: null as an empty array', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(legacy) })
    const dashboard = await api.get<CustomDashboard>('/dashboards/d1')
    expect(dashboard.widgets).toEqual([])
  })

  it('reads widgets: null as an empty array in a list', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve([legacy]) })
    const dashboards = await api.get<CustomDashboard[]>('/dashboards')
    expect(dashboards[0].widgets).toEqual([])
  })
})
