import { useEffect, useState } from 'react'
import { api, type QuotaCounter, type QuotaUsage } from '../api/client'

const rows: { key: 'souls' | 'journeys' | 'alert_channels' | 'team_members'; label: string }[] = [
  { key: 'souls', label: 'Souls' },
  { key: 'journeys', label: 'Journeys' },
  { key: 'alert_channels', label: 'Alert Channels' },
  { key: 'team_members', label: 'Team Members' },
]

function QuotaRow({ label, counter }: { label: string; counter: QuotaCounter }) {
  const limited = counter.limit > 0
  const ratio = limited ? Math.min(counter.used / counter.limit, 1) : 0
  const full = limited && counter.used >= counter.limit
  const bar = full ? 'bg-rose-500' : ratio >= 0.8 ? 'bg-amber-500' : 'bg-emerald-500'
  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm text-gray-400">{label}</span>
        <span className={`text-sm ${full ? 'text-rose-400' : 'text-white'}`}>
          {limited ? `${counter.used} / ${counter.limit}` : `${counter.used} (unlimited)`}
        </span>
      </div>
      {limited && (
        <div
          className="w-full h-2 bg-gray-800 rounded-full overflow-hidden"
          role="progressbar"
          aria-label={`${label} quota`}
          aria-valuemin={0}
          aria-valuemax={counter.limit}
          aria-valuenow={counter.used}
        >
          <div className={`h-full rounded-full ${bar}`} style={{ width: `${Math.round(ratio * 100)}%` }} />
        </div>
      )}
    </div>
  )
}

/** Usage of the current workspace against its quotas (GET /api/v1/quota). */
export function WorkspaceQuotaPanel() {
  const [usage, setUsage] = useState<QuotaUsage | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const result = await api.get<QuotaUsage>('/quota')
        if (!cancelled) setUsage(result)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load quota usage')
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [])

  const floor = usage?.check_interval_min && usage.check_interval_min !== '0s' ? usage.check_interval_min : null

  return (
    <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
      <h2 className="text-lg font-semibold text-white mb-1">Workspace Quotas</h2>
      <p className="text-gray-400 text-sm mb-6">
        {usage && !usage.enforced
          ? 'Quotas are not enforced on this server (tenants.enabled is off); nothing is limited.'
          : 'Resources this workspace holds against the limits set in tenants.default_quotas.'}
      </p>
      {error && <p role="alert" className="text-sm text-rose-400">{error}</p>}
      {!usage && !error && <p className="text-sm text-gray-500">Loading quota usage…</p>}
      {usage && (
        <div className="space-y-4">
          {rows.map((row) => {
            const counter = usage[row.key]
            return counter ? <QuotaRow key={row.key} label={row.label} counter={counter} /> : null
          })}
          {usage.enforced && (
            <dl className="pt-4 border-t border-gray-700/50 grid grid-cols-2 gap-4 text-sm">
              <div>
                <dt className="text-gray-400">Minimum check interval</dt>
                <dd className="text-white">{floor ?? 'No minimum'}</dd>
              </div>
              <div>
                <dt className="text-gray-400">History retention cap</dt>
                <dd className="text-white">{usage.retention_days > 0 ? `${usage.retention_days} days` : 'No cap'}</dd>
              </div>
            </dl>
          )}
        </div>
      )}
    </div>
  )
}
