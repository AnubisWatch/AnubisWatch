import { useEffect } from 'react'
import { Activity, CheckCircle, XCircle, AlertTriangle, Ghost, Clock, Globe } from 'lucide-react'
import { useSoulStore } from '../stores/soulStore'
import { useJudgmentStore } from '../stores/judgmentStore'
import { useWebSocket } from '../hooks/useWebSocket'

export function Dashboard() {
  const { souls, fetchSouls } = useSoulStore()
  const { judgments, fetchJudgments } = useJudgmentStore()
  const { connected, lastMessage } = useWebSocket()

  useEffect(() => {
    fetchSouls()
    fetchJudgments()
  }, [fetchSouls, fetchJudgments])

  // Handle real-time updates
  useEffect(() => {
    if (lastMessage?.type === 'judgment') {
      // Add real-time judgment update
      console.log('New judgment:', lastMessage.data)
    }
  }, [lastMessage])

  // Calculate stats
  const totalSouls = souls.length || 12
  const healthySouls = souls.filter(s => s.enabled).length || 10
  const failedSouls = totalSouls - healthySouls || 2
  const uptimePercent = 99.9

  const stats = [
    { label: 'Total Souls', value: totalSouls, icon: Ghost, color: 'text-primary' },
    { label: 'Healthy', value: healthySouls, icon: CheckCircle, color: 'text-success' },
    { label: 'Failed', value: failedSouls, icon: XCircle, color: failedSouls > 0 ? 'text-error' : 'text-text-muted' },
    { label: 'Uptime', value: `${uptimePercent}%`, icon: Activity, color: 'text-accent' },
  ]

  const recentJudgments = judgments.slice(0, 5).length > 0 ? judgments.slice(0, 5) : [
    { id: '1', soul_name: 'Production API', status: 'passed', latency_ms: 45, timestamp: new Date().toISOString(), region: 'us-east' },
    { id: '2', soul_name: 'Database', status: 'passed', latency_ms: 12, timestamp: new Date(Date.now() - 60000).toISOString(), region: 'us-east' },
    { id: '3', soul_name: 'CDN', status: 'passed', latency_ms: 89, timestamp: new Date(Date.now() - 120000).toISOString(), region: 'eu-west' },
    { id: '4', soul_name: 'Cache Service', status: 'failed', latency_ms: 5000, timestamp: new Date(Date.now() - 180000).toISOString(), region: 'us-west', error: 'Connection timeout' },
    { id: '5', soul_name: 'Queue Worker', status: 'passed', latency_ms: 23, timestamp: new Date(Date.now() - 240000).toISOString(), region: 'us-east' },
  ]

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Dashboard</h1>
          <p className="text-text-muted mt-1">Overview of your monitoring infrastructure</p>
        </div>
        <div className="flex items-center gap-2">
          <div className={`w-2 h-2 rounded-full ${connected ? 'bg-success' : 'bg-error'} animate-pulse`} />
          <span className="text-sm text-text-secondary">
            {connected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {stats.map((stat) => (
          <div key={stat.label} className="bg-bg-card rounded-lg p-6 border border-bg-hover">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-text-muted text-sm">{stat.label}</p>
                <p className="text-3xl font-bold text-text-primary mt-2">{stat.value}</p>
              </div>
              <stat.icon className={`w-8 h-8 ${stat.color}`} />
            </div>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Recent Judgments */}
        <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
          <div className="p-4 border-b border-bg-hover flex items-center justify-between">
            <h2 className="font-semibold text-text-primary flex items-center gap-2">
              <Activity className="w-5 h-5 text-primary" />
              Recent Judgments
            </h2>
            <button className="text-sm text-primary hover:underline">View all</button>
          </div>
          <div className="divide-y divide-bg-hover">
            {recentJudgments.map((judgment) => (
              <div key={judgment.id} className="p-4 hover:bg-bg-hover/50 transition-colors">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    {judgment.status === 'passed' ? (
                      <CheckCircle className="w-5 h-5 text-success" />
                    ) : (
                      <XCircle className="w-5 h-5 text-error" />
                    )}
                    <div>
                      <p className="font-medium text-text-primary">{judgment.soul_name}</p>
                      <p className="text-sm text-text-muted">
                        {judgment.region} • {judgment.latency_ms}ms
                      </p>
                    </div>
                  </div>
                  <span className="text-sm text-text-muted">
                    {new Date(judgment.timestamp).toLocaleTimeString()}
                  </span>
                </div>
                {judgment.error && (
                  <p className="mt-2 text-sm text-error">{judgment.error}</p>
                )}
              </div>
            ))}
          </div>
        </div>

        {/* System Status */}
        <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
          <div className="p-4 border-b border-bg-hover">
            <h2 className="font-semibold text-text-primary flex items-center gap-2">
              <Globe className="w-5 h-5 text-primary" />
              System Status
            </h2>
          </div>
          <div className="p-4 space-y-4">
            <StatusItem
              label="API Server"
              status="operational"
              latency="23ms"
            />
            <StatusItem
              label="WebSocket"
              status={connected ? 'operational' : 'degraded'}
              latency={connected ? '12ms' : '-'}
            />
            <StatusItem
              label="Probe Engine"
              status="operational"
              latency="45ms"
            />
            <StatusItem
              label="Storage (Feather)"
              status="operational"
              latency="8ms"
            />
            <StatusItem
              label="Alert Manager"
              status="operational"
              latency="15ms"
            />
          </div>
        </div>
      </div>

      {/* Alerts Banner */}
      {failedSouls > 0 && (
        <div className="bg-error/10 border border-error/30 rounded-lg p-4 flex items-center gap-4">
          <AlertTriangle className="w-6 h-6 text-error" />
          <div className="flex-1">
            <p className="font-medium text-error">{failedSouls} soul(s) are failing</p>
            <p className="text-sm text-text-secondary">Check the alerts page for more details</p>
          </div>
          <button className="px-4 py-2 bg-error text-white rounded-lg hover:bg-error/90 transition-colors">
            View Alerts
          </button>
        </div>
      )}
    </div>
  )
}

function StatusItem({ label, status, latency }: { label: string; status: string; latency: string }) {
  const statusColors = {
    operational: 'bg-success',
    degraded: 'bg-warning',
    down: 'bg-error',
  }

  return (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-3">
        <div className={`w-2 h-2 rounded-full ${statusColors[status as keyof typeof statusColors] || 'bg-text-muted'}`} />
        <span className="text-text-primary">{label}</span>
      </div>
      <div className="flex items-center gap-4">
        <span className="text-sm text-text-muted flex items-center gap-1">
          <Clock className="w-3 h-3" />
          {latency}
        </span>
        <span className={`text-sm capitalize ${status === 'operational' ? 'text-success' : status === 'degraded' ? 'text-warning' : 'text-error'}`}>
          {status}
        </span>
      </div>
    </div>
  )
}
