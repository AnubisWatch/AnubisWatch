import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { dispatchAuthSessionChanged } from './authEvents'
import {
  useChannels,
  useClusterPeers,
  useClusterStatus,
  useDashboards,
  useIncidents,
  useJudgments,
  useRules,
  useSoul,
  useSoulJudgments,
  useSouls,
  useStats,
  useStatusPages,
  useAuth,
} from './hooks'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))

vi.mock('./client', async () => {
  const actual = await vi.importActual<typeof import('./client')>('./client')
  return { ...actual, api: mocks }
})

const soul = {
  id: 's1', name: 'Soul', type: 'http' as const, target: 'https://example.com',
  enabled: true, weight: 60, timeout: 10,
}
const channel = {
  id: 'c1', name: 'Email', type: 'email' as const, enabled: true, config: {},
}
const rule = {
  id: 'r1', name: 'Rule', enabled: true, condition: 'response_time', threshold: 10,
  channels: ['c1'], severity: 'warning' as const,
}
const page = { id: 'p1', name: 'Page', slug: 'page', enabled: true, souls: [] }
const dashboard = { id: 'd1', name: 'Dash', widgets: [], refresh_sec: 30 }

async function settled(result: { current: { loading: boolean } }) {
  await waitFor(() => expect(result.current.loading).toBe(false))
}

describe('API hooks coverage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue([])
    mocks.post.mockResolvedValue({ id: 'created' })
    mocks.put.mockResolvedValue({ id: 'updated' })
    mocks.delete.mockResolvedValue(undefined)
  })

  it('runs every souls operation and both update payload paths', async () => {
    mocks.get.mockResolvedValue({ data: [soul], pagination: { total: 1, offset: 0, limit: 100, has_more: false } })
    const hook = renderHook(() => useSouls())
    await settled(hook.result)
    expect(hook.result.current.souls).toEqual([soul])
    expect(hook.result.current.pagination).toEqual({ total: 1, offset: 0, limit: 100, has_more: false })

    await act(async () => {
      expect(await hook.result.current.createSoul({ ...soul, id: undefined } as never)).toEqual({ id: 'created' })
      expect(await hook.result.current.updateSoul('s1', { name: 'Changed' })).toEqual({ id: 'updated' })
      await hook.result.current.deleteSoul('s1')
      expect(await hook.result.current.forceCheck('s1')).toEqual({ id: 'created' })
      await hook.result.current.refetch()
    })
    expect(mocks.put).toHaveBeenCalledWith('/souls/s1', expect.objectContaining({ id: 's1', name: 'Changed' }))

    mocks.get.mockResolvedValue({ data: [] })
    await act(async () => { await hook.result.current.refetch() })
    await act(async () => { await hook.result.current.updateSoul('missing', { name: 'Only partial' }) })
    expect(mocks.put).toHaveBeenLastCalledWith('/souls/missing', { name: 'Only partial' })
  })

  it('loads every soul page and rejects non-advancing pagination', async () => {
    mocks.get
      .mockResolvedValueOnce({ data: [soul], pagination: { total: 2, offset: 0, limit: 100, has_more: true, next_offset: 100 } })
      .mockResolvedValueOnce({ data: [{ ...soul, id: 's2' }], pagination: { total: 2, offset: 100, limit: 100, has_more: false } })
    const hook = renderHook(() => useSouls())
    await settled(hook.result)
    expect(hook.result.current.souls.map((item) => item.id)).toEqual(['s1', 's2'])
    expect(mocks.get).toHaveBeenNthCalledWith(1, '/souls?offset=0&limit=100')
    expect(mocks.get).toHaveBeenNthCalledWith(2, '/souls?offset=100&limit=100')
    hook.unmount()

    mocks.get.mockResolvedValueOnce({ data: [soul], pagination: { total: 2, offset: 0, limit: 100, has_more: true, next_offset: 0 } })
    const invalid = renderHook(() => useSouls())
    await settled(invalid.result)
    expect(invalid.result.current.error).toContain('next_offset must advance')
    invalid.unmount()
  })

  it.each([
    ['Error', new Error('souls failed'), 'souls failed'],
    ['unknown', 'failure', 'Unknown error'],
  ])('reports %s failures from useSouls', async (_label, failure, message) => {
    mocks.get.mockRejectedValue(failure)
    const hook = renderHook(() => useSouls())
    await settled(hook.result)
    expect(hook.result.current.error).toBe(message)
    expect(hook.result.current.souls).toEqual([])
  })

  it('runs single-soul success, no-id guards, and generic refetch failure', async () => {
    mocks.get.mockResolvedValue(soul)
    const hook = renderHook(() => useSoul('s1'))
    await settled(hook.result)
    expect(hook.result.current.soul).toEqual(soul)
    await act(async () => {
      expect(await hook.result.current.updateSoul({ name: 'Changed' })).toEqual({ id: 'updated' })
      await hook.result.current.deleteSoul()
      expect(await hook.result.current.forceCheck()).toEqual({ id: 'created' })
    })

    const noId = renderHook(() => useSoul(undefined))
    await settled(noId.result)
    await act(async () => {
      expect(await noId.result.current.updateSoul({})).toBeUndefined()
      expect(await noId.result.current.deleteSoul()).toBeUndefined()
      expect(await noId.result.current.forceCheck()).toBeUndefined()
    })

    mocks.get.mockRejectedValueOnce('bad value')
    await act(async () => {
      await expect(hook.result.current.refetch()).rejects.toBe('bad value')
    })
    expect(hook.result.current.error).toBe('Unknown error')
  })

  it.each([
    [new Error('generic failed'), 'generic failed'],
    ['opaque generic failure', 'Unknown error'],
  ])('reports generic useApi failures', async (failure, message) => {
    mocks.get.mockRejectedValueOnce(failure)
    const hook = renderHook(() => useStats())
    await waitFor(() => expect(hook.result.current.loading).toBe(false))
    expect(hook.result.current.error).toBe(message)
    hook.unmount()
  })

  it('covers generic API hooks and suppresses state updates after unmount', async () => {
    const hooks = [
      [useJudgments, '/judgments'],
      [useStats, '/stats/overview'],
      [useClusterStatus, '/cluster/status'],
      [useClusterPeers, '/cluster/peers'],
    ] as const
    for (const [useHook, endpoint] of hooks) {
      mocks.get.mockResolvedValueOnce({ endpoint })
      const hook = renderHook(() => useHook())
      await settled(hook.result)
      expect(mocks.get).toHaveBeenCalledWith(endpoint)
      expect(hook.result.current.data).toEqual({ endpoint })
      hook.unmount()
    }

    mocks.get.mockResolvedValueOnce([])
    const judgments = renderHook(() => useSoulJudgments('s1'))
    await settled(judgments.result)
    expect(mocks.get).toHaveBeenCalledWith('/souls/s1/judgments')

    let resolvePending!: (value: unknown) => void
    mocks.get.mockImplementationOnce(() => new Promise((resolve) => { resolvePending = resolve }))
    const pending = renderHook(() => useStats())
    pending.unmount()
    await act(async () => { resolvePending({ ok: true }); await Promise.resolve() })

    let rejectPending!: (reason: unknown) => void
    mocks.get.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectPending = reject }))
    const rejected = renderHook(() => useStats())
    rejected.unmount()
    await act(async () => { rejectPending(new Error('late')); await Promise.resolve() })
  })

  it('runs all channel operations with merged and partial updates', async () => {
    mocks.get.mockResolvedValue({ data: [channel] })
    const hook = renderHook(() => useChannels())
    await settled(hook.result)
    expect(hook.result.current.channels).toEqual([channel])
    await act(async () => {
      await hook.result.current.createChannel({ ...channel, id: undefined } as never)
      await hook.result.current.updateChannel('c1', { name: 'New' })
      await hook.result.current.deleteChannel('c1')
      await hook.result.current.testChannel('c1')
      await hook.result.current.refetch()
    })
    expect(mocks.put).toHaveBeenCalledWith('/channels/c1', expect.objectContaining({ id: 'c1', name: 'New' }))
    mocks.get.mockResolvedValue({ data: [] })
    await act(async () => { await hook.result.current.refetch() })
    await act(async () => { await hook.result.current.updateChannel('missing', { enabled: false }) })
    expect(mocks.put).toHaveBeenLastCalledWith('/channels/missing', { enabled: false })
  })

  it.each([
    [useChannels, new Error('channels failed'), 'channels failed'],
    [useChannels, 42, 'Unknown error'],
    [useRules, new Error('rules failed'), 'rules failed'],
    [useRules, 42, 'Unknown error'],
    [useStatusPages, new Error('pages failed'), 'pages failed'],
    [useStatusPages, 42, 'Unknown error'],
    [useDashboards, new Error('dashboards failed'), 'dashboards failed'],
    [useDashboards, 42, 'Unknown error'],
  ])('reports list-hook errors', async (useHook, failure, message) => {
    mocks.get.mockRejectedValue(failure)
    const hook = renderHook(() => useHook())
    await settled(hook.result)
    expect(hook.result.current.error).toBe(message)
  })

  it('runs all rule operations with merged and partial updates', async () => {
    mocks.get.mockResolvedValue({ data: [rule] })
    const hook = renderHook(() => useRules())
    await settled(hook.result)
    expect(hook.result.current.rules).toEqual([rule])
    await act(async () => {
      await hook.result.current.createRule({ ...rule, id: undefined } as never)
      await hook.result.current.updateRule('r1', { name: 'New' })
      await hook.result.current.deleteRule('r1')
      await hook.result.current.refetch()
    })
    expect(mocks.put).toHaveBeenCalledWith('/rules/r1', expect.objectContaining({ id: 'r1', name: 'New' }))
    mocks.get.mockResolvedValue({ data: [] })
    await act(async () => { await hook.result.current.refetch() })
    await act(async () => { await hook.result.current.updateRule('missing', { enabled: false }) })
    expect(mocks.put).toHaveBeenLastCalledWith('/rules/missing', { enabled: false })
  })

  it('handles an initial incident-fetch rejection through the effect catch', async () => {
    mocks.get.mockRejectedValueOnce(new Error('initial incidents failed'))
    const hook = renderHook(() => useIncidents())
    await waitFor(() => expect(hook.result.current.loading).toBe(false))
    expect(hook.result.current.error).toBe('initial incidents failed')
    hook.unmount()
  })

  it('handles incidents including null data, mutations, and both error types', async () => {
    mocks.get.mockResolvedValueOnce(null)
    const hook = renderHook(() => useIncidents())
    await settled(hook.result)
    expect(hook.result.current.incidents).toEqual([])
    mocks.get.mockResolvedValue([{ id: 'i1' }])
    await act(async () => {
      await hook.result.current.refetch()
      await hook.result.current.acknowledgeIncident('i1')
      await hook.result.current.resolveIncident('i1')
    })
    expect(hook.result.current.incidents).toEqual([{ id: 'i1' }])

    mocks.get.mockRejectedValueOnce(new Error('incident failed'))
    await act(async () => {
      await expect(hook.result.current.refetch()).rejects.toThrow('incident failed')
    })
    expect(hook.result.current.error).toBe('incident failed')
    mocks.get.mockRejectedValueOnce('unknown')
    await act(async () => {
      await expect(hook.result.current.refetch()).rejects.toBe('unknown')
    })
    expect(hook.result.current.error).toBe('Unknown error')
  })

  it('runs status-page CRUD and both empty/populated states', async () => {
    mocks.get.mockResolvedValue([page])
    const hook = renderHook(() => useStatusPages())
    await settled(hook.result)
    expect(hook.result.current.pages).toEqual([page])
    await act(async () => {
      await hook.result.current.createPage({ ...page, id: undefined } as never)
      await hook.result.current.updatePage('p1', { name: 'New' })
      await hook.result.current.deletePage('p1')
      await hook.result.current.refetch()
    })
    mocks.get.mockResolvedValue(null)
    await act(async () => { await hook.result.current.refetch() })
    expect(hook.result.current.pages).toEqual([])
  })

  it('runs dashboard CRUD and both empty/populated states', async () => {
    mocks.get.mockResolvedValue([dashboard])
    const hook = renderHook(() => useDashboards())
    await settled(hook.result)
    expect(hook.result.current.dashboards).toEqual([dashboard])
    await act(async () => {
      await hook.result.current.createDashboard({ ...dashboard, id: undefined } as never)
      await hook.result.current.updateDashboard('d1', { name: 'New' })
      await hook.result.current.deleteDashboard('d1')
      await hook.result.current.refetch()
    })
    mocks.get.mockResolvedValue(null)
    await act(async () => { await hook.result.current.refetch() })
    expect(hook.result.current.dashboards).toEqual([])
  })

  it('completes a current successful auth sync and clears loading', async () => {
    const currentUser = { id: 'current', email: 'current@example.com', name: 'Current', role: 'admin', workspace: 'w' }
    mocks.get.mockResolvedValueOnce(currentUser)
    const hook = renderHook(() => useAuth())
    await waitFor(() => expect(hook.result.current.loading).toBe(false))
    expect(hook.result.current.user).toEqual(currentUser)
    expect(hook.result.current.isAuthenticated).toBe(true)
    hook.unmount()
  })

  it('completes a current failed auth sync and clears loading', async () => {
    mocks.get.mockRejectedValueOnce(new Error('anonymous now'))
    const hook = renderHook(() => useAuth())
    await waitFor(() => expect(hook.result.current.loading).toBe(false))
    expect(hook.result.current.user).toBeNull()
    expect(hook.result.current.isAuthenticated).toBe(false)
    hook.unmount()
  })

  it('executes auth login and stale successful-session finalizer branches', async () => {
    const firstUser = { id: 'u1', email: 'one@example.com', name: 'One', role: 'admin', workspace: 'w' }
    const secondUser = { ...firstUser, id: 'u2', email: 'two@example.com' }
    let resolveInitial!: (value: typeof firstUser) => void
    mocks.get.mockImplementationOnce(() => new Promise((resolve) => { resolveInitial = resolve }))
    const hook = renderHook(() => useAuth())
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user: secondUser }))
    await act(async () => { resolveInitial(firstUser); await Promise.resolve() })
    expect(hook.result.current.user).toEqual(secondUser)
    await act(async () => {
      expect(await hook.result.current.login('one@example.com', 'secret')).toEqual({ id: 'created' })
    })
    expect(mocks.post).toHaveBeenCalledWith('/auth/login', {
      email: 'one@example.com', password: 'secret',
    })
    hook.unmount()
  })

  it('executes initial and resync rejection handlers', async () => {
    mocks.get.mockRejectedValue(new Error('anonymous'))
    const hook = renderHook(() => useAuth())
    await waitFor(() => expect(hook.result.current.loading).toBe(false))
    act(() => dispatchAuthSessionChanged({ state: 'resync' }))
    await waitFor(() => expect(mocks.get).toHaveBeenCalledTimes(2))
    hook.unmount()
  })
})
