import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  useChannels,
  useDashboards,
  useIncidents,
  useRules,
  useStatusPages,
} from './hooks'

// Regression coverage for response ownership in the list hooks that used to
// hand-roll their fetching.
//
// useApi() (hooks.ts:11) already guards every state write with
// `mountedRef.current && sequence === requestSequence.current`; useSoul,
// useJudgments, useStats and useSouls use it. These five re-implemented
// fetching with a bare setData(result), so the last response to *resolve* won
// instead of the last request to *start*, and a slow earlier fetch silently
// reverted a newer list. useIncidents additionally wired
// useRealtimeRefresh, so a burst of alert events made the overlap routine.

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('./client', () => ({ api: mocks }))
vi.mock('../hooks/useRealtimeRefresh', () => ({ useRealtimeRefresh: vi.fn() }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

// Paginated hooks (fetchAllPaginated) get an ApiResponse envelope; the
// plain-array hooks get the bare array.
const enveloped = (ids: string[]) => ({
  data: ids.map((id) => ({ id })),
  pagination: { total: ids.length, offset: 0, limit: ids.length, has_more: false },
})

type Row = { id: string }

const cases = [
  { name: 'useChannels', use: () => useChannels(), key: 'channels', make: enveloped },
  { name: 'useRules', use: () => useRules(), key: 'rules', make: enveloped },
  { name: 'useIncidents', use: () => useIncidents(), key: 'incidents', make: (ids: string[]) => ids.map((id) => ({ id })) },
  { name: 'useStatusPages', use: () => useStatusPages(), key: 'pages', make: (ids: string[]) => ids.map((id) => ({ id })) },
  { name: 'useDashboards', use: () => useDashboards(), key: 'dashboards', make: (ids: string[]) => ids.map((id) => ({ id })) },
] as const

describe.each(cases)('$name response ownership', ({ use, key, make }) => {
  beforeEach(() => {
    mocks.get.mockReset()
  })

  it('keeps the newest response when an older one resolves last', async () => {
    const older = deferred<ReturnType<typeof make>>()
    const newer = deferred<ReturnType<typeof make>>()
    mocks.get.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)

    const hook = renderHook(() => use())
    const refetch = () => (hook.result.current as { refetch: () => Promise<unknown> }).refetch()

    await act(async () => {
      void refetch()
    })
    await act(async () => {
      newer.resolve(make(['fresh-1', 'fresh-2']))
      await newer.promise
    })
    await act(async () => {
      older.resolve(make(['stale']))
      await older.promise
    })

    const rows = (hook.result.current as Record<string, Row[]>)[key]
    expect(rows.map((r) => r.id)).toEqual(['fresh-1', 'fresh-2'])
  })

  it('does not apply a stale error over a newer success', async () => {
    const older = deferred<ReturnType<typeof make>>()
    const newer = deferred<ReturnType<typeof make>>()
    mocks.get.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)

    const hook = renderHook(() => use())
    const refetch = () => (hook.result.current as { refetch: () => Promise<unknown> }).refetch()

    await act(async () => {
      void refetch().catch(() => {})
    })
    await act(async () => {
      newer.resolve(make(['fresh-1']))
      await newer.promise
    })
    await act(async () => {
      older.reject(new Error('stale failure'))
      await older.promise.catch(() => {})
    })

    expect(hook.result.current.error).toBeNull()
  })

  it('is the control: a fetch populates the list', async () => {
    mocks.get.mockResolvedValue(make(['only']))

    const hook = renderHook(() => use())
    await act(async () => {
      await (hook.result.current as { refetch: () => Promise<unknown> }).refetch().catch(() => {})
    })

    const rows = (hook.result.current as Record<string, Row[]>)[key]
    expect(rows.map((r) => r.id)).toEqual(['only'])
  })
})