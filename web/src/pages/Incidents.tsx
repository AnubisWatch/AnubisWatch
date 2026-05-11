import { useState, useEffect } from 'react'
import { AlertTriangle, CheckCircle, Clock, User, Search, Filter } from 'lucide-react'
import { api } from '../api/client'

interface Incident {
  id: string
  rule_id: string
  soul_id: string
  soul_name?: string
  workspace_id: string
  status: 'open' | 'acknowledged' | 'resolved'
  severity: 'critical' | 'warning' | 'info'
  started_at: string
  acked_at?: string
  resolved_at?: string
  acked_by?: string
  resolved_by?: string
  escalation_level: number
}

export function Incidents() {
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<string>('all')
  const [severityFilter, setSeverityFilter] = useState<string>('all')

  useEffect(() => {
    fetchIncidents()
  }, [])

  const fetchIncidents = async () => {
    setLoading(true)
    try {
      const result = await api.get<Incident[]>('/incidents')
      setIncidents(result ?? [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load incidents')
    } finally {
      setLoading(false)
    }
  }

  const handleAcknowledge = async (id: string) => {
    try {
      await api.post(`/incidents/${id}/acknowledge`)
      await fetchIncidents()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to acknowledge incident')
    }
  }

  const handleResolve = async (id: string) => {
    try {
      await api.post(`/incidents/${id}/resolve`)
      await fetchIncidents()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to resolve incident')
    }
  }

  const filtered = incidents.filter((inc) => {
    if (statusFilter !== 'all' && inc.status !== statusFilter) return false
    if (severityFilter !== 'all' && inc.severity !== severityFilter) return false
    if (search) {
      const q = search.toLowerCase()
      return (
        inc.soul_name?.toLowerCase().includes(q) ||
        inc.id.toLowerCase().includes(q) ||
        inc.soul_id.toLowerCase().includes(q)
      )
    }
    return true
  })

  const severityColors: Record<string, string> = {
    critical: 'text-red-400 bg-red-500/10 border-red-500/20',
    warning: 'text-amber-400 bg-amber-500/10 border-amber-500/20',
    info: 'text-blue-400 bg-blue-500/10 border-blue-500/20',
  }

  const statusIcons: Record<string, { icon: typeof AlertTriangle; color: string }> = {
    open: { icon: AlertTriangle, color: 'text-red-400' },
    acknowledged: { icon: Clock, color: 'text-amber-400' },
    resolved: { icon: CheckCircle, color: 'text-emerald-400' },
  }

  const formatDate = (dateStr?: string) => {
    if (!dateStr) return '—'
    return new Date(dateStr).toLocaleString()
  }

  const openCount = incidents.filter((i) => i.status === 'open').length
  const ackedCount = incidents.filter((i) => i.status === 'acknowledged').length
  const resolvedCount = incidents.filter((i) => i.status === 'resolved').length
  const criticalCount = incidents.filter((i) => i.severity === 'critical').length

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-cinzel font-bold gradient-gold-shine tracking-wider">Cries of Chaos</h1>
        <p className="text-gray-400 mt-1 font-cormorant italic">Track the disturbances in the realm</p>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4">
          <p className="text-gray-400 text-sm">Open</p>
          <p className="text-2xl font-bold text-red-400">{openCount}</p>
        </div>
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4">
          <p className="text-gray-400 text-sm">Acknowledged</p>
          <p className="text-2xl font-bold text-amber-400">{ackedCount}</p>
        </div>
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4">
          <p className="text-gray-400 text-sm">Resolved</p>
          <p className="text-2xl font-bold text-emerald-400">{resolvedCount}</p>
        </div>
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-4">
          <p className="text-gray-400 text-sm">Critical</p>
          <p className="text-2xl font-bold text-red-400">{criticalCount}</p>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap gap-3">
        <div className="flex-1 min-w-[200px]">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search incidents..."
              className="w-full pl-10 pr-4 py-2 bg-gray-900 border border-gray-700/50 rounded-xl text-white text-sm focus:border-amber-500/50 focus:outline-none"
            />
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Filter className="w-4 h-4 text-gray-500" />
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className="bg-gray-900 border border-gray-700/50 rounded-xl text-white text-sm px-3 py-2 focus:border-amber-500/50 focus:outline-none"
          >
            <option value="all">All Status</option>
            <option value="open">Open</option>
            <option value="acknowledged">Acknowledged</option>
            <option value="resolved">Resolved</option>
          </select>
          <select
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value)}
            className="bg-gray-900 border border-gray-700/50 rounded-xl text-white text-sm px-3 py-2 focus:border-amber-500/50 focus:outline-none"
          >
            <option value="all">All Severity</option>
            <option value="critical">Critical</option>
            <option value="warning">Warning</option>
            <option value="info">Info</option>
          </select>
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="p-4 bg-red-500/10 border border-red-500/20 rounded-xl text-red-400 text-sm">
          {error}
        </div>
      )}

      {/* Loading */}
      {loading && (
        <div className="flex justify-center py-12">
          <div className="w-8 h-8 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
        </div>
      )}

      {/* Incidents list */}
      {!loading && (
        <div className="space-y-3">
          {filtered.length === 0 ? (
            <div className="text-center py-16 bg-gray-900/50 border border-gray-800 rounded-2xl">
              <AlertTriangle className="w-12 h-12 text-gray-600 mx-auto mb-3" />
              <p className="text-gray-400 font-medium">No incidents found</p>
              <p className="text-gray-500 text-sm mt-1">All systems are running smoothly</p>
            </div>
          ) : (
            filtered.map((inc) => {
              const StatusIcon = statusIcons[inc.status]?.icon || AlertTriangle
              const statusColor = statusIcons[inc.status]?.color || 'text-gray-400'
              return (
                <div
                  key={inc.id}
                  className={`bg-gradient-to-br from-gray-900 to-gray-800/50 border rounded-2xl p-5 ${severityColors[inc.severity] || severityColors.info}`}
                >
                  <div className="flex items-start justify-between gap-4">
                    <div className="flex-1">
                      <div className="flex items-center gap-2 mb-2">
                        <StatusIcon className={`w-5 h-5 ${statusColor}`} />
                        <span className="font-semibold text-white">{inc.soul_name || inc.soul_id}</span>
                        <span className={`text-xs px-2 py-0.5 rounded-md border ${severityColors[inc.severity]}`}>
                          {inc.severity}
                        </span>
                        <span className={`text-xs px-2 py-0.5 rounded-md ${statusColor}`}>
                          {inc.status}
                        </span>
                      </div>

                      <div className="grid grid-cols-2 gap-x-6 gap-y-1 text-xs text-gray-400 mt-3">
                        <div>
                          <span className="text-gray-500">Started:</span> {formatDate(inc.started_at)}
                        </div>
                        <div>
                          <span className="text-gray-500">Rule:</span> {inc.rule_id}
                        </div>
                        {inc.acked_at && (
                          <div>
                            <span className="text-gray-500">Acked:</span> {formatDate(inc.acked_at)}
                            {inc.acked_by && (
                              <span className="ml-1">by <User className="w-3 h-3 inline" /> {inc.acked_by}</span>
                            )}
                          </div>
                        )}
                        {inc.resolved_at && (
                          <div>
                            <span className="text-gray-500">Resolved:</span> {formatDate(inc.resolved_at)}
                            {inc.resolved_by && (
                              <span className="ml-1">by {inc.resolved_by}</span>
                            )}
                          </div>
                        )}
                        {inc.escalation_level > 0 && (
                          <div>
                            <span className="text-gray-500">Escalation:</span> Level {inc.escalation_level}
                          </div>
                        )}
                      </div>
                    </div>

                    {/* Actions */}
                    <div className="flex flex-col gap-2 shrink-0">
                      {inc.status === 'open' && (
                        <button
                          onClick={() => handleAcknowledge(inc.id)}
                          className="px-3 py-1.5 text-xs font-medium bg-amber-500/10 text-amber-400 border border-amber-500/20 rounded-lg hover:bg-amber-500/20 transition-colors"
                        >
                          Acknowledge
                        </button>
                      )}
                      {(inc.status === 'open' || inc.status === 'acknowledged') && (
                        <button
                          onClick={() => handleResolve(inc.id)}
                          className="px-3 py-1.5 text-xs font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-lg hover:bg-emerald-500/20 transition-colors"
                        >
                          Resolve
                        </button>
                      )}
                    </div>
                  </div>
                </div>
              )
            })
          )}
        </div>
      )}
    </div>
  )
}
