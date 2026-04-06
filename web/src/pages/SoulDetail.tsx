import { useParams, useNavigate } from 'react-router-dom'
import { ArrowLeft, Activity, Clock, Globe, CheckCircle, XCircle, RefreshCw } from 'lucide-react'
import { useState } from 'react'

export function SoulDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [timeRange, setTimeRange] = useState('24h')

  // Mock soul data - would fetch from API in real implementation
  const soul = {
    id: id || '1',
    name: 'Production API',
    type: 'http',
    target: 'https://api.example.com/health',
    enabled: true,
    weight: 30,
    timeout: 10,
    tags: ['production', 'api'],
    region: 'us-east',
    http_config: {
      method: 'GET',
      valid_status: [200],
      headers: { 'Accept': 'application/json' },
      body: '',
    },
  }

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const uptimeData = [
    { date: 'Mon', uptime: 100, responseTime: 45 },
    { date: 'Tue', uptime: 99.8, responseTime: 52 },
    { date: 'Wed', uptime: 100, responseTime: 48 },
    { date: 'Thu', uptime: 99.5, responseTime: 65 },
    { date: 'Fri', uptime: 100, responseTime: 42 },
    { date: 'Sat', uptime: 100, responseTime: 40 },
    { date: 'Sun', uptime: 99.9, responseTime: 44 },
  ]
  void uptimeData

  const recentJudgments = [
    { id: '1', status: 'passed', latency: 45, timestamp: new Date().toISOString(), region: 'us-east' },
    { id: '2', status: 'passed', latency: 42, timestamp: new Date(Date.now() - 30000).toISOString(), region: 'us-east' },
    { id: '3', status: 'passed', latency: 48, timestamp: new Date(Date.now() - 60000).toISOString(), region: 'us-east' },
    { id: '4', status: 'passed', latency: 51, timestamp: new Date(Date.now() - 90000).toISOString(), region: 'us-east' },
    { id: '5', status: 'failed', latency: 5000, timestamp: new Date(Date.now() - 120000).toISOString(), region: 'us-east', error: 'Connection timeout' },
  ]

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <button
          onClick={() => navigate('/souls')}
          className="p-2 text-text-secondary hover:text-text-primary hover:bg-bg-hover rounded-lg transition-colors"
        >
          <ArrowLeft className="w-5 h-5" />
        </button>
        <div className="flex-1">
          <h1 className="text-2xl font-bold text-text-primary">{soul.name}</h1>
          <p className="text-text-muted flex items-center gap-2">
            <Globe className="w-4 h-4" />
            {soul.target}
          </p>
        </div>
        <button className="flex items-center gap-2 px-4 py-2 bg-bg-hover text-text-primary rounded-lg hover:bg-bg-hover/80 transition-colors">
          <RefreshCw className="w-4 h-4" />
          Test Now
        </button>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <StatCard
          label="Uptime (24h)"
          value="99.95%"
          icon={Activity}
          color="text-success"
        />
        <StatCard
          label="Avg Response"
          value="45ms"
          icon={Clock}
          color="text-primary"
        />
        <StatCard
          label="Checks"
          value="2,880"
          icon={CheckCircle}
          color="text-info"
        />
        <StatCard
          label="Failures"
          value="2"
          icon={XCircle}
          color="text-error"
        />
      </div>

      {/* Configuration */}
      <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
        <div className="p-4 border-b border-bg-hover">
          <h2 className="font-semibold text-text-primary">Configuration</h2>
        </div>
        <div className="p-6 grid grid-cols-2 gap-6">
          <div>
            <label className="text-sm text-text-muted block mb-1">Type</label>
            <p className="text-text-primary font-medium uppercase">{soul.type}</p>
          </div>
          <div>
            <label className="text-sm text-text-muted block mb-1">Interval</label>
            <p className="text-text-primary font-medium">{soul.weight} seconds</p>
          </div>
          <div>
            <label className="text-sm text-text-muted block mb-1">Timeout</label>
            <p className="text-text-primary font-medium">{soul.timeout} seconds</p>
          </div>
          <div>
            <label className="text-sm text-text-muted block mb-1">Region</label>
            <p className="text-text-primary font-medium">{soul.region}</p>
          </div>
          {soul.http_config && (
            <>
              <div>
                <label className="text-sm text-text-muted block mb-1">Method</label>
                <p className="text-text-primary font-medium">{soul.http_config.method}</p>
              </div>
              <div>
                <label className="text-sm text-text-muted block mb-1">Valid Status</label>
                <p className="text-text-primary font-medium">{soul.http_config.valid_status.join(', ')}</p>
              </div>
            </>
          )}
          <div className="col-span-2">
            <label className="text-sm text-text-muted block mb-1">Tags</label>
            <div className="flex gap-2">
              {soul.tags.map(tag => (
                <span key={tag} className="px-3 py-1 bg-bg-hover text-text-secondary rounded-full text-sm">
                  {tag}
                </span>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Recent Judgments */}
      <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
        <div className="p-4 border-b border-bg-hover flex items-center justify-between">
          <h2 className="font-semibold text-text-primary">Recent Judgments</h2>
          <select
            value={timeRange}
            onChange={(e) => setTimeRange(e.target.value)}
            className="bg-bg-dark border border-bg-hover rounded-lg px-3 py-1 text-sm text-text-primary"
          >
            <option value="1h">Last Hour</option>
            <option value="24h">Last 24 Hours</option>
            <option value="7d">Last 7 Days</option>
          </select>
        </div>
        <div className="divide-y divide-bg-hover">
          {recentJudgments.map((judgment) => (
            <div key={judgment.id} className="p-4 flex items-center justify-between hover:bg-bg-hover/30">
              <div className="flex items-center gap-3">
                {judgment.status === 'passed' ? (
                  <CheckCircle className="w-5 h-5 text-success" />
                ) : (
                  <XCircle className="w-5 h-5 text-error" />
                )}
                <div>
                  <p className="text-text-primary font-medium capitalize">{judgment.status}</p>
                  <p className="text-sm text-text-muted">{judgment.region}</p>
                </div>
              </div>
              <div className="text-right">
                <p className="text-text-primary">{judgment.latency}ms</p>
                <p className="text-sm text-text-muted">
                  {new Date(judgment.timestamp).toLocaleTimeString()}
                </p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function StatCard({ label, value, icon: Icon, color }: { label: string; value: string; icon: typeof Activity; color: string }) {
  return (
    <div className="bg-bg-card rounded-lg p-6 border border-bg-hover">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-text-muted text-sm">{label}</p>
          <p className="text-2xl font-bold text-text-primary mt-1">{value}</p>
        </div>
        <Icon className={`w-8 h-8 ${color}`} />
      </div>
    </div>
  )
}
