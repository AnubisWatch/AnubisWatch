import { useState, useEffect } from 'react'
import Sidebar from './components/Sidebar'
import Header from './components/Header'
import Dashboard from './pages/Dashboard'
import Souls from './pages/Souls'
import Judgments from './pages/Judgments'
import Alerts from './pages/Alerts'
import Cluster from './pages/Cluster'
import Settings from './pages/Settings'
import { WebSocketProvider } from './hooks/useWebSocket'

function App() {
  const [activeTab, setActiveTab] = useState('dashboard')
  const [stats, setStats] = useState({
    totalSouls: 0,
    healthy: 0,
    degraded: 0,
    dead: 0,
    activeIncidents: 0,
    avgLatency: 0,
  })

  // Fetch stats on mount
  useEffect(() => {
    fetchStats()
    const interval = setInterval(fetchStats, 30000)
    return () => clearInterval(interval)
  }, [])

  const fetchStats = async () => {
    try {
      const res = await fetch('/api/v1/stats/overview')
      if (res.ok) {
        const data = await res.json()
        setStats({
          totalSouls: data.souls?.total || 0,
          healthy: data.souls?.healthy || 0,
          degraded: data.souls?.degraded || 0,
          dead: data.souls?.dead || 0,
          activeIncidents: data.alerts?.active_incidents || 0,
          avgLatency: data.judgments?.avg_latency_ms || 0,
        })
      }
    } catch (err) {
      console.error('Failed to fetch stats:', err)
    }
  }

  const renderContent = () => {
    switch (activeTab) {
      case 'dashboard':
        return <Dashboard stats={stats} />
      case 'souls':
        return <Souls />
      case 'judgments':
        return <Judgments />
      case 'alerts':
        return <Alerts />
      case 'cluster':
        return <Cluster />
      case 'settings':
        return <Settings />
      default:
        return <Dashboard stats={stats} />
    }
  }

  return (
    <WebSocketProvider>
      <div className="min-h-screen bg-slate-950">
        <Sidebar activeTab={activeTab} setActiveTab={setActiveTab} />
        <div className="lg:ml-64">
          <Header stats={stats} />
          <main className="p-6">
            {renderContent()}
          </main>
        </div>
      </div>
    </WebSocketProvider>
  )
}

export default App
