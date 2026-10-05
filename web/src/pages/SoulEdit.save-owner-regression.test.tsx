import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { SoulEdit } from './SoulEdit'
const fixtures = vi.hoisted(() => ({ A: { id: 'A', name: 'A', target: 'https://fixture.test', type: 'http', enabled: true, tags: [], weight: 60, timeout: 10 }, B: { id: 'B', name: 'B', target: 'https://fixture.test', type: 'http', enabled: true, tags: [], weight: 60, timeout: 10 } }))
const state = vi.hoisted(() => ({ id: 'A', update: vi.fn(), navigate: vi.fn() }))
vi.mock('react-router-dom', () => ({ useParams: () => ({ id: state.id }), useNavigate: () => state.navigate }))
vi.mock('../api/hooks', () => ({ useSoul: () => ({ soul: state.id === 'A' ? fixtures.A : fixtures.B, loading: false, error: null, updateSoul: state.update }) }))
function deferred() { let resolve!: (value: unknown) => void; let reject!: (error: Error) => void; const promise = new Promise((res, rej) => { resolve = res; reject = rej }); return { promise, resolve, reject } }
beforeEach(() => { state.id = 'A'; state.update.mockReset(); state.navigate.mockReset() })

it('active save navigates to its Soul', async () => {
  state.update.mockResolvedValue({})
  render(<SoulEdit />)
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Save Changes' })) })
  expect(state.navigate).toHaveBeenCalledWith('/souls/A')
})
it.each(['success', 'failure'])('unmounted %s cannot navigate', async outcome => {
  const pending = deferred(); state.update.mockReturnValue(pending.promise)
  const view = render(<SoulEdit />)
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  view.unmount()
  await act(async () => { if (outcome === 'success') pending.resolve({}); else pending.reject(new Error('obsolete save')) })
  expect(state.navigate).not.toHaveBeenCalled()
})
it('replaced route drops old failure', async () => {
  const pending = deferred(); state.update.mockReturnValue(pending.promise)
  const view = render(<SoulEdit />)
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  state.id = 'B'; view.rerender(<SoulEdit />)
  await act(async () => { pending.reject(new Error('obsolete save')) })
  expect(screen.queryByText('obsolete save')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
  expect(state.navigate).not.toHaveBeenCalled()
})
it('old completion cannot finish a new save', async () => {
  const first = deferred(); const second = deferred()
  state.update.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
  const view = render(<SoulEdit />)
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  state.id = 'B'; view.rerender(<SoulEdit />)
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await act(async () => { first.resolve({}) })
  expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled()
  expect(state.navigate).not.toHaveBeenCalled()
  await act(async () => { second.resolve({}) })
  expect(state.navigate).toHaveBeenCalledExactlyOnceWith('/souls/B')
})
