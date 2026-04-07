import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Activity,
  Clock,
  Globe,
  CheckCircle2,
  XCircle,
  RefreshCw,
  Zap,
  TrendingUp,
  Calendar,
  Server,
  Settings2,
  Tag,
  Timer,
  MapPin,
  Play,
  Pause,
  Edit3,
  Trash2,
  Copy,
  ChevronRight,
  AlertTriangle,
  Loader2
} from 'lucide-react'
import { useState, useMemo } from 'react'
import { useSoul, useSoulJudgments } from '../api/hooks'

interface UptimeData {
  date: string
  uptime: number
  responseTime: number
}

export function SoulDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [timeRange, setTimeRange] = useState('24h')
  const [activeTab, setActiveTab] = useState('overview')
  const [isDeleting, setIsDeleting] = useState(false)
  const [isChecking, setIsChecking] = useState(false)
  const [checkResult, setCheckResult] = useState<{ success: boolean; message: string } | null>(null)

  const { soul, loading: soulLoading, error: soulError, refetch, updateSoul, deleteSoul, forceCheck } = useSoul(id)
  const { data: judgmentsData, loading: judgmentsLoading, error: judgmentsError } = useSoulJudgments(id)

  const judgments = judgmentsData || []

  // Calculate stats from real data
  const stats = useMemo(() => {
    if (!judgments.length) {
      return {
        uptime24h: 100,
        uptime7d: 100,
        uptime30d: 100,
        avgResponse: 0,
        totalChecks: 0,
        failures: 0,
        availability: 100,
      }
    }

    const now = Date.now()
    const dayMs = 24 * 60 * 60 * 1000

    const last24h = judgments.filter(j => new Date(j.timestamp).getTime() > now - dayMs)
    const last7d = judgments.filter(j => new Date(j.timestamp).getTime() > now - 7 * dayMs)
    const last30d = judgments.filter(j => new Date(j.timestamp).getTime() > now - 30 * dayMs)

    const calcUptime = (items: typeof judgments) => {
      if (!items.length) return 100
      const passed = items.filter(j => j.status === 'passed').length
      return Math.round((passed / items.length) * 10000) / 100
    }

    const avgResponse = judgments.length
      ? Math.round(judgments.reduce((acc, j) => acc + j.latency, 0) / judgments.length)
      : 0

    const failures = judgments.filter(j => j.status === 'failed').length

    return {
      uptime24h: calcUptime(last24h),
      uptime7d: calcUptime(last7d),
      uptime30d: calcUptime(last30d),
      avgResponse,
      totalChecks: judgments.length,
      failures,
      availability: calcUptime(judgments),
    }
  }, [judgments])

  // Generate uptime chart data from judgments
  const uptimeData: UptimeData[] = useMemo(() => {
    const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
    const data: UptimeData[] = []

    for (let i = 6; i >= 0; i--) {
      const date = new Date()
      date.setDate(date.getDate() - i)
      const dayName = days[date.getDay()]

      const dayStart = new Date(date.setHours(0, 0, 0, 0)).getTime()
      const dayEnd = dayStart + 24 * 60 * 60 * 1000

      const dayJudgments = judgments.filter(j => {
        const jTime = new Date(j.timestamp).getTime()
        return jTime >= dayStart && jTime < dayEnd
      })

      if (dayJudgments.length > 0) {
        const passed = dayJudgments.filter(j => j.status === 'passed').length
        const avgLatency = dayJudgments.reduce((acc, j) => acc + j.latency, 0) / dayJudgments.length
        data.push({
          date: dayName,
          uptime: Math.round((passed / dayJudgments.length) * 100 * 10) / 10,
          responseTime: Math.round(avgLatency / 10), // Scale for display
        })
      } else {
        data.push({ date: dayName, uptime: 100, responseTime: 0 })
      }
    }

    return data
  }, [judgments])

  const handleToggleEnabled = async () => {
    if (!soul) return
    await updateSoul({ enabled: !soul.enabled })
  }

  const handleDelete = async () => {
    if (!soul || !confirm('Are you sure you want to delete this soul? This action cannot be undone.')) return
    setIsDeleting(true)
    try {
      await deleteSoul()
      navigate('/souls')
    } catch (err) {
      alert('Failed to delete soul: ' + (err instanceof Error ? err.message : 'Unknown error'))
    } finally {
      setIsDeleting(false)
    }
  }

  const handleForceCheck = async () => {
    if (!soul) return
    setIsChecking(true)
    setCheckResult(null)
    try {
      const result = await forceCheck()
      setCheckResult({
        success: result?.status === 'passed',
        message: result?.status === 'passed'
          ? `Check passed! Latency: ${result.latency}ms`
          : `Check failed: ${result?.error || 'Unknown error'}`
      })
      await refetch()
    } catch (err) {
      setCheckResult({
        success: false,
        message: 'Check failed: ' + (err instanceof Error ? err.message : 'Unknown error')
      })
    } finally {
      setIsChecking(false)
      setTimeout(() => setCheckResult(null), 5000)
    }
  }

  const handleCopyTarget = () => {
    if (soul?.target) {
      navigator.clipboard.writeText(soul.target)
    }
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'passed': return <CheckCircle2 className="w-5 h-5 text-emerald-400" />
      case 'failed': return <XCircle className="w-5 h-5 text-rose-400" />
      case 'pending': return <Activity className="w-5 h-5 text-amber-400" />
      default: return <Activity className="w-5 h-5 text-gray-400" />
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

  if (soulLoading) {
    return (
      <div className="flex items-center justify-center py-32">
        <div className="w-10 h-10 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
      </div>
    )
  }

  if (soulError || !soul) {
    return (
      <div className="text-center py-16">
        <AlertTriangle className="w-12 h-12 text-rose-500 mx-auto mb-4" />
        <p className="text-gray-400">{soulError || 'Soul not found'}</p>
        <button
          onClick={() => navigate('/souls')}
          className="mt-4 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
        >
          Back to Souls
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <button
          onClick={() => navigate('/souls')}
          className="hover:text-amber-400 transition-colors"
        >
          Souls
        </button>
        <ChevronRight className="w-4 h-4" />
        <span className="text-gray-300">{soul.name}</span>
      </div>

      {/* Header */}
      <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <button
            onClick={() => navigate('/souls')}
            className="p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all"
          >
            <ArrowLeft className="w-5 h-5" />
          </button>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-3xl font-bold text-white">{soul.name}</h1>
              <span className={`px-2.5 py-1 rounded-lg text-xs font-semibold ${
                soul.enabled ? 'bg-emerald-500/10 text-emerald-400' : 'bg-gray-800 text-gray-500'
              }`}>
                {soul.enabled ? 'Active' : 'Disabled'}
              </span>
            </div>
            <p className="text-gray-400 flex items-center gap-2 mt-1">
              <Globe className="w-4 h-4 text-amber-400" />
              <span className="font-mono">{soul.target}</span>
              <button
                onClick={handleCopyTarget}
                className="p-1 hover:bg-gray-800 rounded transition-colors"
                title="Copy target"
              >
                <Copy className="w-3 h-3" />
              </button>
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 flex-wrap">
          {checkResult && (
            <span className={`text-sm px-3 py-1.5 rounded-lg ${
              checkResult.success ? 'bg-emerald-500/10 text-emerald-400' : 'bg-rose-500/10 text-rose-400'
            }`}>
              {checkResult.message}
            </span>
          )}
          <button
            onClick={handleToggleEnabled}
            className="flex items-center gap-2 px-4 py-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all"
          >
            {soul.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
            {soul.enabled ? 'Pause' : 'Resume'}
          </button>
          <button
            onClick={() => navigate(`/souls/${id}/edit`)}
            className="flex items-center gap-2 px-4 py-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all"
          >
            <Edit3 className="w-4 h-4" />
            Edit
          </button>
          <button
            onClick={handleForceCheck}
            disabled={isChecking}
            className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white rounded-xl transition-all shadow-lg shadow-amber-600/20"
          >
            {isChecking ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
            Test Now
          </button>
          <button
            onClick={handleDelete}
            disabled={isDeleting}
            className="p-2.5 bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 rounded-xl transition-all disabled:opacity-50"
          >
            {isDeleting ? <Loader2 className="w-5 h-5 animate-spin" /> : <Trash2 className="w-5 h-5" />}
          </button>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          label="Uptime (24h)"
          value={`${stats.uptime24h}%`}
          subValue={stats.uptime24h >= 99.9 ? 'Excellent' : stats.uptime24h >= 99 ? 'Good' : 'Needs Attention'}
          icon={Activity}
          gradient="from-emerald-500/20 to-emerald-600/10"
          iconColor="text-emerald-400"
          borderColor="border-emerald-500/20"
        />
        <StatCard
          label="Avg Response"
          value={`${stats.avgResponse}ms`}
          subValue={stats.avgResponse < 100 ? 'Fast' : stats.avgResponse < 500 ? 'Normal' : 'Slow'}
          icon={Zap}
          gradient="from-amber-500/20 to-amber-600/10"
          iconColor="text-amber-400"
          borderColor="border-amber-500/20"
        />
        <StatCard
          label="Total Checks"
          value={stats.totalChecks.toLocaleString()}
          subValue="All time"
          icon={CheckCircle2}
          gradient="from-blue-500/20 to-blue-600/10"
          iconColor="text-blue-400"
          borderColor="border-blue-500/20"
        />
        <StatCard
          label="Failures"
          value={stats.failures.toString()}
          subValue={stats.totalChecks > 0 ? `${((stats.failures / stats.totalChecks) * 100).toFixed(2)}% rate` : 'No checks yet'}
          icon={XCircle}
          gradient="from-rose-500/20 to-rose-600/10"
          iconColor="text-rose-400"
          borderColor="border-rose-500/20"
        />
      </div>

      {/* Tabs */}
      <div className="border-b border-gray-700/50">
        <div className="flex gap-6">
          {['overview', 'performance', 'history', 'settings'].map((tab) => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={`pb-3 text-sm font-medium capitalize transition-colors relative ${
                activeTab === tab ? 'text-amber-400' : 'text-gray-400 hover:text-gray-300'
              }`}
            >
              {tab}
              {activeTab === tab && (
                <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-amber-400 rounded-full" />
              )}
            </button>
          ))}
        </div>
      </div>

      {/* Content */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left Column */}
        <div className="lg:col-span-2 space-y-6">
          {/* Uptime Chart */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-6">
            <div className="flex items-center justify-between mb-6">
              <div>
                <h3 className="text-lg font-semibold text-white flex items-center gap-2">
                  <TrendingUp className="w-5 h-5 text-amber-400" />
                  Response Time & Uptime
                </h3>
                <p className="text-sm text-gray-400 mt-1">Performance trends over the last 7 days</p>
              </div>
              <select
                value={timeRange}
                onChange={(e) => setTimeRange(e.target.value)}
                className="bg-gray-800 border border-gray-700/50 rounded-xl px-4 py-2 text-sm text-white focus:outline-none focus:border-amber-500/50"
              >
                <option value="1h">Last Hour</option>
                <option value="24h">Last 24 Hours</option>
                <option value="7d">Last 7 Days</option>
                <option value="30d">Last 30 Days</option>
              </select>
            </div>

            {uptimeData.some(d => d.responseTime > 0) ? (
              <>
                {/* Chart Bars */}
                <div className="h-48 flex items-end justify-between gap-3">
                  {uptimeData.map((day) => (
                    <div key={day.date} className="flex-1 flex flex-col items-center gap-2">
                      <div className="w-full flex flex-col gap-1">
                        <div
                          className="w-full bg-emerald-500/80 rounded-t-lg transition-all hover:bg-emerald-400"
                          style={{ height: `${day.uptime * 1.5}px` }}
                          title={`Uptime: ${day.uptime}%`}
                        />
                        <div
                          className="w-full bg-amber-500/60 rounded-t-lg transition-all hover:bg-amber-400"
                          style={{ height: `${Math.min(day.responseTime, 100)}px` }}
                          title={`Response: ${day.responseTime * 10}ms`}
                        />
                      </div>
                      <span className="text-xs text-gray-500">{day.date}</span>
                    </div>
                  ))}
                </div>

                <div className="flex items-center justify-center gap-6 mt-4 pt-4 border-t border-gray-700/50">
                  <div className="flex items-center gap-2">
                    <div className="w-3 h-3 bg-emerald-500/80 rounded" />
                    <span className="text-sm text-gray-400">Uptime %</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <div className="w-3 h-3 bg-amber-500/60 rounded" />
                    <span className="text-sm text-gray-400">Response Time</span>
                  </div>
                </div>
              </>
            ) : (
              <div className="h-48 flex items-center justify-center">
                <div className="text-center">
                  <Activity className="w-12 h-12 text-gray-600 mx-auto mb-3" />
                  <p className="text-gray-400">No data available yet</p>
                  <p className="text-sm text-gray-500 mt-1">Judgments will appear here after checks run</p>
                </div>
              </div>
            )}
          </div>

          {/* Recent Judgments */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl overflow-hidden">
            <div className="p-5 border-b border-gray-700/50 flex items-center justify-between">
              <h3 className="text-lg font-semibold text-white flex items-center gap-2">
                <Clock className="w-5 h-5 text-amber-400" />
                Recent Judgments
              </h3>
              <button
                onClick={() => navigate('/judgments')}
                className="text-sm text-amber-400 hover:text-amber-300 transition-colors"
              >
                View All
              </button>
            </div>
            {judgmentsLoading ? (
              <div className="flex items-center justify-center py-12">
                <div className="w-8 h-8 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
              </div>
            ) : judgmentsError ? (
              <div className="text-center py-8">
                <p className="text-rose-400">{judgmentsError}</p>
              </div>
            ) : judgments.length === 0 ? (
              <div className="text-center py-8">
                <Activity className="w-12 h-12 text-gray-600 mx-auto mb-3" />
                <p className="text-gray-400">No judgments yet</p>
                <p className="text-sm text-gray-500 mt-1">Click &quot;Test Now&quot; to run the first check</p>
              </div>
            ) : (
              <div className="divide-y divide-gray-700/50">
                {judgments.slice(0, 8).map((judgment) => (
                  <div
                    key={judgment.id}
                    className="p-4 flex items-center justify-between hover:bg-gray-800/50 transition-colors"
                  >
                    <div className="flex items-center gap-4">
                      <div className={`w-10 h-10 rounded-xl flex items-center justify-center ${
                        judgment.status === 'passed' ? 'bg-emerald-500/10' :
                        judgment.status === 'failed' ? 'bg-rose-500/10' : 'bg-amber-500/10'
                      }`}>
                        {getStatusIcon(judgment.status)}
                      </div>
                      <div>
                        <p className={`font-medium capitalize ${getStatusTextColor(judgment.status)}`}>
                          {judgment.status}
                        </p>
                        {judgment.error && (
                          <p className="text-sm text-rose-400">{judgment.error}</p>
                        )}
                        <p className="text-sm text-gray-500 flex items-center gap-2">
                          <MapPin className="w-3 h-3" />
                          {judgment.region || 'unknown'}
                        </p>
                      </div>
                    </div>
                    <div className="text-right">
                      <p className="text-white font-mono">
                        {judgment.latency > 1000 ? `${(judgment.latency / 1000).toFixed(1)}s` : `${judgment.latency}ms`}
                      </p>
                      <p className="text-sm text-gray-500">
                        {new Date(judgment.timestamp).toLocaleTimeString()}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Right Column */}
        <div className="space-y-6">
          {/* Quick Stats */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
            <h3 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
              <TrendingUp className="w-5 h-5 text-amber-400" />
              Availability
            </h3>
            <div className="space-y-4">
              <div>
                <div className="flex items-center justify-between text-sm mb-1">
                  <span className="text-gray-400">24 Hours</span>
                  <span className="text-emerald-400 font-semibold">{stats.uptime24h}%</span>
                </div>
                <div className="h-2 bg-gray-700 rounded-full overflow-hidden">
                  <div className="h-full bg-gradient-to-r from-emerald-500 to-emerald-400 rounded-full" style={{ width: `${Math.min(stats.uptime24h, 100)}%` }} />
                </div>
              </div>
              <div>
                <div className="flex items-center justify-between text-sm mb-1">
                  <span className="text-gray-400">7 Days</span>
                  <span className="text-emerald-400 font-semibold">{stats.uptime7d}%</span>
                </div>
                <div className="h-2 bg-gray-700 rounded-full overflow-hidden">
                  <div className="h-full bg-gradient-to-r from-emerald-500 to-emerald-400 rounded-full" style={{ width: `${Math.min(stats.uptime7d, 100)}%` }} />
                </div>
              </div>
              <div>
                <div className="flex items-center justify-between text-sm mb-1">
                  <span className="text-gray-400">30 Days</span>
                  <span className="text-emerald-400 font-semibold">{stats.uptime30d}%</span>
                </div>
                <div className="h-2 bg-gray-700 rounded-full overflow-hidden">
                  <div className="h-full bg-gradient-to-r from-emerald-500 to-emerald-400 rounded-full" style={{ width: `${Math.min(stats.uptime30d, 100)}%` }} />
                </div>
              </div>
            </div>
          </div>

          {/* Configuration */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
            <h3 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
              <Settings2 className="w-5 h-5 text-amber-400" />
              Configuration
            </h3>
            <div className="space-y-4">
              <div className="flex items-center justify-between py-2 border-b border-gray-700/50">
                <div className="flex items-center gap-2 text-gray-400">
                  <Server className="w-4 h-4" />
                  <span className="text-sm">Type</span>
                </div>
                <span className="text-white font-medium uppercase px-2 py-1 bg-amber-500/10 rounded-lg text-sm">
                  {soul.type}
                </span>
              </div>
              <div className="flex items-center justify-between py-2 border-b border-gray-700/50">
                <div className="flex items-center gap-2 text-gray-400">
                  <Timer className="w-4 h-4" />
                  <span className="text-sm">Interval</span>
                </div>
                <span className="text-white font-medium">{soul.interval || soul.weight}s</span>
              </div>
              <div className="flex items-center justify-between py-2 border-b border-gray-700/50">
                <div className="flex items-center gap-2 text-gray-400">
                  <Clock className="w-4 h-4" />
                  <span className="text-sm">Timeout</span>
                </div>
                <span className="text-white font-medium">{soul.timeout}s</span>
              </div>
              <div className="flex items-center justify-between py-2 border-b border-gray-700/50">
                <div className="flex items-center gap-2 text-gray-400">
                  <MapPin className="w-4 h-4" />
                  <span className="text-sm">Region</span>
                </div>
                <span className="text-white font-medium">{soul.region || 'global'}</span>
              </div>
              {soul.http_config && (
                <div className="flex items-center justify-between py-2">
                  <div className="flex items-center gap-2 text-gray-400">
                    <Activity className="w-4 h-4" />
                    <span className="text-sm">Method</span>
                  </div>
                  <span className="text-white font-medium">{soul.http_config.method}</span>
                </div>
              )}
            </div>
          </div>

          {/* Tags */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
            <h3 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
              <Tag className="w-5 h-5 text-amber-400" />
              Tags
            </h3>
            <div className="flex flex-wrap gap-2">
              {(soul.tags || []).length > 0 ? (
                soul.tags?.map(tag => (
                  <span
                    key={tag}
                    className="px-3 py-1.5 bg-gray-800 text-gray-300 rounded-xl text-sm border border-gray-700/50 hover:border-amber-500/30 transition-colors"
                  >
                    {tag}
                  </span>
                ))
              ) : (
                <span className="text-sm text-gray-500">No tags</span>
              )}
            </div>
          </div>

          {/* Info */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
            <h3 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
              <Calendar className="w-5 h-5 text-amber-400" />
              Information
            </h3>
            <div className="space-y-3 text-sm">
              <div className="flex items-center justify-between">
                <span className="text-gray-400">Soul ID</span>
                <span className="text-gray-300 font-mono text-xs">{soul.id}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-gray-400">Created</span>
                <span className="text-gray-300">
                  {soul.created_at ? new Date(soul.created_at).toLocaleDateString() : 'Unknown'}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-gray-400">Updated</span>
                <span className="text-gray-300">
                  {soul.updated_at ? new Date(soul.updated_at).toLocaleDateString() : 'Unknown'}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

interface StatCardProps {
  label: string
  value: string
  subValue: string
  icon: typeof Activity
  gradient: string
  iconColor: string
  borderColor: string
}

function StatCard({ label, value, subValue, icon: Icon, gradient, iconColor, borderColor }: StatCardProps) {
  return (
    <div className={`bg-gradient-to-br ${gradient} border ${borderColor} rounded-2xl p-5`}>
      <div className="flex items-start justify-between">
        <div>
          <p className="text-gray-400 text-sm font-medium">{label}</p>
          <p className="text-2xl font-bold text-white mt-1">{value}</p>
          <p className="text-sm text-gray-400 mt-1">{subValue}</p>
        </div>
        <div className="w-12 h-12 bg-gray-900/50 rounded-xl flex items-center justify-center">
          <Icon className={`w-6 h-6 ${iconColor}`} />
        </div>
      </div>
    </div>
  )
}
