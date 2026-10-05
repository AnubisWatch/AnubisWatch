import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { SoulDetail } from './SoulDetail'
const state = vi.hoisted(() => ({ id: 'A', forceCheck: vi.fn(), refetch: vi.fn() }))
vi.mock('react-router-dom', () => ({ useParams: () => ({ id: state.id }), useNavigate: () => vi.fn() }))
vi.mock('../api/hooks', () => ({
  useSoul: () => ({ soul: { id: state.id, name: state.id, target: 'https://fixture.test', type: 'http', enabled: true, tags: [] }, loading: false, error: null, forceCheck: state.forceCheck, refetch: state.refetch }),
  useSoulJudgments: () => ({ data: [], loading: false, error: null, refetch: state.refetch }),
}))
function deferred() { let resolve!: (value: unknown) => void; let reject!: (error: Error) => void; const promise = new Promise((res, rej) => { resolve = res; reject = rej }); return { promise, resolve, reject } }
beforeEach(() => { state.id = 'A'; state.forceCheck.mockReset(); state.refetch.mockReset(); state.refetch.mockResolvedValue(undefined) })

afterEach(() => vi.useRealTimers())

it('current check completion remains visible', async () => {
  state.forceCheck.mockResolvedValue({ status: 'passed', latency: 88 })
  render(<SoulDetail />)
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Test Now' })) })
  expect(screen.getByText('Check passed! Latency: 88ms')).toBeInTheDocument()
})
it.each(['success', 'failure'])('ignores a stale route check %s', async mode => {
  const old = deferred(); state.forceCheck.mockReturnValue(old.promise)
  const view = render(<SoulDetail />)
  fireEvent.click(screen.getByRole('button', { name: 'Test Now' }))
  state.id = 'B'; view.rerender(<SoulDetail />)
  expect(screen.getByRole('button', { name: 'Test Now' })).toBeEnabled()
  await act(async () => { if (mode === 'success') old.resolve({ status: 'passed', latency: 119 }); else old.reject(new Error('old error')) })
  expect(screen.queryByText(/119ms|old error/)).not.toBeInTheDocument()
  expect(state.refetch).not.toHaveBeenCalled()
})
it('an old completion cannot finish the replacement pending check', async () => {
  const old = deferred(), current = deferred()
  state.forceCheck.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
  const view = render(<SoulDetail />)
  fireEvent.click(screen.getByRole('button', { name: 'Test Now' }))
  state.id = 'B'; view.rerender(<SoulDetail />)
  fireEvent.click(screen.getByRole('button', { name: 'Test Now' }))
  await act(async () => { old.resolve({ status: 'passed', latency: 119 }) })
  expect(screen.getByRole('button', { name: 'Test Now' })).toBeDisabled()
  expect(screen.queryByText(/119ms/)).not.toBeInTheDocument()
  await act(async () => { current.resolve({ status: 'passed', latency: 222 }) })
  expect(screen.getByRole('button', { name: 'Test Now' })).toBeEnabled()
  expect(screen.getByText('Check passed! Latency: 222ms')).toBeInTheDocument()
})
it('an earlier result timer cannot clear a newer check result', async () => {
  vi.useFakeTimers()
  state.forceCheck.mockResolvedValueOnce({ status: 'passed', latency: 88 }).mockResolvedValueOnce({ status: 'passed', latency: 99 })
  render(<SoulDetail />)
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Test Now' })) })
  await act(async () => { vi.advanceTimersByTime(4000) })
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Test Now' })) })
  await act(async () => { vi.advanceTimersByTime(1000) })
  expect(screen.getByText('Check passed! Latency: 99ms')).toBeInTheDocument()
  await act(async () => { vi.advanceTimersByTime(4000) })
  expect(screen.queryByText('Check passed! Latency: 99ms')).not.toBeInTheDocument()
})
it('unmounted check completion cannot start refetch side effects', async () => {
  const old = deferred(); state.forceCheck.mockReturnValue(old.promise)
  const view = render(<SoulDetail />)
  fireEvent.click(screen.getByRole('button', { name: 'Test Now' }))
  view.unmount()
  await act(async () => { old.resolve({ status: 'passed', latency: 119 }) })
  expect(state.refetch).not.toHaveBeenCalled()
})
