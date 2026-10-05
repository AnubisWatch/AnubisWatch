import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSoulStore } from './soulStore'
import type { Judgment, Soul } from '../api/client'

const api = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('../api/client', () => ({ api }))
const soul: Soul = { id: 'one', name: 'One', type: 'http', target: 'https://example.test', created_at: '', updated_at: '' }
const judgment = (latency: number): Judgment => ({ id: `j${latency}`, soul_id: 'one', status: 'passed', latency, timestamp: `t${latency}`, region: 'default' })
function deferred() {
  let resolve!: (value: Judgment) => void
  let reject!: (error: Error) => void
  const promise = new Promise<Judgment>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

beforeEach(() => {
  api.post.mockReset()
  useSoulStore.setState({ souls: [soul], loading: false, error: null, initialChecks: {} })
})

describe('initial check completion ownership', () => {
  it.each(['old success', 'old failure', 'new failure'])('retains the newer retry when %s completes', async (scenario) => {
    const old = deferred()
    const latest = deferred()
    api.post.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const first = useSoulStore.getState().retryInitialCheck('one')
    const second = useSoulStore.getState().retryInitialCheck('one')
    if (scenario === 'new failure') latest.reject(new Error('latest failed'))
    else latest.resolve(judgment(20))
    await second
    if (scenario === 'old failure') old.reject(new Error('old failed'))
    else old.resolve(judgment(10))
    await first
    expect(useSoulStore.getState().souls[0].latency).toBe(scenario === 'new failure' ? undefined : 20)
    expect(useSoulStore.getState().initialChecks.one).toBe(scenario === 'new failure' ? 'failed' : undefined)
  })

  it('keeps independent Soul retries and repeated sequential checks working', async () => {
    useSoulStore.setState({ souls: [soul, { ...soul, id: 'two' }] })
    const one = deferred()
    const two = deferred()
    api.post.mockReturnValueOnce(one.promise).mockReturnValueOnce(two.promise)
    const first = useSoulStore.getState().retryInitialCheck('one')
    const second = useSoulStore.getState().retryInitialCheck('two')
    two.resolve({ ...judgment(30), soul_id: 'two' })
    await second
    one.resolve(judgment(10))
    await first
    expect(useSoulStore.getState().souls.map(s => s.latency)).toEqual([10, 30])
    api.post.mockResolvedValueOnce(judgment(40))
    await useSoulStore.getState().retryInitialCheck('one')
    expect(useSoulStore.getState().souls[0].latency).toBe(40)
    expect(useSoulStore.getState().initialChecks).toEqual({})
  })
})
