import { useState } from 'react'
import {
  Filter,
  CheckCircle2,
  XCircle,
  Clock,
  Search,
  ChevronDown,
  ArrowDownRight,
  BarChart3,
  Calendar,
  Activity,
  Server,
  Globe,
  Zap,
  Download,
  RefreshCw,
  Link as LinkIcon
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { useJudgments } from '../api/hooks'
import { Judgment } from '../api/client'


export function Judgments() {
  const { data: judgmentsData, loading, error, refetch } = useJudgments()
  const [filter, setFilter] = useState('all')
  const [search, setSearch] = useState('')
  const [timeRange, setTimeRange] = useState('24h')
  const [refreshing, setRefreshing] = useState(false)

  const judgments: Judgment[] = judgmentsData?.data || []

  const handleRefresh = async () => {
    setRefreshing(true)
    await refetch()
    setTimeout(() => setRefreshing(false), 500)
  }

  const filteredJudgments = judgments.filter(j => {
    const matchesFilter = filter === 'all' || j.status === filter
    const matchesSearch = (j.soul_name?.toLowerCase().includes(search.toLowerCase()) ||
                         j.region?.toLowerCase().includes(search.toLowerCase()))
    return matchesFilter && matchesSearch
  })

  const stats = {
    total: judgments.length,
    passed: judgments.filter(j => j.status === 'passed').length,
    failed: judgments.filter(j => j.status === 'failed').length,
    avgLatency: judgments.length > 0
      ? Math.round(judgments.reduce((acc, j) => acc + j.latency, 0) / judgments.length)
      : 0,
    avgPurity: judgments.length > 0
      ? Math.round(judgments.reduce((acc, j) => acc + (j.purity || 0), 0) / judgments.length)
      : 0,
    successRate: judgments.length > 0
      ? Math.round((judgments.filter(j => j.status === 'passed').length / judgments.length) * 100)
      : 0
  }

  const getPurityColor = (score: number) => {
    if (score >= 90) return 'bg-emerald-500'
    if (score >= 70) return 'bg-amber-500'
    return 'bg-rose-500'
  }

  const getPurityTextColor = (score: number) => {
    if (score >= 90) return 'text-emerald-400'
    if (score >= 70) return 'text-amber-400'
    return 'text-rose-400'
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white tracking-tight">Judgments</h1>
          <p className="text-gray-400 mt-1 text-sm">Review all health check executions and results</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            className={`p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all ${refreshing ? 'animate-spin' : ''}`}
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          <button className="flex items-center gap-2 px-4 py-2.5 bg-gray-800 hover:bg-gray-700 text-white rounded-xl transition-all font-medium">
            <Download className="w-4 h-4" />
            Export
          </button>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Total Checks</p>
              <p className="text-2xl font-bold text-white mt-1">{stats.total}</p>
            </div>
            <div className="w-10 h-10 bg-gray-800 rounded-xl flex items-center justify-center">
              <Activity className="w-5 h-5 text-gray-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Passed</p>
              <p className="text-2xl font-bold text-emerald-400 mt-1">{stats.passed}</p>
            </div>
            <div className="w-10 h-10 bg-emerald-500/10 rounded-xl flex items-center justify-center">
              <CheckCircle2 className="w-5 h-5 text-emerald-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Failed</p>
              <p className="text-2xl font-bold text-rose-400 mt-1">{stats.failed}</p>
            </div>
            <div className="w-10 h-10 bg-rose-500/10 rounded-xl flex items-center justify-center">
              <XCircle className="w-5 h-5 text-rose-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Success Rate</p>
              <p className="text-2xl font-bold text-blue-400 mt-1">{stats.successRate}%</p>
            </div>
            <div className="w-10 h-10 bg-blue-500/10 rounded-xl flex items-center justify-center">
              <BarChart3 className="w-5 h-5 text-blue-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Avg Latency</p>
              <p className="text-2xl font-bold text-amber-400 mt-1">{stats.avgLatency}ms</p>
            </div>
            <div className="w-10 h-10 bg-amber-500/10 rounded-xl flex items-center justify-center">
              <Zap className="w-5 h-5 text-amber-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Avg Purity</p>
              <p className="text-2xl font-bold text-purple-400 mt-1">{stats.avgPurity}%</p>
            </div>
            <div className="w-10 h-10 bg-purple-500/10 rounded-xl flex items-center justify-center">
              <Activity className="w-5 h-5 text-purple-400" />
            </div>
          </div>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-col lg:flex-row items-stretch lg:items-center gap-4">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <input
            type="text"
            placeholder="Search by soul name or region..."
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
              <option value="all">All Judgments</option>
              <option value="passed">Passed Only</option>
              <option value="failed">Failed Only</option>
            </select>
            <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 pointer-events-none" />
          </div>

          <div className="relative">
            <Calendar className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
            <select
              value={timeRange}
              onChange={(e) => setTimeRange(e.target.value)}
              className="bg-gray-900 border border-gray-700/50 rounded-xl pl-10 pr-8 py-3 text-sm text-white focus:outline-none focus:border-amber-500/50 appearance-none cursor-pointer"
            >
              <option value="1h">Last Hour</option>
              <option value="24h">Last 24 Hours</option>
              <option value="7d">Last 7 Days</option>
              <option value="30d">Last 30 Days</option>
            </select>
            <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 pointer-events-none" />
          </div>
        </div>
      </div>

      {/* Content */}
      {loading ? (
        <div className="flex items-center justify-center py-16">
          <div className="w-8 h-8 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
        </div>
      ) : error ? (
        <div className="text-center py-16">
          <XCircle className="w-12 h-12 text-rose-500 mx-auto mb-4" />
          <p className="text-gray-400">{error}</p>
          <button
            onClick={handleRefresh}
            className="mt-4 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
          >
            Try Again
          </button>
        </div>
      ) : filteredJudgments.length === 0 ? (
        <div className="text-center py-16">
          <Activity className="w-12 h-12 text-gray-600 mx-auto mb-4" />
          <h3 className="text-lg font-semibold text-white mb-2">No judgments yet</h3>
          <p className="text-gray-400 mb-4">Judgments will appear here when souls are checked</p>
          <Link
            to="/souls"
            className="inline-flex items-center gap-2 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
          >
            Go to Souls
          </Link>
        </div>
      ) : (
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden">
          <table className="w-full">
            <thead className="bg-gray-800/50">
              <tr>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Status</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Soul</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Latency</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Purity</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Region</th>
                <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-700/50">
              {filteredJudgments.map((judgment) => (
                  <tr key={judgment.id} className="hover:bg-gray-800/30 transition-colors group">
                    <td className="px-6 py-4">
                      <div className={`inline-flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-semibold ${
                        judgment.status === 'passed'
                          ? 'bg-emerald-500/10 text-emerald-400'
                          : 'bg-rose-500/10 text-rose-400'
                      }`}>
                        {judgment.status === 'passed' ? (
                          <CheckCircle2 className="w-4 h-4" />
                        ) : (
                          <XCircle className="w-4 h-4" />
                        )}
                        <span className="capitalize">{judgment.status}</span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <div>
                        <Link to={`/souls/${judgment.soul_id}`} className="font-semibold text-white hover:text-amber-400 transition-colors flex items-center gap-2">
                          <Server className="w-4 h-4 text-gray-500" />
                          {judgment.soul_name || judgment.soul_id}
                        </Link>
                        {judgment.error && (
                          <p className="text-sm text-rose-400 mt-1 flex items-center gap-1.5">
                            <ArrowDownRight className="w-3.5 h-3.5" />
                            {judgment.error}
                          </p>
                        )}
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-2">
                        <Clock className="w-4 h-4 text-gray-500" />
                        <span className={`font-mono font-medium ${
                          judgment.latency > 1000 ? 'text-amber-400' : 'text-emerald-400'
                        }`}>
                          {judgment.latency}ms
                        </span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-3">
                        <div className="w-20 h-2 bg-gray-800 rounded-full overflow-hidden">
                          <div
                            className={`h-full rounded-full ${getPurityColor(judgment.purity || 0)}`}
                            style={{ width: `${judgment.purity || 0}%` }}
                          />
                        </div>
                        <span className={`text-sm font-semibold ${getPurityTextColor(judgment.purity || 0)}`}>
                          {judgment.purity || 0}
                        </span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-2 text-gray-400">
                        <Globe className="w-4 h-4" />
                        <span className="text-sm">{judgment.region}</span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-sm text-gray-400">
                        {new Date(judgment.timestamp).toLocaleString()}
                      </span>
                    </td>
                  </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Empty State Info */}
      {judgments.length === 0 && !loading && !error && (
        <div className="bg-gradient-to-br from-amber-900/20 to-gray-800 border border-amber-500/20 rounded-2xl p-8 text-center">
          <LinkIcon className="w-12 h-12 text-amber-400 mx-auto mb-4" />
          <h3 className="text-xl font-bold text-white mb-2">Connect Your Souls</h3>
          <p className="text-gray-400 mb-4">Add souls to start monitoring and see real judgments here</p>
          <Link
            to="/souls"
            className="inline-flex items-center gap-2 px-6 py-3 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-semibold"
          >
            Create Your First Soul
          </Link>
        </div>
      )}
    </div>
  )
}
