import { useState } from 'react'
import { Filter, CheckCircle, XCircle, Clock } from 'lucide-react'

export function Judgments() {
  const [filter, setFilter] = useState('all')

  // Mock judgments data
  const judgments = [
    { id: '1', soul_name: 'Production API', soul_id: '1', status: 'passed', latency_ms: 45, timestamp: new Date().toISOString(), region: 'us-east', purity_score: 95 },
    { id: '2', soul_name: 'Database Primary', soul_id: '2', status: 'passed', latency_ms: 12, timestamp: new Date(Date.now() - 30000).toISOString(), region: 'us-east', purity_score: 98 },
    { id: '3', soul_name: 'Redis Cache', soul_id: '3', status: 'passed', latency_ms: 3, timestamp: new Date(Date.now() - 60000).toISOString(), region: 'us-east', purity_score: 99 },
    { id: '4', soul_name: 'CDN Edge', soul_id: '4', status: 'failed', latency_ms: 5000, timestamp: new Date(Date.now() - 90000).toISOString(), region: 'eu-west', purity_score: 45, error: 'Connection timeout' },
    { id: '5', soul_name: 'SMTP Server', soul_id: '5', status: 'passed', latency_ms: 234, timestamp: new Date(Date.now() - 120000).toISOString(), region: 'us-east', purity_score: 88 },
    { id: '6', soul_name: 'DNS Resolver', soul_id: '6', status: 'passed', latency_ms: 8, timestamp: new Date(Date.now() - 150000).toISOString(), region: 'us-east', purity_score: 99 },
    { id: '7', soul_name: 'Production API', soul_id: '1', status: 'passed', latency_ms: 42, timestamp: new Date(Date.now() - 180000).toISOString(), region: 'us-east', purity_score: 96 },
    { id: '8', soul_name: 'Queue Worker', soul_id: '7', status: 'passed', latency_ms: 23, timestamp: new Date(Date.now() - 210000).toISOString(), region: 'us-west', purity_score: 92 },
  ]

  const filteredJudgments = filter === 'all'
    ? judgments
    : judgments.filter(j => j.status === filter)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Judgments</h1>
          <p className="text-text-muted mt-1">Review all health check executions</p>
        </div>
        <div className="flex items-center gap-2">
          <Filter className="w-4 h-4 text-text-muted" />
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="bg-bg-card border border-bg-hover rounded-lg px-4 py-2 text-sm text-text-primary"
          >
            <option value="all">All Judgments</option>
            <option value="passed">Passed Only</option>
            <option value="failed">Failed Only</option>
          </select>
        </div>
      </div>

      {/* Judgments List */}
      <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-bg-hover/50">
              <tr>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Status</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Soul</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Latency</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Purity Score</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Region</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-bg-hover">
              {filteredJudgments.map((judgment) => (
                <tr key={judgment.id} className="hover:bg-bg-hover/30 transition-colors">
                  <td className="px-6 py-4">
                    <div className={`inline-flex items-center gap-2 px-3 py-1 rounded-full text-sm font-medium ${
                      judgment.status === 'passed'
                        ? 'bg-success/10 text-success'
                        : 'bg-error/10 text-error'
                    }`}>
                      {judgment.status === 'passed' ? (
                        <CheckCircle className="w-4 h-4" />
                      ) : (
                        <XCircle className="w-4 h-4" />
                      )}
                      <span className="capitalize">{judgment.status}</span>
                    </div>
                  </td>
                  <td className="px-6 py-4">
                    <div>
                      <p className="font-medium text-text-primary">{judgment.soul_name}</p>
                      {judgment.error && (
                        <p className="text-sm text-error mt-1">{judgment.error}</p>
                      )}
                    </div>
                  </td>
                  <td className="px-6 py-4">
                    <span className="text-text-primary font-mono">{judgment.latency_ms}ms</span>
                  </td>
                  <td className="px-6 py-4">
                    <div className="flex items-center gap-2">
                      <div className="w-16 h-2 bg-bg-hover rounded-full overflow-hidden">
                        <div
                          className={`h-full rounded-full ${
                            judgment.purity_score >= 90 ? 'bg-success' :
                            judgment.purity_score >= 70 ? 'bg-warning' : 'bg-error'
                          }`}
                          style={{ width: `${judgment.purity_score}%` }}
                        />
                      </div>
                      <span className="text-sm text-text-secondary">{judgment.purity_score}</span>
                    </div>
                  </td>
                  <td className="px-6 py-4">
                    <span className="text-text-secondary">{judgment.region}</span>
                  </td>
                  <td className="px-6 py-4">
                    <div className="flex items-center gap-2 text-text-secondary">
                      <Clock className="w-4 h-4" />
                      <span>{new Date(judgment.timestamp).toLocaleTimeString()}</span>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Pagination */}
      <div className="flex items-center justify-between">
        <p className="text-sm text-text-muted">
          Showing {filteredJudgments.length} of 1,247 judgments
        </p>
        <div className="flex items-center gap-2">
          <button className="px-4 py-2 bg-bg-card border border-bg-hover rounded-lg text-text-secondary hover:text-text-primary disabled:opacity-50" disabled>
            Previous
          </button>
          <button className="px-4 py-2 bg-bg-card border border-bg-hover rounded-lg text-text-secondary hover:text-text-primary">
            Next
          </button>
        </div>
      </div>
    </div>
  )
}
