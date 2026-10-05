import { act, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { DashboardDetail } from './DashboardDetail'
const state = vi.hoisted(() => ({ id: 'A', get: vi.fn() }))
vi.mock('react-router-dom', () => ({ useParams: () => ({ id: state.id }), useNavigate: () => vi.fn() }))
vi.mock('../api/client', () => ({ api: { get: (...args: unknown[]) => state.get(...args) } }))
const dashboard = (id: string) => ({ id, name: `Dashboard ${id}`, description: '', refresh_sec: 0, widgets: [] })
function deferred() { let resolve!: (value: unknown) => void; let reject!: (error: Error) => void; const promise = new Promise((res, rej) => { resolve = res; reject = rej }); return { promise, resolve, reject } }
beforeEach(() => { state.id = 'A'; state.get.mockReset() })
it('current fetch renders its dashboard', async () => {
  state.get.mockResolvedValue(dashboard('A'))
  await act(async () => { render(<DashboardDetail />) })
  expect(screen.getByText('Dashboard A')).toBeInTheDocument()
})
it.each(['success', 'failure'])('stale %s cannot replace newly loaded route', async outcome => {
  const old = deferred(); state.get.mockReturnValueOnce(old.promise).mockResolvedValueOnce(dashboard('B'))
  const view = render(<DashboardDetail />)
  state.id = 'B'
  await act(async () => { view.rerender(<DashboardDetail />) })
  await act(async () => { if (outcome === 'success') old.resolve(dashboard('A')); else old.reject(new Error('old failure')) })
  expect(screen.getByText('Dashboard B')).toBeInTheDocument()
  expect(screen.queryByText('Dashboard A')).not.toBeInTheDocument()
  expect(screen.queryByText('Dashboard Not Found')).not.toBeInTheDocument()
})
it('old completion cannot clear new route loading', async () => {
  const first = deferred(); const second = deferred()
  state.get.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
  const view = render(<DashboardDetail />)
  state.id = 'B'; view.rerender(<DashboardDetail />)
  await act(async () => { first.resolve(dashboard('A')) })
  expect(document.querySelector('.animate-spin')).toBeInTheDocument()
  expect(screen.queryByText('Dashboard A')).not.toBeInTheDocument()
  await act(async () => { second.resolve(dashboard('B')) })
  expect(screen.getByText('Dashboard B')).toBeInTheDocument()
})
it('old fetch cannot overwrite new-dashboard route', async () => {
  const old = deferred(); state.get.mockReturnValue(old.promise)
  const view = render(<DashboardDetail />)
  state.id = 'new'; view.rerender(<DashboardDetail />)
  await act(async () => { old.resolve(dashboard('A')) })
  expect(screen.getByRole('heading', { name: 'New Dashboard' })).toBeInTheDocument()
  expect(screen.queryByText('Dashboard A')).not.toBeInTheDocument()
})
