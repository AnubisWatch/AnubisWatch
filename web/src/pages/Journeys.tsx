import { useState, useEffect, useCallback } from 'react'
import {
  Route,
  Plus,
  Play,
  Pause,
  Edit,
  Trash2,
  CheckCircle2,
  XCircle,
  Clock,
  Footprints,
  Search,
  Filter,
  ChevronDown,
  RefreshCw,
  TrendingUp,
  AlertCircle,
  Timer,
  Activity,
  Loader2,
  X
} from 'lucide-react'
import { api } from '../api/client'

interface Journey {
  id: string
  name: string
  enabled: boolean
  weight: number
  timeout: number
  step_count: number
  last_run?: string
  last_status: 'passed' | 'failed' | 'pending' | 'unknown'
  avg_duration: number
  success_rate: number
  description?: string
  region?: string
  workspace_id?: string
  created_at?: string
  updated_at?: string
}

// Custom hook for journeys since it's not in the main hooks file yet
function useJourneys() {
  const [journeys, setJourneys] = useState<Journey[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchJourneys = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.get<Journey[]>('/journeys')
      setJourneys(result || [])
      setError(null)
    } catch (err) {
      // If endpoint doesn't exist yet, just show empty state
      if (err instanceof Error && err.message.includes('404')) {
        setJourneys([])
        setError(null)
      } else {
        setError(err instanceof Error ? err.message : 'Unknown error')
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchJourneys()
  }, [fetchJourneys])

  const createJourney = async (journey: Omit<Journey, 'id'>) => {
    const result = await api.post<Journey>('/journeys', journey)
    await fetchJourneys()
    return result
  }

  const updateJourney = async (id: string, journey: Partial<Journey>) => {
    const result = await api.put<Journey>(`/journeys/${id}`, journey)
    await fetchJourneys()
    return result
  }

  const deleteJourney = async (id: string) => {
    await api.delete(`/journeys/${id}`)
    await fetchJourneys()
  }

  const runJourney = async (id: string) => {
    return api.post<{ status: string; duration: number }>(`/journeys/${id}/run`)
  }

  return {
    journeys,
    loading,
    error,
    refetch: fetchJourneys,
    createJourney,
    updateJourney,
    deleteJourney,
    runJourney
  }
}

export function Journeys() {
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [runningId, setRunningId] = useState<string | null>(null)

  const {
    journeys,
    loading,
    error,
    refetch,
    deleteJourney,
    updateJourney,
    runJourney
  } = useJourneys()

  const handleRefresh = async () => {
    await refetch()
  }

  const handleToggleEnabled = async (id: string, enabled: boolean) => {
    await updateJourney(id, { enabled: !enabled })
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this journey?')) return
    await deleteJourney(id)
  }

  const handleRun = async (id: string) => {
    setRunningId(id)
    try {
      await runJourney(id)
      await refetch()
    } catch (err) {
      // Journey run failed
    } finally {
      setRunningId(null)
    }
  }

  const filteredJourneys = journeys.filter(journey => {
    const matchesSearch = journey.name.toLowerCase().includes(search.toLowerCase()) ||
                         journey.description?.toLowerCase().includes(search.toLowerCase())
    const matchesFilter = filter === 'all' ||
                         (filter === 'enabled' && journey.enabled) ||
                         (filter === 'disabled' && !journey.enabled) ||
                         (filter === 'issues' && journey.last_status === 'failed')
    return matchesSearch && matchesFilter
  })

  const stats = {
    total: journeys.length,
    active: journeys.filter(j => j.enabled).length,
    disabled: journeys.filter(j => !j.enabled).length,
    issues: journeys.filter(j => j.last_status === 'failed').length,
    totalSteps: journeys.reduce((acc, j) => acc + (j.step_count || 0), 0),
    avgSuccessRate: journeys.length > 0
      ? Math.round(journeys.filter(j => j.success_rate > 0).reduce((acc, j) => acc + j.success_rate, 0) / journeys.filter(j => j.success_rate > 0).length)
      : 0
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'passed': return 'bg-emerald-500'
      case 'failed': return 'bg-rose-500'
      case 'pending': return 'bg-amber-500'
      default: return 'bg-gray-500'
    }
  }

  const getStatusTextColor = (status: string) => {
    switch (status) {
      case 'passed': return 'text-emerald-400'
      case 'failed': return 'text-rose-400'
      case 'pending': return 'text-amber-400'
      default: return 'text-gray-400'
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-32">
        <div className="w-10 h-10 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
      </div>
    )
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white tracking-tight">Journeys</h1>
          <p className="text-gray-400 mt-1 text-sm">Multi-step synthetic monitoring workflows</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            className="p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all"
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          <button
            onClick={() => setShowCreateModal(true)}
            className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20"
          >
            <Plus className="w-4 h-4" />
            Create Journey
          </button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Total Journeys</p>
              <p className="text-2xl font-bold text-white mt-1">{stats.total}</p>
            </div>
            <div className="w-10 h-10 bg-gray-800 rounded-xl flex items-center justify-center">
              <Route className="w-5 h-5 text-gray-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Active</p>
              <p className="text-2xl font-bold text-emerald-400 mt-1">{stats.active}</p>
            </div>
            <div className="w-10 h-10 bg-emerald-500/10 rounded-xl flex items-center justify-center">
              <Play className="w-5 h-5 text-emerald-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Issues</p>
              <p className="text-2xl font-bold text-rose-400 mt-1">{stats.issues}</p>
            </div>
            <div className="w-10 h-10 bg-rose-500/10 rounded-xl flex items-center justify-center">
              <AlertCircle className="w-5 h-5 text-rose-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Total Steps</p>
              <p className="text-2xl font-bold text-amber-400 mt-1">{stats.totalSteps}</p>
            </div>
            <div className="w-10 h-10 bg-amber-500/10 rounded-xl flex items-center justify-center">
              <Footprints className="w-5 h-5 text-amber-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Avg Success</p>
              <p className="text-2xl font-bold text-blue-400 mt-1">{stats.avgSuccessRate}%</p>
            </div>
            <div className="w-10 h-10 bg-blue-500/10 rounded-xl flex items-center justify-center">
              <TrendingUp className="w-5 h-5 text-blue-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Disabled</p>
              <p className="text-2xl font-bold text-gray-400 mt-1">{stats.disabled}</p>
            </div>
            <div className="w-10 h-10 bg-gray-700 rounded-xl flex items-center justify-center">
              <Pause className="w-5 h-5 text-gray-400" />
            </div>
          </div>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-4">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <input
            type="text"
            placeholder="Search journeys..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-gray-900 border border-gray-700/50 rounded-xl pl-11 pr-4 py-3 text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50 transition-colors"
          />
        </div>

        <div className="relative">
          <Filter className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="bg-gray-900 border border-gray-700/50 rounded-xl pl-10 pr-8 py-3 text-sm text-white focus:outline-none focus:border-amber-500/50 appearance-none cursor-pointer"
          >
            <option value="all">All Journeys</option>
            <option value="enabled">Active Only</option>
            <option value="disabled">Disabled Only</option>
            <option value="issues">With Issues</option>
          </select>
          <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 pointer-events-none" />
        </div>
      </div>

      {/* Error State */}
      {error && (
        <div className="bg-rose-500/10 border border-rose-500/20 rounded-2xl p-6 text-center">
          <AlertCircle className="w-12 h-12 text-rose-500 mx-auto mb-3" />
          <p className="text-rose-400">{error}</p>
          <button
            onClick={handleRefresh}
            className="mt-4 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
          >
            Try Again
          </button>
        </div>
      )}

      {/* Journeys Grid */}
      {!error && journeys.length > 0 ? (
        <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-5">
          {filteredJourneys.map((journey) => (
            <div
              key={journey.id}
              className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden hover:border-gray-600 transition-all group"
            >
              <div className="p-5">
                <div className="flex items-start justify-between mb-4">
                  <div className="flex items-center gap-3">
                    <div className={`w-12 h-12 rounded-xl flex items-center justify-center ${
                      journey.enabled ? 'bg-amber-500/10' : 'bg-gray-800'
                    }`}>
                      <Route className={`w-6 h-6 ${journey.enabled ? 'text-amber-400' : 'text-gray-500'}`} />
                    </div>
                    <div>
                      <h3 className="font-semibold text-white">{journey.name}</h3>
                      <p className="text-sm text-gray-500">{journey.step_count || 0} steps</p>
                    </div>
                  </div>
                  <span className={`px-2.5 py-1 rounded-lg text-xs font-semibold ${
                    journey.enabled ? 'bg-emerald-500/10 text-emerald-400' : 'bg-gray-800 text-gray-500'
                  }`}>
                    {journey.enabled ? 'Active' : 'Disabled'}
                  </span>
                </div>

                {journey.description && (
                  <p className="text-sm text-gray-400 mb-4 line-clamp-2">{journey.description}</p>
                )}

                <div className="grid grid-cols-2 gap-3 mb-4">
                  <div className="bg-gray-800/50 rounded-xl p-3">
                    <div className="flex items-center gap-2 text-gray-500 mb-1">
                      <Timer className="w-4 h-4" />
                      <span className="text-xs">Interval</span>
                    </div>
                    <p className="text-white font-medium">{journey.weight}s</p>
                  </div>
                  <div className="bg-gray-800/50 rounded-xl p-3">
                    <div className="flex items-center gap-2 text-gray-500 mb-1">
                      <Clock className="w-4 h-4" />
                      <span className="text-xs">Timeout</span>
                    </div>
                    <p className="text-white font-medium">{journey.timeout}s</p>
                  </div>
                </div>

                <div className="flex items-center justify-between pt-4 border-t border-gray-700/50">
                  <div className="flex items-center gap-2">
                    {journey.last_status === 'passed' ? (
                      <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                    ) : journey.last_status === 'failed' ? (
                      <XCircle className="w-4 h-4 text-rose-400" />
                    ) : (
                      <Activity className="w-4 h-4 text-amber-400" />
                    )}
                    <span className={`text-sm font-medium ${getStatusTextColor(journey.last_status)}`}>
                      {journey.last_status.charAt(0).toUpperCase() + journey.last_status.slice(1)}
                    </span>
                    {journey.last_run && (
                      <span className="text-gray-500 text-sm">
                        • {new Date(journey.last_run).toLocaleTimeString()}
                      </span>
                    )}
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className={`w-2 h-2 rounded-full ${getStatusColor(journey.last_status)}`} />
                  </div>
                </div>

                {journey.avg_duration > 0 && (
                  <div className="mt-3 flex items-center gap-2 text-sm text-gray-500">
                    <Clock className="w-3.5 h-3.5" />
                    Avg: <span className="text-gray-300 font-medium">{(journey.avg_duration / 1000).toFixed(1)}s</span>
                  </div>
                )}
              </div>

              <div className="px-5 py-3 bg-gray-800/30 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <div className="w-16 h-1.5 bg-gray-700 rounded-full overflow-hidden">
                    <div
                      className={`h-full rounded-full ${journey.success_rate >= 90 ? 'bg-emerald-500' : journey.success_rate >= 70 ? 'bg-amber-500' : 'bg-rose-500'}`}
                      style={{ width: `${journey.success_rate}%` }}
                    />
                  </div>
                  <span className={`text-xs font-medium ${journey.success_rate >= 90 ? 'text-emerald-400' : journey.success_rate >= 70 ? 'text-amber-400' : 'text-rose-400'}`}>
                    {journey.success_rate.toFixed(1)}%
                  </span>
                </div>
                <div className="flex items-center gap-1">
                  <button
                    onClick={() => handleRun(journey.id)}
                    disabled={runningId === journey.id || !journey.enabled}
                    className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors disabled:opacity-50"
                    title="Run Now"
                  >
                    {runningId === journey.id ? <Loader2 className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
                  </button>
                  <button
                    onClick={() => handleToggleEnabled(journey.id, journey.enabled)}
                    className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                    title={journey.enabled ? 'Disable' : 'Enable'}
                  >
                    {journey.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
                  </button>
                  <button className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors" title="Edit">
                    <Edit className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleDelete(journey.id)}
                    className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                    title="Delete"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            </div>
          ))}

          {/* Create New Card */}
          <button
            onClick={() => setShowCreateModal(true)}
            className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-dashed border-gray-700/50 rounded-2xl p-6 flex flex-col items-center justify-center gap-4 hover:border-amber-500 transition-all text-gray-500 min-h-[280px]"
          >
            <div className="w-16 h-16 rounded-full bg-gray-800 flex items-center justify-center">
              <Plus className="w-8 h-8" />
            </div>
            <div className="text-center">
              <p className="font-medium text-white">Create New Journey</p>
              <p className="text-sm text-gray-500 mt-1">Set up multi-step monitoring</p>
            </div>
          </button>
        </div>
      ) : !error && (
        /* Empty State */
        <div className="text-center py-16">
          <Route className="w-16 h-16 text-gray-600 mx-auto mb-4" />
          <h3 className="text-xl font-semibold text-white mb-2">No journeys yet</h3>
          <p className="text-gray-400 mb-6 max-w-md mx-auto">
            Journeys are multi-step synthetic monitoring workflows. Create your first journey to monitor complex user flows.
          </p>
          <button
            onClick={() => setShowCreateModal(true)}
            className="px-6 py-3 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-colors"
          >
            Create Your First Journey
          </button>
          <p className="text-sm text-gray-500 mt-4">
            Note: Journeys feature requires backend API support
          </p>
        </div>
      )}

      {/* Create Modal Placeholder */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50">
          <div className="bg-gray-900 border border-gray-700/50 rounded-2xl p-6 w-full max-w-lg">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-semibold text-white">Create Journey</h2>
              <button onClick={() => setShowCreateModal(false)} className="p-2 text-gray-400 hover:text-white">
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="text-center py-8">
              <Route className="w-12 h-12 text-gray-600 mx-auto mb-4" />
              <p className="text-gray-400">
                Journey creation requires backend API support for the /journeys endpoint.
              </p>
              <p className="text-sm text-gray-500 mt-2">
                This feature is planned for a future release.
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
