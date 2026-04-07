import { useEffect, useState } from 'react'
import {
  Plus,
  Search,
  Filter,
  Ghost,
  Play,
  Pause,
  Trash2,
  Edit,
  Activity,
  Globe,
  Server,
  XCircle,
  Clock,
  ChevronDown,
  RefreshCw,
  Wifi,
  X,
  CheckCircle2
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { useSoulStore, Soul } from '../stores/soulStore'

type SoulType = Soul['type']

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
  const { souls: rawSouls, fetchSouls, createSoul, updateSoul, deleteSoul } = useSoulStore()
  const souls = rawSouls as SoulWithStatus[]
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('list')
  const [refreshing, setRefreshing] = useState(false)
  const [showModal, setShowModal] = useState(false)
  const [loading, setLoading] = useState(false)

  // Form state
  const [formData, setFormData] = useState({
    name: '',
    type: 'http' as SoulType,
    target: '',
    enabled: true,
    weight: 60,
    timeout: 10,
    tags: [] as string[]
  })

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
    setLoading(true)
    try {
      await createSoul({
        ...formData,
        workspace_id: 'default'
      })
      setShowModal(false)
      setFormData({
        name: '',
        type: 'http',
        target: '',
        enabled: true,
        weight: 60,
        timeout: 10,
        tags: []
      })
    } catch (err) {
      alert('Failed to create soul: ' + (err instanceof Error ? err.message : 'Unknown error'))
    } finally {
      setLoading(false)
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this soul?')) return
    try {
      await deleteSoul(id)
    } catch (err) {
      alert('Failed to delete soul')
    }
  }

  const handleToggle = async (soul: SoulWithStatus) => {
    try {
      await updateSoul(soul.id, { enabled: !soul.enabled })
    } catch (err) {
      alert('Failed to update soul')
    }
  }

  const filteredSouls = souls.filter(soul => {
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

  const stats = {
    total: souls.length,
    active: souls.filter(s => s.enabled).length,
    disabled: souls.filter(s => !s.enabled).length,
    issues: souls.filter(s => s.status === 'unhealthy').length,
    types: new Set(souls.map(s => s.type)).size
  }

  const getStatusColor = (status?: string) => {
    switch (status) {
      case 'healthy': return 'bg-emerald-500'
      case 'unhealthy': return 'bg-rose-500'
      default: return 'bg-gray-500'
    }
  }

  const getStatusText = (status?: string) => {
    switch (status) {
      case 'healthy': return 'text-emerald-400'
      case 'unhealthy': return 'text-rose-400'
      default: return 'text-gray-400'
    }
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white tracking-tight">Souls</h1>
          <p className="text-gray-400 mt-1 text-sm">Manage your monitored targets and services</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            className={`p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all ${refreshing ? 'animate-spin' : ''}`}
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          <button
            onClick={() => setShowModal(true)}
            className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20"
          >
            <Plus className="w-4 h-4" />
            Add Soul
          </button>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Total Souls</p>
              <p className="text-2xl font-bold text-white mt-1">{stats.total}</p>
            </div>
            <div className="w-10 h-10 bg-gray-800 rounded-xl flex items-center justify-center">
              <Ghost className="w-5 h-5 text-gray-400" />
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
              <XCircle className="w-5 h-5 text-rose-400" />
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

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Types</p>
              <p className="text-2xl font-bold text-amber-400 mt-1">{stats.types}</p>
            </div>
            <div className="w-10 h-10 bg-amber-500/10 rounded-xl flex items-center justify-center">
              <Server className="w-5 h-5 text-amber-400" />
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
            placeholder="Search souls by name or target..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-gray-900 border border-gray-700/50 rounded-xl pl-11 pr-4 py-3 text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50 transition-colors"
          />
        </div>

        <div className="flex items-center gap-3">
          <div className="relative">
            <Filter className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
            <select
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="bg-gray-900 border border-gray-700/50 rounded-xl pl-10 pr-8 py-3 text-sm text-white focus:outline-none focus:border-amber-500/50 appearance-none cursor-pointer"
            >
              <option value="all">All Souls</option>
              <option value="enabled">Active Only</option>
              <option value="disabled">Disabled Only</option>
              <option value="http">HTTP</option>
              <option value="tcp">TCP</option>
              <option value="issues">With Issues</option>
            </select>
            <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 pointer-events-none" />
          </div>

          <div className="flex items-center bg-gray-900 border border-gray-700/50 rounded-xl p-1">
            <button
              onClick={() => setViewMode('list')}
              className={`p-2 rounded-lg transition-colors ${viewMode === 'list' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}`}
            >
              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M3 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zm0 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zm0 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1z" clipRule="evenodd" />
              </svg>
            </button>
            <button
              onClick={() => setViewMode('grid')}
              className={`p-2 rounded-lg transition-colors ${viewMode === 'grid' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}`}
            >
              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                <path d="M5 3a2 2 0 00-2 2v2a2 2 0 002 2h2a2 2 0 002-2V5a2 2 0 00-2-2H5zM5 11a2 2 0 00-2 2v2a2 2 0 002 2h2a2 2 0 002-2v-2a2 2 0 00-2-2H5zM11 5a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V5zM11 13a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
              </svg>
            </button>
          </div>
        </div>
      </div>

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
                            {soul.tags.slice(0, 2).map(tag => (
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
                        <div className={`w-2 h-2 rounded-full ${getStatusColor(soul.status)}`} />
                        <span className={`text-sm font-medium ${getStatusText(soul.status)}`}>
                          {soul.status?.charAt(0).toUpperCase()}{soul.status?.slice(1) || 'Unknown'}
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
                      {soul.latency ? (
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
                        <Link to={`/souls/${soul.id}`} className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors">
                          <Edit className="w-4 h-4" />
                        </Link>
                        <button
                          onClick={() => handleToggle(soul)}
                          className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                        >
                          {soul.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
                        </button>
                        <button
                          onClick={() => handleDelete(soul.id)}
                          className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
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
                  <div className={`w-2 h-2 rounded-full ${getStatusColor(soul.status)}`} />
                </div>

                <div className="space-y-3 mb-4">
                  <div className="flex items-center gap-2 text-sm">
                    <Globe className="w-4 h-4 text-gray-500" />
                    <span className="text-gray-400 font-mono text-xs truncate">{soul.target}</span>
                  </div>
                  {soul.latency && (
                    <div className="flex items-center gap-2 text-sm">
                      <Clock className="w-4 h-4 text-gray-500" />
                      <span className={soul.latency > 1000 ? 'text-amber-400' : 'text-emerald-400'}>
                        {soul.latency}ms
                      </span>
                    </div>
                  )}
                </div>

                <div className="flex gap-1 pt-4 border-t border-gray-700/50">
                  <Link to={`/souls/${soul.id}`} className="flex-1 p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors">
                    <Edit className="w-4 h-4 mx-auto" />
                  </Link>
                  <button
                    onClick={() => handleToggle(soul)}
                    className="flex-1 p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                  >
                    {soul.enabled ? <Pause className="w-4 h-4 mx-auto" /> : <Play className="w-4 h-4 mx-auto" />}
                  </button>
                  <button
                    onClick={() => handleDelete(soul.id)}
                    className="flex-1 p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
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
      {filteredSouls.length === 0 && (
        <div className="text-center py-16">
          <div className="w-16 h-16 bg-gray-800 rounded-2xl flex items-center justify-center mx-auto mb-4">
            <Ghost className="w-8 h-8 text-gray-500" />
          </div>
          <h3 className="text-lg font-semibold text-white mb-2">No souls found</h3>
          <p className="text-gray-400 text-sm mb-4">Try adjusting your search or filters</p>
          {souls.length === 0 && (
            <button
              onClick={() => setShowModal(true)}
              className="px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
            >
              Create Your First Soul
            </button>
          )}
        </div>
      )}

      {/* Add Soul Modal */}
      {showModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/50 backdrop-blur-sm">
          <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl w-full max-w-lg max-h-[90vh] overflow-auto">
            <div className="p-6 border-b border-gray-700/50 flex items-center justify-between">
              <h2 className="text-xl font-bold text-white">Add New Soul</h2>
              <button
                onClick={() => setShowModal(false)}
                className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={handleCreateSoul} className="p-6 space-y-5">
              {/* Name */}
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Name</label>
                <input
                  type="text"
                  required
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  placeholder="e.g., Production API"
                  className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              {/* Type */}
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Type</label>
                <select
                  value={formData.type}
                  onChange={(e) => setFormData({ ...formData, type: e.target.value as SoulType })}
                  className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                >
                  <option value="http">HTTP</option>
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                  <option value="dns">DNS</option>
                  <option value="icmp">ICMP</option>
                  <option value="smtp">SMTP</option>
                  <option value="grpc">gRPC</option>
                  <option value="websocket">WebSocket</option>
                  <option value="tls">TLS</option>
                </select>
              </div>

              {/* Target */}
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Target URL/Host</label>
                <input
                  type="text"
                  required
                  value={formData.target}
                  onChange={(e) => setFormData({ ...formData, target: e.target.value })}
                  placeholder="https://api.example.com/health"
                  className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              {/* Interval & Timeout */}
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Interval (seconds)</label>
                  <input
                    type="number"
                    min="10"
                    value={formData.weight}
                    onChange={(e) => setFormData({ ...formData, weight: parseInt(e.target.value) })}
                    className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Timeout (seconds)</label>
                  <input
                    type="number"
                    min="1"
                    value={formData.timeout}
                    onChange={(e) => setFormData({ ...formData, timeout: parseInt(e.target.value) })}
                    className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                  />
                </div>
              </div>

              {/* Enabled Toggle */}
              <div className="flex items-center gap-3 p-4 bg-gray-800/50 rounded-xl">
                <input
                  type="checkbox"
                  id="enabled"
                  checked={formData.enabled}
                  onChange={(e) => setFormData({ ...formData, enabled: e.target.checked })}
                  className="w-5 h-5 rounded border-gray-600 text-amber-500 focus:ring-amber-500/20"
                />
                <label htmlFor="enabled" className="text-sm text-gray-300">
                  Enable monitoring immediately
                </label>
              </div>

              {/* Buttons */}
              <div className="flex gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => setShowModal(false)}
                  className="flex-1 px-4 py-3 bg-gray-800 hover:bg-gray-700 text-white rounded-xl transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={loading}
                  className="flex-1 px-4 py-3 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
                >
                  {loading ? (
                    <>
                      <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                      Creating...
                    </>
                  ) : (
                    <>
                      <CheckCircle2 className="w-4 h-4" />
                      Create Soul
                    </>
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
