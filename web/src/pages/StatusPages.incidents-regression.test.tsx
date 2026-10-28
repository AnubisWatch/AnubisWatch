import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { StatusPages } from './StatusPages'

// Incidents and announcements have their own endpoints. The page editor's PUT
// must not echo the copy it loaded, or an update another editor posted in the
// meantime would be overwritten.

const mockUpdatePage = vi.fn()
vi.mock('../api/hooks', async () => {
  const actual = await vi.importActual('../api/hooks')
  return {
    ...actual,
    useStatusPages: () => ({
      pages: [{
        id: 'page-1', name: 'Production Status', slug: 'production', enabled: true, souls: ['soul-1'],
        incidents: [{ id: 'inc-1', title: 'Stale copy' }], announcements: [{ id: 'a-1', title: 'Stale' }],
      }],
      loading: false, error: null, refetch: vi.fn(), createPage: vi.fn(), updatePage: mockUpdatePage, deletePage: vi.fn(),
    }),
    useSouls: () => ({ souls: [{ id: 'soul-1', name: 'API', type: 'http', enabled: true }] }),
  }
})

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() }))
vi.mock('../api/client', async () => ({ ...(await vi.importActual('../api/client')), api }))

describe('StatusPages incidents', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUpdatePage.mockResolvedValue(undefined)
    api.get.mockResolvedValue([])
  })
  afterEach(cleanup)

  it('does not send incidents or announcements with a page edit', async () => {
    render(<StatusPages />)
    fireEvent.click(screen.getByLabelText(/edit status page production status/i))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Save Status Page' }))
    await waitFor(() => expect(mockUpdatePage).toHaveBeenCalled())
    const payload = mockUpdatePage.mock.calls[0][1]
    expect(payload).not.toHaveProperty('incidents')
    expect(payload).not.toHaveProperty('announcements')
  })

  it('opens the incidents panel for a page', async () => {
    render(<StatusPages />)
    fireEvent.click(screen.getByLabelText('Incidents for status page Production Status'))
    expect(await screen.findByRole('dialog', { name: /Incidents · Production Status/ })).toBeTruthy()
    await waitFor(() => expect(api.get).toHaveBeenCalledWith('/status-pages/page-1/incidents'))
    fireEvent.click(screen.getByLabelText('Close incidents'))
    expect(screen.queryByRole('dialog', { name: /Incidents ·/ })).toBeNull()
  })
})
