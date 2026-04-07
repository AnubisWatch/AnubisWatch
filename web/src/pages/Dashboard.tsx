import { useEffect, useMemo, useState } from 'react'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Server,
  TrendingUp,
  Zap,
  Shield,
  RefreshCw,
  Plus,
  Target,
  BarChart3,
  Globe,
  ArrowUpRight,
  Clock
} from 'lucide-react'
import { useSoulStore } from '../stores/soulStore'
import { useStats, useClusterStatus, useJudgments } from '../api/hooks'
import { Link } from 'react-router-dom'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  BarChart,
  Bar
} from 'recharts'

interface SystemStatus {
  name: string
  status: 'operational' | 'degraded' | 'down'
  value: string
  icon: typeof Server
  color: string
}

export function Dashboard() {
  const { souls, fetchSouls } = useSoulStore()
  const { data: statsData, refetch: refetchStats } = useStats()
  const { data: clusterData } = useClusterStatus()
  const { data: judgmentsData } = useJudgments()
  const [refreshing, setRefreshing] = useState(false)
  const [mounted, setMounted] = useState(false)

  useEffect(() => {
    setMounted(true)
    fetchSouls()
  }, [fetchSouls])

  const handleRefresh = async () => {
    setRefreshing(true)
    await Promise.all([fetchSouls(), refetchStats()])
    setTimeout(() => setRefreshing(false), 500)
  }

  const stats = useMemo(() => {
    const soulsArray = Array.isArray(souls) ? souls : []
    const total = soulsArray.length
    const healthy = soulsArray.filter(s => s.enabled).length
    const failed = total - healthy

    return {
      total,
      healthy,
      failed,
      uptime: statsData?.souls ?
        Math.round(((statsData.souls.healthy + statsData.souls.degraded) / Math.max(statsData.souls.total, 1)) * 1000) / 10 :
        100,
      checksToday: statsData?.judgments?.today || 0,
      avgLatency: Math.round(statsData?.judgments?.avg_latency_ms || 0)
    }
  }, [souls, statsData])

  // Prepare chart data from judgments
  const chartData = useMemo(() => {
    if (!judgmentsData?.data || judgmentsData.data.length === 0) {
      // Return empty data for last 12 hours
      return Array.from({ length: 12 }, (_, i) => ({
        time: `${i}:00`,
        latency: 0,
        count: 0,
        passed: 0,
        failed: 0
      }))
    }

    const judgments = judgmentsData.data
    const now = new Date()
    const hours = 12
    const data = []

    for (let i = hours - 1; i >= 0; i--) {
      const hour = new Date(now.getTime() - i * 60 * 60 * 1000)
      const hourStart = new Date(hour.setMinutes(0, 0, 0))
      const hourEnd = new Date(hourStart.getTime() + 60 * 60 * 1000)

      const hourJudgments = judgments.filter(j => {
        const jTime = new Date(j.timestamp)
        return jTime >= hourStart && jTime < hourEnd
      })

      const passed = hourJudgments.filter(j => j.status === 'passed').length
      const failed = hourJudgments.filter(j => j.status === 'failed').length
      const avgLatency = hourJudgments.length > 0
        ? Math.round(hourJudgments.reduce((acc, j) => acc + j.latency, 0) / hourJudgments.length)
        : 0

      data.push({
        time: hourStart.getHours() + ':00',
        latency: avgLatency,
        count: hourJudgments.length,
        passed,
        failed
      })
    }

    return data
  }, [judgmentsData])

  const systemStatus: SystemStatus[] = [
    {
      name: 'Probe Engine',
      status: stats.failed > 0 ? 'degraded' : 'operational',
      value: `${stats.avgLatency}ms`,
      icon: Activity,
      color: stats.failed > 0 ? 'text-amber-400' : 'text-emerald-400'
    },
    {
      name: clusterData?.is_clustered ? 'Cluster' : 'Standalone',
      status: 'operational',
      value: clusterData?.is_clustered ? `${clusterData.peer_count} nodes` : 'Active',
      icon: Globe,
      color: 'text-blue-400'
    },
    {
      name: 'Storage',
      status: 'operational',
      value: 'Healthy',
      icon: Server,
      color: 'text-purple-400'
    },
    {
      name: 'Alert Manager',
      status: 'operational',
      value: 'Active',
      icon: Shield,
      color: 'text-rose-400'
    },
  ]

  const recentSouls = souls.slice(0, 5)

  const StatCard = ({ title, value, subtext, icon: Icon, trend, color, delay }: {
    title: string
    value: string | number
    subtext: string
    icon: typeof Server
    trend?: string
    color: 'amber' | 'emerald' | 'rose' | 'blue' | 'gold' | 'turquoise' | 'carnelian' | 'lapis'
    delay: number
  }) => {
    const colors = {
      amber: 'from-amber-500/20 to-amber-600/10 border-amber-500/20 text-amber-400',
      emerald: 'from-emerald-500/20 to-emerald-600/10 border-emerald-500/20 text-emerald-400',
      rose: 'from-rose-500/20 to-rose-600/10 border-rose-500/20 text-rose-400',
      blue: 'from-blue-500/20 to-blue-600/10 border-blue-500/20 text-blue-400',
      // Ancient Egypt colors
      gold: 'from-[#D4AF37]/20 to-[#B8860B]/10 border-[#D4AF37]/30 text-[#F4D03F]',
      turquoise: 'from-[#40E0D0]/20 to-[#20B2AA]/10 border-[#40E0D0]/30 text-[#40E0D0]',
      carnelian: 'from-[#B7410E]/20 to-[#8B0000]/10 border-[#B7410E]/30 text-[#CD5C5C]',
      lapis: 'from-[#1E3A5F]/40 to-[#0f172a]/20 border-[#1E3A5F]/40 text-[#60A5FA]',
    }

    return (
      <div
        className={`relative overflow-hidden bg-gradient-to-br ${colors[color]} border rounded-2xl p-6
                    group hover:scale-[1.02] hover:shadow-xl transition-all duration-500 animate-slide-up`}
        style={{ animationDelay: `${delay}ms` }}
      >
        {/* Background Glow */}
        <div className="absolute -top-20 -right-20 w-40 h-40 bg-white/5 rounded-full blur-3xl
                        group-hover:bg-white/10 transition-all duration-700" />

        <div className="relative">
          <div className="flex items-start justify-between mb-4">
            <div className={`w-12 h-12 rounded-xl bg-gradient-to-br ${colors[color].split(' ')[0]}
                            flex items-center justify-center shadow-lg`}>
              <Icon className="w-6 h-6 text-white" />
            </div>
            {trend && (
              <span className={`flex items-center gap-1 text-sm font-medium ${
                trend.startsWith('+') ? 'text-emerald-400' : 'text-gray-400'
              }`}>
                <TrendingUp className="w-4 h-4" />
                {trend}
              </span>
            )}
          </div>

          <p className="text-gray-400 text-sm font-medium mb-1">{title}</p>
          <p className="text-3xl font-bold text-white mb-2">{value}</p>
          <p className="text-gray-500 text-xs">{subtext}</p>
        </div>

        {/* Hover Shine Effect */}
        <div className="absolute inset-0 opacity-0 group-hover:opacity-100 transition-opacity duration-500
                        bg-gradient-to-r from-transparent via-white/5 to-transparent -skew-x-12
                        translate-x-[-100%] group-hover:translate-x-[100%] transition-transform duration-1000" />
      </div>
    )
  }

  return (
    <div className="space-y-8 animate-fade-in">
      {/* Background Effects */}
      <div className="fixed inset-0 pointer-events-none overflow-hidden">
        <div className="absolute top-0 left-1/4 w-96 h-96 bg-amber-500/5 rounded-full blur-[100px]" />
        <div className="absolute bottom-0 right-1/4 w-96 h-96 bg-blue-500/5 rounded-full blur-[100px]" />
      </div>

      {/* Header */}
      <div className={`flex flex-col sm:flex-row sm:items-center justify-between gap-4 transition-all duration-700 ${
        mounted ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4'
      }`}>
        <div>
          <div className="flex items-center gap-3 mb-1">
            <div className="relative">
              <img src="/anubis-icon.svg" alt="" className="w-8 h-8 animate-pulse" />
            </div>
            <h1 className="text-4xl font-cinzel font-bold gradient-gold-shine tracking-wider">Hall of Judgment</h1>
          </div>
          <p className="text-gray-400 font-cormorant italic">The eternal watch over your digital realm</p>
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            className={`p-3 bg-[#D4AF37]/10 hover:bg-[#D4AF37]/20 text-[#D4AF37] rounded-xl
                       border border-[#D4AF37]/30 hover:border-[#D4AF37]/50 transition-all duration-300
                       ${refreshing ? 'animate-spin' : 'hover:rotate-180'}`}
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          <Link
            to="/souls"
            className="flex items-center gap-2 px-5 py-3 bg-gradient-to-r from-[#B8860B] via-[#D4AF37] to-[#B8860B]
                       hover:from-[#D4AF37] hover:via-[#F4D03F] hover:to-[#D4AF37] text-gray-950 rounded-xl transition-all duration-300
                       font-cinzel font-bold shadow-lg shadow-[#D4AF37]/30 hover:shadow-[#D4AF37]/50
                       hover:scale-105 active:scale-95 border border-[#D4AF37]"
          >
            <Plus className="w-5 h-5" />
            Summon Soul
          </Link>
        </div>
      </div>

      {/* Stats Grid - Ancient Egypt themed */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-5">
        <StatCard
          title="Total Souls"
          value={stats.total}
          subtext="Monitored in the realm"
          icon={Target}
          trend="+2"
          color="gold"
          delay={100}
        />
        <StatCard
          title="Pure Hearts"
          value={stats.healthy}
          subtext="Blessed services"
          icon={CheckCircle2}
          color="turquoise"
          delay={200}
        />
        <StatCard
          title="Chaos"
          value={stats.failed}
          subtext="Require judgment"
          icon={AlertTriangle}
          color="carnelian"
          delay={300}
        />
        <StatCard
          title="Balance"
          value={`${stats.uptime}%`}
          subtext="Last 30 days"
          icon={Zap}
          color="blue"
          delay={400}
        />
      </div>

      {/* Main Content */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Chart Section */}
        <div className={`lg:col-span-2 bg-gradient-to-br from-gray-900 to-gray-800/50
                        border border-gray-700/50 rounded-2xl p-6 transition-all duration-700 delay-500
                        ${mounted ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4'}`}>
          <div className="flex items-center justify-between mb-6">
            <div>
              <h2 className="text-xl font-semibold text-white flex items-center gap-2">
                <BarChart3 className="w-5 h-5 text-amber-400" />
                Activity Overview
              </h2>
              <p className="text-sm text-gray-500 mt-1">Response time and check volume over the last 12 hours</p>
            </div>
            <div className="flex items-center gap-2">
              <span className="flex items-center gap-1.5 text-sm text-gray-400">
                <span className="w-2 h-2 rounded-full bg-emerald-500" />
                Checks: {stats.checksToday}
              </span>
            </div>
          </div>

          {/* Charts */}
          <div className="space-y-6">
            {/* Latency Chart */}
            <div className="h-64">
              <h3 className="text-sm font-medium text-gray-400 mb-3 flex items-center gap-2">
                <Clock className="w-4 h-4" />
                Average Latency (ms)
              </h3>
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData}>
                  <defs>
                    <linearGradient id="latencyGradient" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#f59e0b" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#f59e0b" stopOpacity={0}/>
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                  <XAxis
                    dataKey="time"
                    stroke="#6b7280"
                    fontSize={12}
                    tickLine={false}
                  />
                  <YAxis
                    stroke="#6b7280"
                    fontSize={12}
                    tickLine={false}
                    axisLine={false}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: '#1f2937',
                      border: '1px solid #374151',
                      borderRadius: '8px',
                      color: '#fff'
                    }}
                  />
                  <Area
                    type="monotone"
                    dataKey="latency"
                    stroke="#f59e0b"
                    strokeWidth={2}
                    fill="url(#latencyGradient)"
                  />
                </AreaChart>
              </ResponsiveContainer>
            </div>

            {/* Checks Chart */}
            <div className="h-48">
              <h3 className="text-sm font-medium text-gray-400 mb-3 flex items-center gap-2">
                <Activity className="w-4 h-4" />
                Health Checks (Passed vs Failed)
              </h3>
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                  <XAxis
                    dataKey="time"
                    stroke="#6b7280"
                    fontSize={12}
                    tickLine={false}
                  />
                  <YAxis
                    stroke="#6b7280"
                    fontSize={12}
                    tickLine={false}
                    axisLine={false}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: '#1f2937',
                      border: '1px solid #374151',
                      borderRadius: '8px',
                      color: '#fff'
                    }}
                  />
                  <Bar dataKey="passed" fill="#10b981" radius={[4, 4, 0, 0]} />
                  <Bar dataKey="failed" fill="#f43f5e" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
        </div>

        {/* Right Column */}
        <div className={`space-y-4 transition-all duration-700 delay-600
                        ${mounted ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4'}`}>
          {/* System Status */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-5">
            <h2 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
              <Shield className="w-5 h-5 text-emerald-400" />
              System Status
            </h2>
            <div className="space-y-3">
              {systemStatus.map((service, i) => {
                const Icon = service.icon
                return (
                  <div
                    key={service.name}
                    className="flex items-center justify-between p-3 rounded-xl bg-gray-800/30
                               hover:bg-gray-800/50 transition-all duration-300 group"
                    style={{ animationDelay: `${i * 100}ms` }}
                  >
                    <div className="flex items-center gap-3">
                      <div className={`w-10 h-10 rounded-lg bg-gray-800 flex items-center justify-center
                                      group-hover:scale-110 transition-transform`}>
                        <Icon className={`w-5 h-5 ${service.color}`} />
                      </div>
                      <div>
                        <p className="text-sm font-medium text-white">{service.name}</p>
                        <p className={`text-xs ${service.color}`}>
                          {service.status.charAt(0).toUpperCase() + service.status.slice(1)}
                        </p>
                      </div>
                    </div>
                    <span className="text-sm text-gray-500 font-mono">{service.value}</span>
                  </div>
                )
              })}
            </div>
          </div>

          {/* Recent Souls */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-5">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold text-white flex items-center gap-2">
                <Server className="w-5 h-5 text-amber-400" />
                Recent Souls
              </h2>
              <Link to="/souls" className="text-sm text-amber-400 hover:text-amber-300">
                View All
              </Link>
            </div>
            <div className="space-y-2">
              {recentSouls.length === 0 ? (
                <p className="text-sm text-gray-500 text-center py-4">No souls yet</p>
              ) : (
                recentSouls.map((soul) => (
                  <Link
                    key={soul.id}
                    to={`/souls/${soul.id}`}
                    className="flex items-center gap-3 p-3 rounded-xl bg-gray-800/30
                               hover:bg-gray-800/50 transition-all group"
                  >
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center
                                    ${soul.enabled ? 'bg-emerald-500/10' : 'bg-gray-700'}`}>
                      <div className={`w-2 h-2 rounded-full ${soul.enabled ? 'bg-emerald-400' : 'bg-gray-500'}`} />
                    </div>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-white truncate">{soul.name}</p>
                      <p className="text-xs text-gray-500 truncate">{soul.target}</p>
                    </div>
                    <ArrowUpRight className="w-4 h-4 text-gray-500 group-hover:text-amber-400 transition-colors" />
                  </Link>
                ))
              )}
            </div>
          </div>

          {/* Quick Actions */}
          <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-5">
            <h2 className="text-lg font-semibold text-white mb-4">Quick Actions</h2>
            <div className="grid grid-cols-2 gap-3">
              <Link
                to="/souls"
                className="p-4 bg-gray-800/30 hover:bg-amber-500/10 rounded-xl border border-gray-700/30
                           hover:border-amber-500/30 transition-all group"
              >
                <Server className="w-6 h-6 text-amber-400 mb-2 group-hover:scale-110 transition-transform" />
                <p className="text-sm font-medium text-white">Add Soul</p>
              </Link>
              <Link
                to="/alerts"
                className="p-4 bg-gray-800/30 hover:bg-rose-500/10 rounded-xl border border-gray-700/30
                           hover:border-rose-500/30 transition-all group"
              >
                <Shield className="w-6 h-6 text-rose-400 mb-2 group-hover:scale-110 transition-transform" />
                <p className="text-sm font-medium text-white">Alerts</p>
              </Link>
            </div>
          </div>
        </div>
      </div>

      {/* Empty State */}
      {stats.total === 0 && (
        <div className="text-center py-16 animate-fade-in">
          <div className="relative inline-block mb-6">
            <div className="w-24 h-24 rounded-2xl bg-gradient-to-br from-amber-500/20 to-amber-600/10
                            flex items-center justify-center border border-amber-500/20 animate-float">
              <Target className="w-12 h-12 text-amber-400" />
            </div>
            <div className="absolute -bottom-2 -right-2 w-8 h-8 bg-emerald-500 rounded-full
                            flex items-center justify-center animate-bounce">
              <Plus className="w-5 h-5 text-white" />
            </div>
          </div>
          <h3 className="text-2xl font-bold text-white mb-2">Start Monitoring</h3>
          <p className="text-gray-400 mb-6 max-w-md mx-auto">
            Add your first soul to begin monitoring your infrastructure. Track uptime, latency, and health status in real-time.
          </p>
          <Link
            to="/souls"
            className="inline-flex items-center gap-2 px-6 py-3 bg-gradient-to-r from-amber-600 to-amber-500
                       text-white rounded-xl font-semibold hover:shadow-lg hover:shadow-amber-600/30
                       transition-all hover:scale-105"
          >
            <Plus className="w-5 h-5" />
            Create First Soul
          </Link>
        </div>
      )}
    </div>
  )
}
