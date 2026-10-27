import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Reports } from './Reports'
import type { SLAReport, SLAResponse } from '../api/reports'

const mockGet = vi.fn()

vi.mock('../api/client', () => ({
  api: { get: (...args: unknown[]) => mockGet(...args) },
  ApiError: class ApiError extends Error {
    status: number
    constructor(status: number, body: { error?: string } = {}) {
      super(body.error || `HTTP ${status}`)
      this.status = status
    }
  },
}))

function soulReport(over: Partial<SLAReport>): SLAReport {
  return {
    period_seconds: 2592000,
    excluded_seconds: 0,
    monitored_seconds: 2592000,
    no_data_seconds: 0,
    checks: 1000,
    failed: 1,
    availability_percent: 99.9,
    downtime_seconds: 120,
    incidents: 0,
    mttr_seconds: null,
    latency: { avg_ms: 40, p50_ms: 35, p95_ms: 80.4, p99_ms: 120, exact: true },
    sources: { raw: 2, hour: 700, day: 0 },
    ...over,
  }
}

function response(target: number | null): SLAResponse {
  const budget = (remaining: number | null, met: boolean) =>
    target === null
      ? undefined
      : {
          target_percent: target,
          allowed_downtime_seconds: 2592,
          consumed_downtime_seconds: 120,
          remaining_downtime_seconds: 2472,
          remaining_percent: remaining,
          met,
        }
  return {
    from: '2026-09-27T12:00:00Z',
    to: '2026-10-27T12:00:00Z',
    generated_at: '2026-10-27T12:00:00Z',
    target_percent: target,
    retention_horizon: '2026-01-01T00:00:00Z',
    clipped: false,
    summary: soulReport({ checks: 2000, failed: 51, availability_percent: 97.45, incidents: 3, mttr_seconds: 600, error_budget: budget(-150, false) }),
    souls: [
      soulReport({ soul_id: 'api', soul_name: 'Payments API', soul_type: 'http', error_budget: budget(95.4, true) }),
      soulReport({
        soul_id: 'db', soul_name: 'Database', soul_type: 'tcp', failed: 50, availability_percent: 95,
        incidents: 3, mttr_seconds: 600, latency: { avg_ms: 5, p50_ms: 4, p95_ms: 9, p99_ms: 12, exact: false },
        error_budget: budget(-300, false),
      }),
      soulReport({ soul_id: 'idle', soul_name: 'Idle', checks: 0, failed: 0, availability_percent: null, latency: null, downtime_seconds: 0 }),
    ],
  }
}

function lastQuery(): URLSearchParams {
  const url = mockGet.mock.calls[mockGet.mock.calls.length - 1][0] as string
  return new URLSearchParams(url.split('?')[1])
}

function renderPage() {
  return render(
    <MemoryRouter>
      <Reports />
    </MemoryRouter>,
  )
}

describe('Reports', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockImplementation((url: string) => {
      const target = new URLSearchParams(url.split('?')[1]).get('target')
      return Promise.resolve(response(target === null ? null : Number(target)))
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the workspace summary and one row per soul with its budget', async () => {
    renderPage()
    expect(screen.getByRole('heading', { name: "Scrolls of Ma'at" })).toBeInTheDocument()
    const row = (await screen.findByRole('link', { name: 'Database' })).closest('tr') as HTMLElement
    expect(within(row).getByText('95.000%')).toBeInTheDocument()
    expect(within(row).getByText('-300.0%')).toBeInTheDocument()
    expect(within(row).getByText('9 ms*')).toBeInTheDocument()
    expect(within(row).getByText('10m 0s')).toBeInTheDocument()

    const idle = screen.getByRole('link', { name: 'Idle' }).closest('tr') as HTMLElement
    expect(within(idle).getByText('no data')).toBeInTheDocument()

    expect(screen.getByText('97.450%')).toBeInTheDocument()
    expect(screen.getByText('-150.0%')).toBeInTheDocument()
    const q = lastQuery()
    expect(q.get('target')).toBe('99.9')
    expect(Date.parse(q.get('to')!) - Date.parse(q.get('from')!)).toBe(30 * 86400000)
  })

  it.each([
    ['7 days', 7],
    ['90 days', 90],
    ['1 year', 365],
  ])('the %s preset requests that period', async (label, days) => {
    renderPage()
    await screen.findByRole('link', { name: 'Database' })
    fireEvent.click(screen.getByRole('button', { name: label }))
    await waitFor(() => {
      const q = lastQuery()
      expect(Date.parse(q.get('to')!) - Date.parse(q.get('from')!)).toBe(days * 86400000)
    })
    expect(screen.getByRole('button', { name: label })).toHaveAttribute('aria-pressed', 'true')
  })

  it('validates and applies the SLO target', async () => {
    renderPage()
    await screen.findByRole('link', { name: 'Database' })
    const input = screen.getByLabelText('SLO target (%)')

    fireEvent.change(input, { target: { value: '120' } })
    fireEvent.click(screen.getByRole('button', { name: /Set target/ }))
    expect(screen.getByRole('alert')).toHaveTextContent('at most 100')

    fireEvent.change(input, { target: { value: '99.95' } })
    fireEvent.click(screen.getByRole('button', { name: /Set target/ }))
    await waitFor(() => expect(lastQuery().get('target')).toBe('99.95'))

    fireEvent.change(input, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: /Set target/ }))
    await waitFor(() => expect(lastQuery().has('target')).toBe(false))
    expect(await screen.findByText('Set a target to see it')).toBeInTheDocument()
  })

  it('limits the custom range and rejects an inverted one', async () => {
    renderPage()
    await screen.findByRole('link', { name: 'Database' })
    fireEvent.click(screen.getByRole('button', { name: 'Custom' }))
    const from = screen.getByLabelText('From')
    expect(from).toHaveAttribute('min', '2026-01-01')

    fireEvent.change(from, { target: { value: '2026-10-10' } })
    fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-10-01' } })
    const calls = mockGet.mock.calls.length
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    expect(screen.getByRole('alert')).toHaveTextContent('before the end')
    expect(mockGet.mock.calls.length).toBe(calls)

    fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-10-12' } })
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() => expect(new Date(lastQuery().get('from')!).getDate()).toBe(10))
  })

  it('downloads the SLA CSV and a soul export through the API', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('x', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:x'), revokeObjectURL: vi.fn() }))
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    renderPage()
    await screen.findByRole('link', { name: 'Database' })
    fireEvent.click(screen.getByRole('button', { name: 'Download CSV' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(fetchMock.mock.calls[0][0]).toMatch(/^\/api\/v1\/reports\/sla\.csv\?from=.*&target=99\.9$/)

    fireEvent.click(await screen.findByRole('button', { name: 'Export judgments of Database as NDJSON' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(fetchMock.mock.calls[1][0]).toMatch(/^\/api\/v1\/souls\/db\/judgments\/export\?from=.*&format=ndjson$/)
  })

  it('shows a failed download or report', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'role lacks permission' }), { status: 403 })))
    renderPage()
    await screen.findByRole('link', { name: 'Database' })
    fireEvent.click(screen.getByRole('button', { name: 'Download CSV' }))
    expect(await screen.findByText('role lacks permission')).toBeInTheDocument()

    mockGet.mockRejectedValueOnce(new Error('server down'))
    fireEvent.click(screen.getByRole('button', { name: '7 days' }))
    expect(await screen.findByText('server down')).toBeInTheDocument()
  })
})
