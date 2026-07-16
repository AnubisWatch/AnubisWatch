import { useEffect, useState, useMemo } from 'react'
import {
  Plus,
  Ghost,
  Play,
  Pause,
  Trash2,
  Eye,
  Activity,
  Globe,
  Server,
  XCircle,
  Clock,
  RefreshCw,
  Wifi,
  CheckCircle2
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { useSoulStore } from '../stores/soulStore'
import type { Soul } from '../api/client'
import { SoulProtocolFields } from '../components/SoulProtocolFields'
import {
  buildSoulPayload,
  defaultSoulFormData,
  nextSoulFormDataForType,
  soulTargetHints,
  soulTypeOptions,
  type SoulFormData,
  type SoulType,
} from '../utils/soulForm'
import { getStatusColor, getStatusText, getStatusLabel } from '../utils/statusUtils'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { SoulStatsCards } from '../components/SoulStatsCards'
import { SoulFilterBar } from '../components/SoulFilterBar'
import { SoulCreateModal } from '../components/SoulCreateModal'

type SoulDisplayStatus = 'healthy' | 'unhealthy' | 'unknown' | 'checking' | 'check_failed'

// Extended Soul type with UI-specific properties
interface SoulWithStatus extends Soul {
  status?: 'healthy' | 'unhealthy' | 'unknown'
  last_check?: string
  latency?: number
}

const typeConfig: Record<SoulType, { label: string; color: string; bg: string; icon: typeof Wifi }> = {
  http: { label: 'HTTP', color: 'text-blue-400', bg: 'bg-blue-500/10', icon: Globe },
  tcp: { label: 'TCP', color: 'text-purple-400', bg: 'bg-purple-500/10', icon: Server },
  udp: { label: 'UDP', color: 'text-yellow-400', bg: 'bg-yellow-500/10', icon: Server },
  smtp: { label: 'SMTP', color: 'text-orange-400', bg: 'bg-orange-500/10', icon: Server },
  dns: { label: 'DNS', color: 'text-cyan-400', bg: 'bg-cyan-500/10', icon: Globe },
  icmp: { label: 'ICMP', color: 'text-pink-400', bg: 'bg-pink-500/10', icon: Activity },
  grpc: { label: 'gRPC', color: 'text-indigo-400', bg: 'bg-indigo-500/10', icon: Server },
  websocket: { label: 'WS', color: 'text-teal-400', bg: 'bg-teal-500/10', icon: Wifi },
  tls: { label: 'TLS', color: 'text-emerald-400', bg: 'bg-emerald-500/10', icon: Server },
}

export function Souls() {
  const { souls: rawSouls, initialChecks, fetchSouls, createSoul, retryInitialCheck, updateSoul, deleteSoul } = useSoulStore()
  const souls = rawSouls as SoulWithStatus[]
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('list')
  const [refreshing, setRefreshing] = useState(false)
  const [showModal, setShowModal] = useState(false)
  const [loading, setLoading] = useState(false)
  const [formErrors, setFormErrors] = useState<Record<string, string>>({})

  // Form state
  const [formData, setFormData] = useState<SoulFormData>(defaultSoulFormData)
  const [deletingSoul, setDeletingSoul] = useState<string | null>(null)

  useEffect(() => {
    fetchSouls()
  }, [fetchSouls])

  const handleRefresh = async () => {
    setRefreshing(true)
    await fetchSouls()
    setTimeout(() => setRefreshing(false), 500)
  }

  const handleCreateSoul = async (e: React.FormEvent) => {
    e.preventDefault()
    const errors: Record<string, string> = {}

    if (!formData.name.trim()) {
      errors.name = 'Name is required'
    } else if (formData.name.length < 2) {
      errors.name = 'Name must be at least 2 characters'
    }

    if (!formData.target.trim()) {
      errors.target = 'Target is required'
    } else if (formData.type === 'http' && !formData.target.match(/^https?:\/\/.+/)) {
      errors.target = 'HTTP target must start with http:// or https://'
    }

    if (Object.keys(errors).length > 0) {
      setFormErrors(errors)
      return
    }

    setFormErrors({})
    setLoading(true)
    try {
      await createSoul({
        ...buildSoulPayload(formData),
      })
      setShowModal(false)
      setFormData(defaultSoulFormData)
    } catch (err) {
      alert('Failed to create soul: ' + (err instanceof Error ? err.message : 'Unknown error'))
    } finally {
      setLoading(false)
    }
  }

  const handleDelete = async (id: string) => {
    setDeletingSoul(id)
  }

  const handleConfirmDelete = async () => {
    const id = deletingSoul
    if (!id) return
    setDeletingSoul(null)
    try {
      await deleteSoul(id)
    } catch (err) {
      alert('Failed to delete soul: ' + (err instanceof Error ? err.message : 'Unknown error'))
    }
  }

  const handleToggle = async (soul: SoulWithStatus) => {
    try {
      await updateSoul(soul.id, { enabled: !soul.enabled })
    } catch (err) {
      alert('Failed to update soul: ' + (err instanceof Error ? err.message : 'Unknown error'))
    }
  }

  const handleRetryInitialCheck = async (soul: SoulWithStatus) => {
    await retryInitialCheck(soul.id)
  }

  const filteredSouls = useMemo(() => {
    return souls.filter(soul => {
      const matchesSearch = soul.name.toLowerCase().includes(search.toLowerCase()) ||
                           soul.target.toLowerCase().includes(search.toLowerCase())
      const matchesFilter = filter === 'all' ||
                           (filter === 'enabled' && soul.enabled) ||
                           (filter === 'disabled' && !soul.enabled) ||
                           (filter === 'http' && soul.type === 'http') ||
                           (filter === 'tcp' && soul.type === 'tcp') ||
                           (filter === 'issues' && soul.status === 'unhealthy')
      return matchesSearch && matchesFilter
    })
  }, [souls, search, filter])

  const stats = useMemo(() => ({
    total: souls.length,
    active: souls.filter(s => s.enabled).length,
    disabled: souls.filter(s => !s.enabled).length,
    issues: souls.filter(s => s.status === 'unhealthy').length,
    types: new Set(souls.map(s => s.type)).size
  }), [souls])

  const getDisplayStatus = (soul: SoulWithStatus): SoulDisplayStatus => {
    const initialCheck = initialChecks[soul.id]
    if (initialCheck === 'running') return 'checking'
    if (initialCheck === 'failed') return 'check_failed'
    return soul.status ?? 'unknown'
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-cinzel font-bold gradient-gold-shine tracking-wider">Essence</h1>
          <p className="text-gray-400 mt-1 font-cormorant italic">The souls that dwell in your realm</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            className={`p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all ${refreshing ? 'animate-spin' : ''}`}
            aria-label="Refresh souls"
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          <button
            onClick={() => { setShowModal(true); setFormErrors({}) }}
            className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20"
          >
            <Plus className="w-4 h-4" />
            Add Soul
          </button>
        </div>
      </div>

      {/* Stats Grid */}
      <SoulStatsCards stats={stats} />

      {/* Filters */}
      <SoulFilterBar
        search={search}
        onSearchChange={setSearch}
        filter={filter}
        onFilterChange={setFilter}
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />

      {/* Content */}
      {viewMode === 'list' ? (
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden">
          <table className="w-full">
            <thead className="bg-gray-800/50">
              <tr>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Soul</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Status</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Type</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Target</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Latency</th>
                <th className="text-right text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-700/50">
              {filteredSouls.map((soul) => {
                const typeInfo = typeConfig[soul.type] || typeConfig.http
                const TypeIcon = typeInfo.icon
                const displayStatus = getDisplayStatus(soul)

                return (
                  <tr key={soul.id} className="hover:bg-gray-800/30 transition-colors group">
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-4">
                        <div className={`w-10 h-10 rounded-xl flex items-center justify-center ${soul.enabled ? typeInfo.bg : 'bg-gray-800'}`}>
                          <TypeIcon className={`w-5 h-5 ${soul.enabled ? typeInfo.color : 'text-gray-500'}`} />
                        </div>
                        <div>
                          <p className="font-semibold text-white">{soul.name}</p>
                          <div className="flex gap-1.5 mt-1.5">
                            {(soul.tags ?? []).slice(0, 2).map(tag => (
                              <span key={tag} className="text-[10px] uppercase tracking-wider bg-gray-800 text-gray-400 px-2 py-0.5 rounded-md font-medium">
                                {tag}
                              </span>
                            ))}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-2">
                        <div className={`w-2 h-2 rounded-full ${getStatusColor(displayStatus)}`} />
                        <span className={`text-sm font-medium ${getStatusText(displayStatus)}`}>
                          {getStatusLabel(displayStatus)}
                        </span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold ${typeInfo.bg} ${typeInfo.color}`}>
                        <TypeIcon className="w-3.5 h-3.5" />
                        {typeInfo.label}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-sm text-gray-400 font-mono">{soul.target}</span>
                    </td>
                    <td className="px-6 py-4">
                      {displayStatus === 'checking' ? (
                        <div className="flex items-center gap-2 text-amber-400">
                          <RefreshCw className="w-4 h-4 animate-spin" />
                          <span className="text-sm font-medium">Running</span>
                        </div>
                      ) : displayStatus === 'check_failed' ? (
                        <button
                          onClick={() => handleRetryInitialCheck(soul)}
                          className="inline-flex items-center gap-2 text-sm font-medium text-rose-400 hover:text-rose-300 transition-colors"
                          aria-label={`Retry initial check for ${soul.name || soul.target}`}
                        >
                          <RefreshCw className="w-4 h-4" />
                          Retry
                        </button>
                      ) : soul.latency ? (
                        <div className="flex items-center gap-2">
                          <Clock className="w-4 h-4 text-gray-500" />
                          <span className={`text-sm font-medium ${soul.latency > 1000 ? 'text-amber-400' : 'text-emerald-400'}`}>
                            {soul.latency}ms
                          </span>
                        </div>
                      ) : (
                        <span className="text-sm text-gray-500">-</span>
                      )}
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center justify-end gap-1">
                        <Link to={`/souls/${soul.id}`} className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors" aria-label={`View soul ${soul.name || soul.target}`}>
                          <Eye className="w-4 h-4" />
                        </Link>
                        <button
                          onClick={() => handleToggle(soul)}
                          className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                          aria-label={soul.enabled ? `Pause ${soul.name || soul.target}` : `Resume ${soul.name || soul.target}`}
                        >
                          {soul.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
                        </button>
                        <button
                          onClick={() => handleDelete(soul.id)}
                          className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                          aria-label={`Delete soul ${soul.name || soul.target}`}
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {filteredSouls.map((soul) => {
            const typeInfo = typeConfig[soul.type] || typeConfig.http
            const TypeIcon = typeInfo.icon
            const displayStatus = getDisplayStatus(soul)

            return (
              <div key={soul.id} className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-5 hover:border-gray-600 transition-all group">
                <div className="flex items-start justify-between mb-4">
                  <div className="flex items-center gap-3">
                    <div className={`w-12 h-12 rounded-xl flex items-center justify-center ${typeInfo.bg}`}>
                      <TypeIcon className={`w-6 h-6 ${typeInfo.color}`} />
                    </div>
                    <div>
                      <h3 className="font-semibold text-white">{soul.name}</h3>
                      <span className={`text-xs ${typeInfo.color}`}>{typeInfo.label}</span>
                    </div>
                  </div>
                  <div className={`w-2 h-2 rounded-full ${getStatusColor(displayStatus)}`} />
                </div>

                <div className="space-y-3 mb-4">
                  <div className="flex items-center gap-2 text-sm">
                    <Globe className="w-4 h-4 text-gray-500" />
                    <span className="text-gray-400 font-mono text-xs truncate">{soul.target}</span>
                  </div>
                  {displayStatus === 'checking' && (
                    <div className="flex items-center gap-2 text-sm text-amber-400">
                      <RefreshCw className="w-4 h-4 animate-spin" />
                      <span>Initial check running</span>
                    </div>
                  )}
                  {displayStatus === 'check_failed' && (
                    <button
                      onClick={() => handleRetryInitialCheck(soul)}
                      className="flex items-center gap-2 text-sm text-rose-400 hover:text-rose-300 transition-colors"
                      aria-label={`Retry initial check for ${soul.name || soul.target}`}
                    >
                      <XCircle className="w-4 h-4" />
                      <span>Retry initial check</span>
                    </button>
                  )}
                  {displayStatus !== 'checking' && displayStatus !== 'check_failed' && soul.latency && (
                    <div className="flex items-center gap-2 text-sm">
                      <Clock className="w-4 h-4 text-gray-500" />
                      <span className={soul.latency > 1000 ? 'text-amber-400' : 'text-emerald-400'}>
                        {soul.latency}ms
                      </span>
                    </div>
                  )}
                </div>

                <div className="flex gap-1 pt-4 border-t border-gray-700/50">
                  <Link to={`/souls/${soul.id}`} className="flex-1 p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors" aria-label={`View soul ${soul.name || soul.target}`}>
                    <Eye className="w-4 h-4 mx-auto" />
                  </Link>
                  <button
                    onClick={() => handleToggle(soul)}
                    className="flex-1 p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                    aria-label={soul.enabled ? `Pause ${soul.name || soul.target}` : `Resume ${soul.name || soul.target}`}
                  >
                    {soul.enabled ? <Pause className="w-4 h-4 mx-auto" /> : <Play className="w-4 h-4 mx-auto" />}
                  </button>
                  <button
                    onClick={() => handleDelete(soul.id)}
                    className="flex-1 p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                    aria-label={`Delete soul ${soul.name || soul.target}`}
                  >
                    <Trash2 className="w-4 h-4 mx-auto" />
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* Empty State */}
      {filteredSouls.length === 0 && !loading && (
        <div className="text-center py-16">
          <div className="w-16 h-16 bg-gray-800 rounded-2xl flex items-center justify-center mx-auto mb-4">
            <Ghost className="w-8 h-8 text-gray-500" />
          </div>
          {search || filter !== 'all' ? (
            <>
              <h3 className="text-lg font-semibold text-white mb-2">No essence matches your search</h3>
              <p className="text-gray-400 text-sm mb-4">Try adjusting your search or sacred filters</p>
              <button
                onClick={() => { setSearch(''); setFilter('all') }}
                className="px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
              >
                Clear Filters
              </button>
            </>
          ) : souls.length === 0 ? (
            <>
              <h3 className="text-lg font-semibold text-white mb-2">No essence in the realm</h3>
              <p className="text-gray-400 text-sm mb-4">Summon your first soul to begin the eternal watch</p>
              <button
                onClick={() => { setShowModal(true); setFormErrors({}) }}
                className="px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
              >
                Summon First Soul
              </button>
            </>
          ) : null}
        </div>
      )}

      {/* Add Soul Modal */}
      <SoulCreateModal
        open={showModal}
        formData={formData}
        formErrors={formErrors}
        loading={loading}
        onClose={() => { setShowModal(false); setFormErrors({}) }}
        onSubmit={handleCreateSoul}
        onFormDataChange={setFormData}
        onErrorsChange={setFormErrors}
      />

      {/* Delete confirmation dialog */}
      <ConfirmDialog
        open={deletingSoul !== null}
        title="Delete Soul"
        message={
          <>
            Are you sure you want to delete{' '}
            <strong className="text-[var(--text-primary)]">
              {souls.find((s) => s.id === deletingSoul)?.name || 'this soul'}
            </strong>
            ? This action cannot be undone. All associated judgments and alert
            history will be permanently removed.
          </>
        }
        confirmLabel="Delete"
        onConfirm={handleConfirmDelete}
        onCancel={() => setDeletingSoul(null)}
        resourceName={souls.find((s) => s.id === deletingSoul)?.name}
      />
    </div>
  )
}
