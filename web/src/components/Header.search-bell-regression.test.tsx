import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { Header } from './Header'

const mocks = vi.hoisted(() => ({ incidents: [] as Array<{ status: string }> }))
vi.mock('../api/hooks', () => ({
  useAuth: () => ({ user: null, logout: vi.fn() }),
  useIncidents: () => ({ incidents: mocks.incidents }),
}))
vi.mock('./WorkspaceSwitcher', () => ({ WorkspaceSwitcher: () => null }))

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

const renderHeader = () =>
  render(
    <MemoryRouter initialEntries={['/']}>
      <Header />
      <Routes><Route path="*" element={<Where />} /></Routes>
    </MemoryRouter>
  )

// The search box had no handler and the bell toggled a panel that was never
// rendered, with a dot that always pulsed.
describe('Header search and notifications', () => {
  it('searches souls on Enter', () => {
    mocks.incidents = []
    renderHeader()
    const input = screen.getByRole('searchbox', { name: 'Search souls' })
    fireEvent.change(input, { target: { value: 'payments api' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(screen.getByTestId('where')).toHaveTextContent('/souls?q=payments%20api')
  })

  it('shows the open incident count and links to incidents', () => {
    mocks.incidents = [{ status: 'open' }, { status: 'acknowledged' }, { status: 'resolved' }]
    renderHeader()
    fireEvent.click(screen.getByRole('button', { name: '2 open incidents' }))
    expect(screen.getByTestId('where')).toHaveTextContent('/incidents')
  })

  it('has no indicator without open incidents', () => {
    mocks.incidents = [{ status: 'resolved' }]
    renderHeader()
    expect(screen.getByRole('button', { name: 'No open incidents' }).querySelector('.animate-ping')).toBeNull()
  })
})
