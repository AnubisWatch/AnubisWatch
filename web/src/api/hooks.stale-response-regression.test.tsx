import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSouls } from './hooks'

// Regression coverage for response ownership in useSouls.
//
// src/api/hooks.ts already has the right abstraction: useApi() (line 11) guards
// every setState with `mountedRef.current && sequence === requestSequence.current`,
// and useSoul/useJudgments/useStats use it. useSouls used to re-implement fetching
// with a bare `setData(result)`, so the last response to *resolve* won rather
// than the last request to *start*, and a slow earlier fetch could overwrite a
// newer list.
//
// This was reachable in normal operation because useSouls wires
//   useRealtimeRefresh(['soul_update','judgment','status'], fetchSouls)
// so every live dashboard event started another fetch, interleaved with the
// create/update/delete paths that also awaited a refresh.

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('./client', () => ({ api: mocks }))
vi.mock('../hooks/useRealtimeRefresh', () => ({ useRealtimeRefresh: vi.fn() }))

type Soul = { id: string }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

function page(ids: string[]) {
  return {
    data: ids.map((id) => ({ id })),
    pagination: { total: ids.length, offset: 0, limit: ids.length, has_more: false },
  }
}

const ids = (souls: Soul[]) => souls.map((s) => s.id)

describe('useSouls response ownership', () => {
  beforeEach(() => {
    mocks.get.mockReset()
  })

  it('keeps the newest response when an older one resolves last', async () => {
    const older = deferred<ReturnType<typeof page>>()
    const newer = deferred<ReturnType<typeof page>>()
    mocks.get.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)

    const hook = renderHook(() => useSouls())

    // A live event starts a second fetch.
    await act(async () => {
      void hook.result.current.refetch()
    })

    // Newest request finishes first.
    await act(async () => {
      newer.resolve(page(['fresh-1', 'fresh-2']))
      await newer.promise
    })

    // Older request finishes afterwards, carrying a stale list.
    await act(async () => {
      older.resolve(page(['stale']))
      await older.promise
    })

    expect(ids(hook.result.current.souls)).toEqual(['fresh-1', 'fresh-2'])
  })

  it('does not apply a stale error over a newer success', async () => {
    const older = deferred<ReturnType<typeof page>>()
    const newer = deferred<ReturnType<typeof page>>()
    mocks.get.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)

    const hook = renderHook(() => useSouls())

    await act(async () => {
      void hook.result.current.refetch()
    })

    await act(async () => {
      newer.resolve(page(['fresh-1']))
      await newer.promise
    })

    await act(async () => {
      older.reject(new Error('stale failure'))
      await older.promise.catch(() => {})
    })

    expect(hook.result.current.error).toBeNull()
  })

  it('is the control: a fetch populates the list', async () => {
    // Not Once: the hook fetches on mount (both before and after the fix) and
    // this case also triggers an explicit refetch.
    mocks.get.mockResolvedValue(page(['only']))

    const hook = renderHook(() => useSouls())

    await act(async () => {
      await hook.result.current.refetch()
    })

    expect(ids(hook.result.current.souls)).toEqual(['only'])
  })
})