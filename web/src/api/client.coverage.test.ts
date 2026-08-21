import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './client'
import { dispatchAuthSessionChanged } from './authEvents'

function response(body: unknown, status = 200, ok = true) {
  return { ok, status, json: vi.fn().mockResolvedValue(body) }
}

function bodies() {
  return (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map((call) =>
    call[1].body ? JSON.parse(call[1].body) : undefined,
  )
}

describe('ApiClient exhaustive branches', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
    api.clearToken(false)
  })
  afterEach(() => vi.restoreAllMocks())

  it('does not dispatch auth events without a browser window', () => {
    vi.stubGlobal('window', undefined)
    expect(() => dispatchAuthSessionChanged({ state: 'resync' })).not.toThrow()
    vi.unstubAllGlobals()
  })

  it('normalizes all duration units, invalid durations, and judgment statuses', async () => {
    global.fetch = vi.fn().mockResolvedValue(response([
      { soul_id: 's', status: 'degraded', duration: '1ns' },
      { soul_id: 's', status: 'unknown', duration: '2us' },
      { soul_id: 's', status: 'embalmed', duration: '3µs' },
      { soul_id: 's', status: 'failed', duration: '4ms', error: 'kept', message: 'ignored' },
      { soul_id: 's', status: 'pending', duration: '5s' },
      { soul_id: 's', status: 'other', duration: '1m2h' },
      { soul_id: 's', status: 'alive', duration: 'invalid' },
      { soul_id: 's', status: 'dead', duration: null },
    ])) as typeof fetch
    const result = await api.get<Array<{ status: string; latency: number; error?: string }>>('/judgments')
    expect(result.map((item) => item.status)).toEqual([
      'pending', 'pending', 'pending', 'failed', 'pending', 'passed', 'passed', 'failed',
    ])
    expect(result.map((item) => item.latency)).toEqual([0, 0, 0, 4, 5000, 7_260_000, 0, 0])
    expect(result[3].error).toBe('kept')
  })

  it('normalizes soul aliases, numeric/fallback durations, and every health status', async () => {
    global.fetch = vi.fn().mockResolvedValue(response({ data: [
      { id: 'a', type: 'tcp', target: 'x', weight: 7, timeout: {}, status: 'healthy', tcp: { send: 'x' } },
      { id: 'b', type: 'dns', target: 'x', weight: null, timeout: '', status: 'unhealthy', dns: { record_type: 'AAAA' } },
      { id: 'c', type: 'http', target: 'x', weight: 'bad', timeout: 'bad', status: 'other', http: { method: 'GET' }, http_config: { method: 'POST' }, tcp_config: {}, dns_config: {} },
      { id: 'd', type: 'udp', target: 'x', weight: '5s' },
      { id: 'journey-shape', steps: [] },
    ] })) as typeof fetch
    const result = await api.get<{ data: Array<Record<string, unknown>> }>('/souls')
    expect(result.data[0]).toMatchObject({ weight: 7, timeout: 10, status: 'healthy', tcp_config: { send: 'x' } })
    expect(result.data[1]).toMatchObject({ weight: 60, timeout: 10, status: 'unhealthy', dns_config: { record_type: 'AAAA' } })
    expect(result.data[2]).toMatchObject({ weight: 60, timeout: 10, status: 'unknown', http_config: { method: 'POST' } })
  })

  it('normalizes sparse and alternate alert rules and leaves unrelated records alone', async () => {
    global.fetch = vi.fn().mockResolvedValue(response([
      { id: 'r1', enabled: true, channels: [], conditions: [{ type: 'failure_rate', threshold: '12', window: null }] },
      { id: 'r2', enabled: true, channels: [], conditions: [{ type: 'consecutive_failures', threshold: '4', duration: '2m' }] },
      { id: 'r2b', enabled: true, channels: [], conditions: [{ type: 'consecutive_failures', threshold: 6, duration: '1s' }] },
      { id: 'r2c', enabled: true, channels: [], conditions: [{ type: 'consecutive_failures' }] },
      { id: 'r3', enabled: true, channels: [], conditions: [{ type: 'threshold', metric: 'tls_expiry_days', value: '9' }], severity: 'critical' },
      { id: 'r4', enabled: true, channels: [], conditions: [], condition: 'kept', threshold: 3, duration: 4, consecutive: 5, severity: 'info' },
      { id: 'r5', enabled: true, channels: [], conditions: 'invalid' },
      { arbitrary: true }, null,
    ])) as typeof fetch
    const result = await api.get<Array<Record<string, unknown>>>('/rules')
    expect(result[0]).toMatchObject({ condition: 'error_rate', threshold: 12, duration: 0, severity: 'warning' })
    expect(result[1]).toMatchObject({ condition: 'downtime', threshold: 4, duration: 120, consecutive: 4 })
    expect(result[2]).toMatchObject({ condition: 'downtime', consecutive: 6 })
    expect(result[3]).toMatchObject({ condition: 'downtime', consecutive: 1 })
    expect(result[4]).toMatchObject({ condition: 'ssl_expiry', threshold: 9, duration: 0, severity: 'critical' })
    expect(result[5]).toMatchObject({ condition: 'kept', threshold: 3, duration: 4, consecutive: 5, severity: 'info' })
    expect(result[6]).toMatchObject({ threshold: 0, duration: 0, severity: 'warning' })
  })

  it('preserves status pages without eligible domain or object theme', async () => {
    global.fetch = vi.fn().mockResolvedValue(response([
      { id: 'p1', slug: 'one', enabled: true, souls: [], domain: 'kept', custom_domain: 'other', theme: 'auto' },
      { id: 'p2', slug: 'two', enabled: true, souls: [], custom_domain: 42, theme: null },
      { id: 'p3', slug: 'three', enabled: true, souls: [], theme: { background_color: '#f8fafc' } },
    ])) as typeof fetch
    const result = await api.get<Array<Record<string, unknown>>>('/status-pages')
    expect(result[0]).toMatchObject({ domain: 'kept', theme: 'auto' })
    expect(result[1].domain).toBeUndefined()
    expect(result[2].theme).toBe('light')
  })

  it('serializes protocol aliases, existing configs, strings, arrays, and primitives', async () => {
    global.fetch = vi.fn().mockResolvedValue(response({ ok: true }, 201)) as typeof fetch
    await api.post('/batch', [
      { type: 'tcp', target: 'x', weight: '1m', timeout: '2s', tcp_config: { send: 'a' } },
      { type: 'tcp', target: 'default' },
      { type: 'dns', target: 'x', dns_config: { record_type: 'AAAA' } },
      { type: 'http', target: 'x', http: { method: 'HEAD' }, http_config: { method: 'POST' } },
      { type: 'udp', target: 'x', udp: { send_hex: 'ff' } },
      { type: 'smtp', target: 'x', smtp: { starttls: true } },
      { type: 'icmp', target: 'x', icmp: { count: 1 } },
      { type: 'grpc', target: 'x', grpc: { metadata: { a: 'b' } } },
      { type: 'websocket', target: 'x', websocket: { ping_check: false } },
      { type: 'tls', target: 'x', tls: { expiry_warn_days: 1 } },
      7,
    ])
    const body = bodies()[0]
    expect(body[0]).toMatchObject({ tcp: { send: 'a' }, weight: '1m', timeout: '2s' })
    expect(body[1]).toMatchObject({ tcp: {} })
    expect(body[2]).toMatchObject({ dns: { record_type: 'AAAA' } })
    expect(body[3].http).toEqual({ method: 'HEAD' })
    expect(body[10]).toBe(7)
  })

  it('serializes journey string durations and rule string/default values with existing defaults', async () => {
    global.fetch = vi.fn().mockResolvedValue(response({ ok: true }, 201)) as typeof fetch
    await api.post('/journeys', { id: 'j', steps: [], weight: '1m', timeout: '30s' })
    await api.post('/journeys', { id: 'minimal', steps: [] })
    await api.post('/rules', { condition: 'response_time', threshold: '25', duration: '15', consecutive: '2', scope: { type: 'soul' }, cooldown: '1m' })
    await api.post('/rules', { condition: 'downtime', threshold: '', duration: '', consecutive: '' })
    const [journey, minimal, first, second] = bodies()
    expect(journey).toMatchObject({ weight: '1m', timeout: '30s' })
    expect(minimal).toMatchObject({ steps: [] })
    expect(first).toMatchObject({ scope: { type: 'soul' }, cooldown: '1m' })
    expect(first.conditions[0]).toMatchObject({ value: 25, window: '15s' })
    expect(second.conditions[0]).toMatchObject({ threshold: 1, window: '60s' })
  })

  it('covers silent token clearing and all 401 endpoint/error fallbacks', async () => {
    const listener = vi.fn()
    window.addEventListener('anubis:auth-session-changed', listener)
    api.setToken('token')
    listener.mockClear()
    api.clearToken(false)
    expect(listener).not.toHaveBeenCalled()

    Object.defineProperty(window, 'location', { value: { href: '' }, configurable: true })
    global.fetch = vi.fn()
      .mockResolvedValueOnce(response({ error: 'no me' }, 401, false))
      .mockResolvedValueOnce(response({ error: 'no login' }, 401, false))
      .mockResolvedValueOnce({ ok: false, status: 503, json: vi.fn().mockRejectedValue(new Error('invalid json')) }) as typeof fetch
    await expect(api.get('/auth/me')).rejects.toThrow('no me')
    await expect(api.post('/auth/login')).rejects.toThrow('no login')
    expect(window.location.href).toBe('')
    await expect(api.get('/down')).rejects.toThrow('Unknown error')
    window.removeEventListener('anubis:auth-session-changed', listener)
  })
})
