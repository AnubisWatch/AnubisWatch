import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Download, FileText, Target, AlertTriangle, Clock, Activity, ShieldCheck } from 'lucide-react'
import {
  customPeriod,
  downloadReport,
  fetchSLAReport,
  formatDuration,
  presetPeriod,
  reportQuery,
  type ReportPeriod,
  type ReportPreset,
  type SLAReport,
  type SLAResponse,
} from '../api/reports'
import { formatDateTime, useDisplayTimeZone } from '../utils/date'

const PRESETS: Array<{ id: Exclude<ReportPreset, 'custom'>; label: string }> = [
  { id: '7d', label: '7 days' },
  { id: '30d', label: '30 days' },
  { id: '90d', label: '90 days' },
  { id: '1y', label: '1 year' },
]

/** YYYY-MM-DD of a date in local time, for <input type="date">. */
function dateInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function percent(v: number | null | undefined, digits = 3): string {
  return v === null || v === undefined ? '—' : `${v.toFixed(digits)}%`
}

function availabilityColor(v: number | null, target: number | null): string {
  if (v === null) return 'text-gray-500'
  const goal = target ?? 99.9
  if (v >= goal) return 'text-emerald-400'
  if (v >= goal - 1) return 'text-amber-400'
  return 'text-red-400'
}

function BudgetCell({ report }: { report: SLAReport }) {
  const budget = report.error_budget
  if (!budget) return <span className="text-gray-500">—</span>
  const remaining = budget.remaining_percent
  const color = !budget.met ? 'text-red-400' : remaining !== null && remaining < 25 ? 'text-amber-400' : 'text-emerald-400'
  return (
    <span className={color} title={`${formatDuration(budget.remaining_downtime_seconds)} of ${formatDuration(budget.allowed_downtime_seconds)} left`}>
      {remaining === null ? (budget.met ? 'met' : 'missed') : `${remaining.toFixed(1)}%`}
    </span>
  )
}

export function Reports() {
  useDisplayTimeZone()
  const [preset, setPreset] = useState<ReportPreset>('30d')
  const [period, setPeriod] = useState<ReportPeriod>(() => presetPeriod('30d'))
  const [customFrom, setCustomFrom] = useState(() => dateInputValue(new Date(Date.now() - 30 * 86400000)))
  const [customTo, setCustomTo] = useState(() => dateInputValue(new Date()))
  const [targetInput, setTargetInput] = useState('99.9')
  const [target, setTarget] = useState<number | undefined>(99.9)
  const [report, setReport] = useState<SLAResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [downloading, setDownloading] = useState<string | null>(null)
  const sequence = useRef(0)

  useEffect(() => {
    const seq = ++sequence.current
    setLoading(true)
    setError(null)
    fetchSLAReport(period, target)
      .then((resp) => {
        if (seq === sequence.current) setReport(resp)
      })
      .catch((err: unknown) => {
        if (seq === sequence.current) setError(err instanceof Error ? err.message : 'Failed to load the SLA report')
      })
      .finally(() => {
        if (seq === sequence.current) setLoading(false)
      })
  }, [period, target])

  const horizon = report?.retention_horizon ? new Date(report.retention_horizon) : null

  const choosePreset = (id: Exclude<ReportPreset, 'custom'>) => {
    setPreset(id)
    setFormError(null)
    setPeriod(presetPeriod(id))
  }

  const applyCustom = () => {
    const result = customPeriod(customFrom, customTo, new Date(), horizon)
    if (typeof result === 'string') {
      setFormError(result)
      return
    }
    setFormError(null)
    setPeriod(result)
  }

  const applyTarget = () => {
    const trimmed = targetInput.trim()
    if (trimmed === '') {
      setFormError(null)
      setTarget(undefined)
      return
    }
    const value = Number(trimmed)
    if (!Number.isFinite(value) || value <= 0 || value > 100) {
      setFormError('The SLO target must be a percentage above 0 and at most 100, e.g. 99.9.')
      return
    }
    setFormError(null)
    setTarget(value)
  }

  const download = async (key: string, endpoint: string, fallback: string) => {
    setDownloading(key)
    setError(null)
    try {
      await downloadReport(endpoint, fallback)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Download failed')
    } finally {
      setDownloading(null)
    }
  }

  const summary = report?.summary
  const targetShown = report?.target_percent ?? null

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-3xl font-cinzel font-bold gradient-gold-shine tracking-wider">Scrolls of Ma'at</h1>
          <p className="text-gray-400 mt-1 font-cormorant italic">Service levels weighed against their promises</p>
        </div>
        <button
          type="button"
          onClick={() => download('sla', `/reports/sla.csv?${reportQuery(period, { target })}`, 'anubis-sla.csv')}
          disabled={downloading !== null}
          className="inline-flex items-center gap-2 px-4 py-2 rounded-xl bg-amber-500/15 border border-amber-500/30 text-amber-300 hover:bg-amber-500/25 disabled:opacity-50"
        >
          <Download className="w-4 h-4" />
          {downloading === 'sla' ? 'Preparing…' : 'Download CSV'}
        </button>
      </div>

      {/* Period and target */}
      <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4 space-y-4">
        <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Report period">
          {PRESETS.map((p) => (
            <button
              key={p.id}
              type="button"
              aria-pressed={preset === p.id}
              onClick={() => choosePreset(p.id)}
              className={`px-3 py-1.5 rounded-lg text-sm border ${preset === p.id ? 'bg-amber-500/20 border-amber-500/40 text-amber-300' : 'border-gray-700/50 text-gray-300 hover:bg-gray-800'}`}
            >
              {p.label}
            </button>
          ))}
          <button
            type="button"
            aria-pressed={preset === 'custom'}
            onClick={() => setPreset('custom')}
            className={`px-3 py-1.5 rounded-lg text-sm border ${preset === 'custom' ? 'bg-amber-500/20 border-amber-500/40 text-amber-300' : 'border-gray-700/50 text-gray-300 hover:bg-gray-800'}`}
          >
            Custom
          </button>
        </div>

        {preset === 'custom' && (
          <div className="flex flex-wrap items-end gap-3">
            <label className="text-sm text-gray-400">
              From
              <input
                type="date"
                value={customFrom}
                min={horizon ? dateInputValue(horizon) : undefined}
                max={dateInputValue(new Date())}
                onChange={(e) => setCustomFrom(e.target.value)}
                className="block mt-1 bg-gray-800 border border-gray-700/50 rounded-lg px-3 py-1.5 text-white"
              />
            </label>
            <label className="text-sm text-gray-400">
              To
              <input
                type="date"
                value={customTo}
                min={customFrom}
                max={dateInputValue(new Date())}
                onChange={(e) => setCustomTo(e.target.value)}
                className="block mt-1 bg-gray-800 border border-gray-700/50 rounded-lg px-3 py-1.5 text-white"
              />
            </label>
            <button type="button" onClick={applyCustom} className="px-4 py-1.5 rounded-lg bg-amber-500 text-gray-900 font-medium hover:bg-amber-400">
              Apply
            </button>
          </div>
        )}

        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            applyTarget()
          }}
        >
          <label className="text-sm text-gray-400">
            SLO target (%)
            <input
              type="text"
              inputMode="decimal"
              value={targetInput}
              placeholder="e.g. 99.9"
              onChange={(e) => setTargetInput(e.target.value)}
              className="block mt-1 w-32 bg-gray-800 border border-gray-700/50 rounded-lg px-3 py-1.5 text-white"
            />
          </label>
          <button type="submit" className="inline-flex items-center gap-2 px-4 py-1.5 rounded-lg border border-gray-700/50 text-gray-200 hover:bg-gray-800">
            <Target className="w-4 h-4" /> Set target
          </button>
          {horizon && <p className="text-xs text-gray-500">Data is kept from {formatDateTime(horizon)}.</p>}
        </form>
        {formError && <p role="alert" className="text-sm text-red-400">{formError}</p>}
      </div>

      {error && (
        <div role="alert" className="p-4 rounded-xl bg-red-500/10 border border-red-500/20 text-red-300">
          {error}
        </div>
      )}

      {report && (
        <p className="text-sm text-gray-400">
          {formatDateTime(report.from)} → {formatDateTime(report.to)}
          {report.clipped && <span className="ml-2 text-amber-400">(clipped to what retention keeps)</span>}
        </p>
      )}

      {/* Summary */}
      {summary && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4">
            <p className="text-gray-400 text-sm flex items-center gap-1"><Activity className="w-4 h-4" /> Availability</p>
            <p className={`text-2xl font-bold ${availabilityColor(summary.availability_percent, targetShown)}`}>{percent(summary.availability_percent)}</p>
            <p className="text-xs text-gray-500">{summary.checks.toLocaleString()} checks, {summary.failed.toLocaleString()} failed</p>
          </div>
          <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4">
            <p className="text-gray-400 text-sm flex items-center gap-1"><Clock className="w-4 h-4" /> Downtime</p>
            <p className="text-2xl font-bold text-white">{formatDuration(summary.downtime_seconds)}</p>
            <p className="text-xs text-gray-500">{formatDuration(summary.excluded_seconds)} maintenance excluded</p>
          </div>
          <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4">
            <p className="text-gray-400 text-sm flex items-center gap-1"><AlertTriangle className="w-4 h-4" /> Incidents</p>
            <p className="text-2xl font-bold text-white">{summary.incidents}</p>
            <p className="text-xs text-gray-500">MTTR {formatDuration(summary.mttr_seconds)}</p>
          </div>
          <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4">
            <p className="text-gray-400 text-sm flex items-center gap-1"><ShieldCheck className="w-4 h-4" /> Error budget</p>
            <p className="text-2xl font-bold"><BudgetCell report={summary} /></p>
            <p className="text-xs text-gray-500">{targetShown === null ? 'Set a target to see it' : `left of a ${targetShown}% target`}</p>
          </div>
        </div>
      )}

      {/* Per soul */}
      <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-gray-400 border-b border-gray-700/50">
              <th className="px-4 py-3 font-medium">Soul</th>
              <th className="px-4 py-3 font-medium">Availability</th>
              <th className="px-4 py-3 font-medium">Checks</th>
              <th className="px-4 py-3 font-medium">Failed</th>
              <th className="px-4 py-3 font-medium">Downtime</th>
              <th className="px-4 py-3 font-medium">Incidents</th>
              <th className="px-4 py-3 font-medium">MTTR</th>
              <th className="px-4 py-3 font-medium">p95</th>
              <th className="px-4 py-3 font-medium">Budget left</th>
              <th className="px-4 py-3 font-medium">Export</th>
            </tr>
          </thead>
          <tbody>
            {loading && !report && (
              <tr><td colSpan={10} className="px-4 py-8 text-center text-gray-500">Weighing the souls…</td></tr>
            )}
            {report && report.souls.length === 0 && (
              <tr><td colSpan={10} className="px-4 py-8 text-center text-gray-500">No souls in this workspace yet.</td></tr>
            )}
            {report?.souls.map((s) => (
              <tr key={s.soul_id} className="border-b border-gray-800/60 hover:bg-gray-800/30">
                <td className="px-4 py-3">
                  <Link to={`/souls/${s.soul_id}`} className="text-white hover:text-amber-300">{s.soul_name || s.soul_id}</Link>
                  {s.soul_type && <span className="ml-2 text-xs text-gray-500 uppercase">{s.soul_type}</span>}
                </td>
                <td className={`px-4 py-3 font-medium ${availabilityColor(s.availability_percent, targetShown)}`}>
                  {s.availability_percent === null ? <span title="No checks in this period">no data</span> : percent(s.availability_percent)}
                </td>
                <td className="px-4 py-3 text-gray-300">{s.checks.toLocaleString()}</td>
                <td className="px-4 py-3 text-gray-300">{s.failed.toLocaleString()}</td>
                <td className="px-4 py-3 text-gray-300">{formatDuration(s.downtime_seconds)}</td>
                <td className="px-4 py-3 text-gray-300">{s.incidents}</td>
                <td className="px-4 py-3 text-gray-300">{formatDuration(s.mttr_seconds)}</td>
                <td className="px-4 py-3 text-gray-300" title={s.latency && !s.latency.exact ? 'Approximate: computed from rollups' : undefined}>
                  {s.latency ? `${Math.round(s.latency.p95_ms)} ms${s.latency.exact ? '' : '*'}` : '—'}
                </td>
                <td className="px-4 py-3"><BudgetCell report={s} /></td>
                <td className="px-4 py-3">
                  <div className="flex gap-2">
                    {(['csv', 'ndjson'] as const).map((format) => (
                      <button
                        key={format}
                        type="button"
                        disabled={downloading !== null}
                        aria-label={`Export judgments of ${s.soul_name || s.soul_id} as ${format.toUpperCase()}`}
                        onClick={() =>
                          download(
                            `${s.soul_id}-${format}`,
                            `/souls/${encodeURIComponent(s.soul_id ?? '')}/judgments/export?${reportQuery(period, { format })}`,
                            `anubis-judgments-${s.soul_id}.${format}`,
                          )
                        }
                        className="inline-flex items-center gap-1 px-2 py-1 rounded-md border border-gray-700/50 text-xs text-gray-300 hover:bg-gray-800 disabled:opacity-50"
                      >
                        <FileText className="w-3 h-3" />
                        {format.toUpperCase()}
                      </button>
                    ))}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <p className="text-xs text-gray-500">
        Availability counts alive checks over all checks. Downtime is estimated per hour from the share of failed checks.
        Enabled maintenance windows are excluded time. Long periods are read from hourly and daily rollups; p95 values marked * are
        approximate. Judgment exports are limited to raw-judgment retention and 100,000 rows per file.
      </p>
    </div>
  )
}
