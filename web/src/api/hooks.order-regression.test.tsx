import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSoul } from './hooks'
const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('./client', () => ({ api: mocks }))
vi.mock('../hooks/useRealtimeRefresh', () => ({ useRealtimeRefresh: vi.fn() }))
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
describe('generic API request ownership', () => {
  beforeEach(() => { mocks.get.mockReset() })
  it.each([
    { name: 'late old success', oldFails: false, latestFails: false, oldFirst: false },
    { name: 'late old failure', oldFails: true, latestFails: false, oldFirst: false },
    { name: 'latest failure', oldFails: false, latestFails: true, oldFirst: false },
    { name: 'old success while latest pending', oldFails: false, latestFails: false, oldFirst: true },
    { name: 'old failure while latest pending', oldFails: true, latestFails: false, oldFirst: true },
  ])('preserves current state with $name', async ({ oldFails, latestFails, oldFirst }) => {
    const old = deferred<{ id: string }>(), latest = deferred<{ id: string }>()
    mocks.get.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const hook = renderHook(({ id }) => useSoul(id), { initialProps: { id: 'old' } })
    hook.rerender({ id: 'latest' })
    const finishOld = async () => {
      await act(async () => {
        if (oldFails) old.reject(new Error('old failure'))
        else old.resolve({ id: 'old' })
        await old.promise.catch(() => {})
      })
    }
    const finishLatest = async () => {
      await act(async () => {
        if (latestFails) latest.reject(new Error('latest failure'))
        else latest.resolve({ id: 'latest' })
        await latest.promise.catch(() => {})
      })
    }
    if (oldFirst) {
      await finishOld()
      expect(hook.result.current.loading).toBe(true)
      expect(hook.result.current.soul).toBeNull()
      expect(hook.result.current.error).toBeNull()
      await finishLatest()
    } else {
      await finishLatest()
      await finishOld()
    }
    expect(hook.result.current.loading).toBe(false)
    expect(hook.result.current.soul?.id ?? null).toBe(latestFails ? null : 'latest')
    expect(hook.result.current.error).toBe(latestFails ? 'latest failure' : null)
    hook.unmount()
  })
  it('keeps a manual refetch newer than an initial request', async () => {
    const old = deferred<{ id: string }>(), latest = deferred<{ id: string }>()
    mocks.get.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const hook = renderHook(() => useSoul('same'))
    let refetch!: Promise<unknown>
    act(() => { refetch = hook.result.current.refetch() })
    await act(async () => { latest.resolve({ id: 'latest' }); await refetch })
    await act(async () => { old.resolve({ id: 'old' }); await old.promise })
    expect(hook.result.current.soul?.id).toBe('latest')
    hook.unmount()
  })
  it('invalidates work across StrictMode effect restarts', async () => {
    const old = deferred<{ id: string }>(), latest = deferred<{ id: string }>()
    mocks.get.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const hook = renderHook(() => useSoul('same'), { reactStrictMode: true })
    await act(async () => { latest.resolve({ id: 'latest' }); await latest.promise })
    await act(async () => { old.resolve({ id: 'old' }); await old.promise })
    expect(hook.result.current.soul?.id).toBe('latest')
    hook.unmount()
  })
})
