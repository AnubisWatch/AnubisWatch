import { api, ApiError } from './client'
import type { components } from './schema.generated'
import { formatDateTime } from '../utils/date'

type SLASchemas = components['schemas']

/** GET /reports/sla: one soul, or the workspace total (no soul_id). */
export type SLAReport = SLASchemas['SLAReport']
/** Latency percentiles over a report period. exact is false when rollup averages stood in for checks. */
export type SLALatency = NonNullable<SLAReport['latency']>
export type SLAErrorBudget = NonNullable<SLAReport['error_budget']>
export type SLAResponse = SLASchemas['SLAResponse']

export interface ReportPeriod {
  from: Date
  to: Date
}

export type ReportPreset = '7d' | '30d' | '90d' | '1y' | 'custom'

const PRESET_DAYS: Record<Exclude<ReportPreset, 'custom'>, number> = { '7d': 7, '30d': 30, '90d': 90, '1y': 365 }

/** The period a preset covers, ending at now. */
export function presetPeriod(preset: Exclude<ReportPreset, 'custom'>, now: Date = new Date()): ReportPeriod {
  return { from: new Date(now.getTime() - PRESET_DAYS[preset] * 24 * 3600 * 1000), to: now }
}

/** Query string for a report or export request. */
export function reportQuery(period: ReportPeriod, extra: Record<string, string | number | undefined> = {}): string {
  const q = new URLSearchParams({ from: period.from.toISOString(), to: period.to.toISOString() })
  for (const [key, value] of Object.entries(extra)) {
    if (value !== undefined && value !== '') q.set(key, String(value))
  }
  return q.toString()
}

export function fetchSLAReport(period: ReportPeriod, target?: number, soulId?: string): Promise<SLAResponse> {
  return api.get<SLAResponse>(`/reports/sla?${reportQuery(period, { target, soul_id: soulId })}`)
}

/** File name from a Content-Disposition header, or fallback. */
export function filenameFromDisposition(header: string | null, fallback: string): string {
  const match = header?.match(/filename="([^"]+)"/)
  return match ? match[1] : fallback
}

/**
 * Downloads an authenticated API response as a file. A plain link cannot
 * carry the bearer token the API also accepts, so the body is fetched and
 * handed to the browser as a blob.
 */
export async function downloadReport(endpoint: string, fallbackName: string): Promise<void> {
  const headers: Record<string, string> = {}
  const token = localStorage.getItem('auth_token')
  if (token) headers['Authorization'] = `Bearer ${token}`
  const response = await fetch(`/api/v1${endpoint}`, { headers, credentials: 'include' })
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: `Download failed (HTTP ${response.status})` }))
    throw new ApiError(response.status, body)
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  try {
    const a = document.createElement('a')
    a.href = url
    a.download = filenameFromDisposition(response.headers.get('Content-Disposition'), fallbackName)
    document.body.appendChild(a)
    a.click()
    a.remove()
  } finally {
    URL.revokeObjectURL(url)
  }
}

/** Human duration for seconds: 0s, 42s, 5m 12s, 3h 4m, 2d 5h. */
export function formatDuration(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds)) return '—'
  const negative = seconds < 0
  let s = Math.round(Math.abs(seconds))
  const d = Math.floor(s / 86400)
  s -= d * 86400
  const h = Math.floor(s / 3600)
  s -= h * 3600
  const m = Math.floor(s / 60)
  s -= m * 60
  const text = d > 0 ? `${d}d ${h}h` : h > 0 ? `${h}h ${m}m` : m > 0 ? `${m}m ${s}s` : `${s}s`
  return negative ? `-${text}` : text
}

/** Local midnight of a YYYY-MM-DD input value. */
function parseDateInput(value: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!m) return null
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return Number.isNaN(d.getTime()) ? null : d
}

/**
 * Validates a custom range: whole local days from `from` through `to`,
 * ending no later than now and starting no earlier than the retention
 * horizon (when known).
 */
export function customPeriod(fromValue: string, toValue: string, now: Date, horizon: Date | null): ReportPeriod | string {
  const from = parseDateInput(fromValue)
  const toDay = parseDateInput(toValue)
  if (!from || !toDay) return 'Pick a start and an end date.'
  let to = new Date(toDay.getFullYear(), toDay.getMonth(), toDay.getDate() + 1)
  if (to > now) to = now
  if (from >= to) return 'The start must be before the end, and not in the future.'
  if (horizon && to <= horizon) return `Data is only kept from ${formatDateTime(horizon)}.`
  return { from: horizon && from < horizon ? horizon : from, to }
}
