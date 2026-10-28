import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  customPeriod,
  downloadReport,
  fetchSLAReport,
  filenameFromDisposition,
  formatDuration,
  presetPeriod,
  reportQuery,
} from './reports'

const mockGet = vi.fn()

vi.mock('./client', () => ({
  api: { get: (...args: unknown[]) => mockGet(...args) },
  ApiError: class ApiError extends Error {
    status: number
    constructor(status: number, body: { error?: string } = {}) {
      super(body.error || `HTTP ${status}`)
      this.status = status
    }
  },
}))

describe('report helpers', () => {
  it.each([
    ['7d', 7],
    ['30d', 30],
    ['90d', 90],
    ['1y', 365],
  ] as const)('preset %s covers %i days ending now', (preset, days) => {
    const now = new Date('2026-10-27T12:00:00Z')
    const p = presetPeriod(preset, now)
    expect(p.to).toBe(now)
    expect(now.getTime() - p.from.getTime()).toBe(days * 86400000)
  })

  it('builds queries with ISO bounds and skips empty values', () => {
    const q = new URLSearchParams(
      reportQuery({ from: new Date('2026-10-01T00:00:00Z'), to: new Date('2026-10-02T00:00:00Z') }, { target: 99.9, soul_id: undefined, format: '' }),
    )
    expect(q.get('from')).toBe('2026-10-01T00:00:00.000Z')
    expect(q.get('to')).toBe('2026-10-02T00:00:00.000Z')
    expect(q.get('target')).toBe('99.9')
    expect(q.has('soul_id')).toBe(false)
    expect(q.has('format')).toBe(false)
  })

  it.each([
    [null, '—'],
    [0, '0s'],
    [42.4, '42s'],
    [312, '5m 12s'],
    [11040, '3h 4m'],
    [190800, '2d 5h'],
    [-600, '-10m 0s'],
  ])('formatDuration(%s) = %s', (input, want) => {
    expect(formatDuration(input as number | null)).toBe(want)
  })

  it('reads the file name from Content-Disposition', () => {
    expect(filenameFromDisposition('attachment; filename="anubis-sla-1-2.csv"', 'x.csv')).toBe('anubis-sla-1-2.csv')
    expect(filenameFromDisposition(null, 'x.csv')).toBe('x.csv')
    expect(filenameFromDisposition('inline', 'x.csv')).toBe('x.csv')
  })

  describe('customPeriod', () => {
    const now = new Date(2026, 9, 27, 15, 30)
    it.each([
      { name: 'whole days through the end date', from: '2026-10-01', to: '2026-10-10', horizon: null, want: [new Date(2026, 9, 1), new Date(2026, 9, 11)] },
      { name: 'ends at now when the end date is today', from: '2026-10-20', to: '2026-10-27', horizon: null, want: [new Date(2026, 9, 20), now] },
      { name: 'clipped to the retention horizon', from: '2026-01-01', to: '2026-10-10', horizon: new Date(2026, 5, 1), want: [new Date(2026, 5, 1), new Date(2026, 9, 11)] },
    ])('$name', ({ from, to, horizon, want }) => {
      const p = customPeriod(from, to, now, horizon)
      expect(typeof p).toBe('object')
      if (typeof p === 'object') {
        expect(p.from.getTime()).toBe(want[0].getTime())
        expect(p.to.getTime()).toBe(want[1].getTime())
      }
    })
    it.each([
      { name: 'missing dates', from: '', to: '2026-10-10', horizon: null, msg: /Pick a start/ },
      { name: 'inverted', from: '2026-10-10', to: '2026-10-01', horizon: null, msg: /before the end/ },
      { name: 'in the future', from: '2026-11-01', to: '2026-11-05', horizon: null, msg: /before the end/ },
      { name: 'entirely before retention', from: '2025-01-01', to: '2025-02-01', horizon: new Date(2026, 0, 1), msg: /only kept/ },
    ])('rejects $name', ({ from, to, horizon, msg }) => {
      expect(customPeriod(from, to, now, horizon)).toMatch(msg)
    })
  })

  it('fetches the SLA report with period, target and soul', async () => {
    mockGet.mockResolvedValue({ souls: [] })
    await fetchSLAReport({ from: new Date('2026-10-01T00:00:00Z'), to: new Date('2026-10-02T00:00:00Z') }, 99.95, 's1')
    const url = mockGet.mock.calls[0][0] as string
    expect(url.startsWith('/reports/sla?')).toBe(true)
    const q = new URLSearchParams(url.split('?')[1])
    expect(q.get('target')).toBe('99.95')
    expect(q.get('soul_id')).toBe('s1')
  })
})

describe('downloadReport', () => {
  const createObjectURL = vi.fn(() => 'blob:report')
  const revokeObjectURL = vi.fn()

  beforeEach(() => {
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL }))
    localStorage.setItem('auth_token', 'tok-1')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('fetches with the bearer token and saves under the server file name', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('a,b\n', { status: 200, headers: { 'Content-Disposition': 'attachment; filename="anubis-sla-x.csv"' } }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    let saved = ''
    click.mockImplementation(function (this: HTMLAnchorElement) {
      saved = this.download
    })

    await downloadReport('/reports/sla.csv?from=a', 'fallback.csv')

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/reports/sla.csv?from=a', {
      headers: { Authorization: 'Bearer tok-1' },
      credentials: 'include',
    })
    expect(saved).toBe('anubis-sla-x.csv')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:report')
  })

  it('surfaces the server error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'soul not found' }), { status: 404 })))
    await expect(downloadReport('/souls/x/judgments/export', 'x.csv')).rejects.toThrow('soul not found')
  })
})
