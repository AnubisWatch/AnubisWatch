import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getDisplayTimeZone, setDisplayTimeZone } from '../utils/date'
import { Settings } from './Settings'

const mocks = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('../api/client', () => ({ api: { get: mocks.get, put: mocks.put } }))
vi.mock('../api/hooks', () => ({
  useAuth: () => ({ user: { id: 'u', email: 'a@x', name: 'A', role: 'admin', workspace: 'default' } }),
  useStats: () => ({ data: null }),
}))

const config = { instance_name: 'AnubisWatch', timezone: '', language: 'en', theme: 'dark', read_only_keys: [] }

describe('Settings timezone', () => {
  beforeEach(() => {
    mocks.get.mockReset().mockResolvedValue(config)
    mocks.put.mockReset().mockImplementation(async (_path: string, body: Record<string, unknown>) => ({ ...config, ...body }))
  })
  afterEach(() => setDisplayTimeZone(''))

  // The select offered five hard-coded zones.
  it('offers every zone plus the viewer-local default, and applies a saved zone', async () => {
    render(<MemoryRouter><Settings /></MemoryRouter>)
    const select = await screen.findByLabelText('Timezone') as HTMLSelectElement
    const values = Array.from(select.options).map((o) => o.value)
    expect(values[0]).toBe('')
    expect(values.length).toBeGreaterThan(300)
    expect(values).toContain('Pacific/Auckland')

    fireEvent.change(select, { target: { value: 'Asia/Tokyo' } })
    fireEvent.click(screen.getByRole('button', { name: /save changes/i }))
    await waitFor(() => expect(mocks.put).toHaveBeenCalledWith('/config', { timezone: 'Asia/Tokyo' }))
    await waitFor(() => expect(getDisplayTimeZone()).toBe('Asia/Tokyo'))
  })

  it('keeps a saved zone the browser does not list', async () => {
    mocks.get.mockResolvedValue({ ...config, timezone: 'Etc/GMT-3' })
    render(<MemoryRouter><Settings /></MemoryRouter>)
    const select = await screen.findByLabelText('Timezone') as HTMLSelectElement
    await waitFor(() => expect(select.value).toBe('Etc/GMT-3'))
  })
})
