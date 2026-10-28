import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { StatusPageIncidents } from './StatusPageIncidents'
import type { StatusPage } from '../api/client'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() }))
vi.mock('../api/client', () => ({ api }))

const auth = vi.hoisted(() => ({ role: 'admin' as string | undefined }))
vi.mock('../api/authEvents', () => ({
  AUTH_SESSION_CHANGED_EVENT: 'anubis:auth-session-changed',
  getLastKnownUser: () => (auth.role ? { id: 'me', role: auth.role } : null),
}))

const page: StatusPage = { id: 'page-1', name: 'Acme', slug: 'acme', enabled: true, souls: ['soul-1', 'soul-2'] }
const souls = [{ id: 'soul-1', name: 'API' }, { id: 'soul-2', name: 'Web' }]

const incident = {
  id: 'inc-1',
  title: '<img src=x onerror=alert(1)> Checkout errors',
  status: 'identified',
  impact: 'major',
  affected_souls: ['soul-1'],
  started_at: '2026-10-27T10:00:00Z',
  updates: [
    { id: 'u1', message: 'Investigating', status: 'investigating', updated_at: '2026-10-27T10:00:00Z' },
    { id: 'u2', message: 'Found it', status: 'identified', updated_at: '2026-10-27T10:30:00Z' },
  ],
}
const announcement = {
  id: 'a-1',
  title: 'DB upgrade',
  message: 'Read-only for a bit',
  starts_at: '2026-11-01T10:00:00Z',
  ends_at: '2026-11-01T11:00:00Z',
  affected_souls: ['soul-2'],
  phase: 'upcoming',
}

let incidents: unknown[] = []
let announcements: unknown[] = []

function mockGets() {
  api.get.mockImplementation((path: string) => {
    if (path.endsWith('/incidents')) return Promise.resolve(incidents)
    if (path.endsWith('/announcements')) return Promise.resolve(announcements)
    if (path === '/maintenance') return Promise.resolve([{ id: 'mw-1', name: 'Network work' }, { id: 'mw-r', name: 'Nightly', recurring: 'daily' }])
    return Promise.reject(new Error('unexpected ' + path))
  })
}

describe('StatusPageIncidents', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    auth.role = 'admin'
    incidents = [incident]
    announcements = [announcement]
    mockGets()
    api.post.mockResolvedValue({})
    api.patch.mockResolvedValue({})
    api.delete.mockResolvedValue(undefined)
  })
  afterEach(cleanup)

  it('lists incidents with a newest-first timeline, as text', async () => {
    const { container } = render(<StatusPageIncidents page={page} souls={souls} onClose={() => {}} />)
    const item = await screen.findByRole('heading', { name: /Checkout errors/ })
    expect(item.textContent).toContain('<img src=x onerror=alert(1)>')
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByText('Affected: API')).toBeTruthy()
    const timeline = item.closest('li')!.querySelector('ol')!
    expect(timeline.textContent!.indexOf('Found it')).toBeLessThan(timeline.textContent!.indexOf('Investigating'))
    expect(screen.getByRole('link', { name: /Preview live page/ }).getAttribute('href')).toBe('/status/acme')
  })

  it('creates an incident through the dedicated endpoint', async () => {
    render(<StatusPageIncidents page={page} souls={souls} onClose={() => {}} />)
    await screen.findByRole('heading', { name: /Checkout errors/ })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Login failures' } })
    fireEvent.change(screen.getByLabelText('Impact'), { target: { value: 'critical' } })
    fireEvent.change(screen.getByLabelText(/^Message/), { target: { value: 'Looking into it' } })
    fireEvent.click(screen.getByLabelText('Web'))
    fireEvent.click(screen.getByRole('button', { name: 'Create incident' }))
    await waitFor(() =>
      expect(api.post).toHaveBeenCalledWith('/status-pages/page-1/incidents', {
        title: 'Login failures', impact: 'critical', status: 'investigating', message: 'Looking into it', affected_souls: ['soul-2'],
      })
    )
    await waitFor(() => expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe(''))
  })

  it('posts an update, resolves and deletes with confirmation', async () => {
    render(<StatusPageIncidents page={page} souls={souls} onClose={() => {}} />)
    await screen.findByRole('heading', { name: /Checkout errors/ })
    fireEvent.change(screen.getByLabelText(/Update message for/), { target: { value: 'Fix rolling out' } })
    fireEvent.change(screen.getByLabelText('Update status'), { target: { value: 'monitoring' } })
    fireEvent.click(screen.getByRole('button', { name: 'Post update' }))
    await waitFor(() =>
      expect(api.post).toHaveBeenCalledWith('/status-pages/page-1/incidents/inc-1/updates', { message: 'Fix rolling out', status: 'monitoring' })
    )

    fireEvent.click(screen.getByRole('button', { name: /Resolve incident/ }))
    await waitFor(() => expect(api.post).toHaveBeenCalledWith('/status-pages/page-1/incidents/inc-1/resolve', {}))

    fireEvent.click(screen.getByRole('button', { name: /Delete incident/ }))
    expect(api.delete).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('dialog', { name: 'Delete Incident' })
    fireEvent.click(within(dialog).getByRole('button', { name: /^Confirm deletion/ }))
    await waitFor(() => expect(api.delete).toHaveBeenCalledWith('/status-pages/page-1/incidents/inc-1'))
  })

  it('shows the server error when a write is refused', async () => {
    api.post.mockRejectedValueOnce(new Error('soul soul-9 is not shown on this status page'))
    render(<StatusPageIncidents page={page} souls={souls} onClose={() => {}} />)
    await screen.findByRole('heading', { name: /Checkout errors/ })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'x' } })
    fireEvent.change(screen.getByLabelText(/^Message/), { target: { value: 'y' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create incident' }))
    expect((await screen.findByRole('alert')).textContent).toContain('not shown on this status page')
  })

  it('manages maintenance announcements', async () => {
    render(<StatusPageIncidents page={page} souls={souls} onClose={() => {}} />)
    await screen.findByRole('heading', { name: /Checkout errors/ })
    fireEvent.click(screen.getByRole('tab', { name: /Maintenance/ }))
    expect(await screen.findByText('DB upgrade')).toBeTruthy()
    expect(screen.getByText('Upcoming')).toBeTruthy()

    // Only one-off windows can be linked.
    const windowSelect = await screen.findByLabelText(/Maintenance window/)
    expect(within(windowSelect).queryByText('Nightly')).toBeNull()

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Kernel patch' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create announcement' }))
    expect((await screen.findByRole('alert')).textContent).toContain('Start and end times are required')
    expect(api.post).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Starts'), { target: { value: '2026-11-02T10:00' } })
    fireEvent.change(screen.getByLabelText('Ends'), { target: { value: '2026-11-02T11:00' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create announcement' }))
    await waitFor(() => expect(api.post).toHaveBeenCalledWith('/status-pages/page-1/announcements', expect.objectContaining({
      title: 'Kernel patch', starts_at: new Date('2026-11-02T10:00').toISOString(), ends_at: new Date('2026-11-02T11:00').toISOString(),
    })))

    fireEvent.click(screen.getByRole('button', { name: 'Edit announcement DB upgrade' }))
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('DB upgrade')
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Now shorter' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save announcement' }))
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('/status-pages/page-1/announcements/a-1', expect.objectContaining({ message: 'Now shorter' })))

    fireEvent.click(screen.getByRole('button', { name: 'Delete announcement DB upgrade' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete Announcement' })
    fireEvent.click(within(dialog).getByRole('button', { name: /^Confirm deletion/ }))
    await waitFor(() => expect(api.delete).toHaveBeenCalledWith('/status-pages/page-1/announcements/a-1'))
  })

  it('hides write controls from viewers', async () => {
    auth.role = 'viewer'
    render(<StatusPageIncidents page={page} souls={souls} onClose={() => {}} />)
    await screen.findByRole('heading', { name: /Checkout errors/ })
    expect(screen.queryByRole('button', { name: 'Create incident' })).toBeNull()
    expect(screen.queryByRole('button', { name: /Resolve incident/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Post update' })).toBeNull()
    expect(api.get).not.toHaveBeenCalledWith('/maintenance')
  })
})
