import { render, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { getDisplayTimeZone, setDisplayTimeZone } from '../utils/date'
import { Layout } from './Layout'

vi.mock('../api/hooks', () => ({
  useIncidents: () => ({ incidents: [] }),
  useAuth: () => ({ user: null, logout: vi.fn() }),
}))
vi.mock('./WorkspaceSwitcher', () => ({ WorkspaceSwitcher: () => null }))

afterEach(() => {
  vi.unstubAllGlobals()
  setDisplayTimeZone('')
})

// The saved timezone was never read by the dashboard.
describe('Layout', () => {
  it('applies the saved display time zone', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const body = String(input).endsWith('/config') ? { timezone: 'Europe/Istanbul' } : []
      return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }))
    render(
      <MemoryRouter>
        <Routes><Route path="/" element={<Layout />} /></Routes>
      </MemoryRouter>,
    )
    await waitFor(() => expect(getDisplayTimeZone()).toBe('Europe/Istanbul'))
  })
})
