import { useEffect, useState } from 'react'
import { useWebSocket } from '../hooks/useWebSocket'
import StatCard from '../components/StatCard'
import StatusChart from '../components/StatusChart'
import RecentJudgments from '../components/RecentJudgments'

function Dashboard({ stats }) {
  const { lastMessage, connected } = useWebSocket()
  const [judgments, setJudgments] = useState([])
  const [uptimeData, setUptimeData] = useState([])

  // Fetch initial data
  useEffect(() => {
    fetchRecentJudgments()
    fetchUptimeData()
  }, [])

  // Handle WebSocket messages
  useEffect(() => {
    if (lastMessage) {
      switch (lastMessage.type) {
        case 'judgment':
          setJudgments((prev) => [lastMessage.payload, ...prev.slice(0, 9)])
          break
        case 'stats':
          // Update stats from WebSocket
          break
      }
    }
  }, [lastMessage])

  const fetchRecentJudgments = async () => {
    try {
      const res = await fetch('/api/v1/judgments?limit=10')
      if (res.ok) {
        const data = await res.json()
        setJudgments(data)
      }
    } catch (err) {
      console.error('Failed to fetch judgments:', err)
    }
  }

  const fetchUptimeData = async () => {
    // Generate sample data - in production this would come from the API
    const data = Array.from({ length: 24 }, (_, i) => ({
      hour: `${i}:00`,
      uptime: 95 + Math.random() * 5,
    }))
    setUptimeData(data)
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Connection Status */}
      <div className="flex items-center gap-2 text-sm">
        <span className={`w-2 h-2 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500'}`}></span>
        <span className={connected ? 'text-green-400' : 'text-red-400'}>
          {connected ? 'Live Updates' : 'Disconnected'}
        </span>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        <StatCard
          title="Total Souls"
          value={stats.totalSouls}
          icon="𓃥"
          color="amber"
        />
        <StatCard
          title="Healthy"
          value={stats.healthy}
          icon="✓"
          color="green"
          trend={stats.totalSouls > 0 ? Math.round((stats.healthy / stats.totalSouls) * 100) : 0}
        />
        <StatCard
          title="Degraded"
          value={stats.degraded}
          icon="⚠"
          color="yellow"
        />
        <StatCard
          title="Dead"
          value={stats.dead}
          icon="✕"
          color="red"
          alert={stats.dead > 0}
        />
      </div>

      {/* Charts Row */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="card-egyptian p-6">
          <h3 className="font-display text-lg font-semibold text-amber-400 mb-4">
            24h Uptime History
          </h3>
          <StatusChart data={uptimeData} />
        </div>

        <div className="card-egyptian p-6">
          <h3 className="font-display text-lg font-semibold text-amber-400 mb-4">
            Status Distribution
          </h3>
          <div className="flex items-center justify-center h-48">
            <div className="flex items-center gap-8">
              <div className="text-center">
                <div className="w-24 h-24 rounded-full border-8 border-green-500 flex items-center justify-center mb-2">
                  <span className="text-2xl font-bold text-green-400">
                    {stats.totalSouls > 0 ? Math.round((stats.healthy / stats.totalSouls) * 100) : 0}%
                  </span>
                </div>
                <span className="text-sm text-slate-400">Healthy</span>
              </div>
              <div className="text-center">
                <div className="w-24 h-24 rounded-full border-8 border-amber-500 flex items-center justify-center mb-2">
                  <span className="text-2xl font-bold text-amber-400">
                    {stats.totalSouls > 0 ? Math.round((stats.degraded / stats.totalSouls) * 100) : 0}%
                  </span>
                </div>
                <span className="text-sm text-slate-400">Degraded</span>
              </div>
              <div className="text-center">
                <div className="w-24 h-24 rounded-full border-8 border-red-500 flex items-center justify-center mb-2">
                  <span className="text-2xl font-bold text-red-400">
                    {stats.totalSouls > 0 ? Math.round((stats.dead / stats.totalSouls) * 100) : 0}%
                  </span>
                </div>
                <span className="text-sm text-slate-400">Dead</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Recent Judgments */}
      <div className="card-egyptian p-6">
        <h3 className="font-display text-lg font-semibold text-amber-400 mb-4">
          Recent Judgments
        </h3>
        <RecentJudgments judgments={judgments} />
      </div>
    </div>
  )
}

export default Dashboard
