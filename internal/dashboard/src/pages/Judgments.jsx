import { useState, useEffect } from 'react'

function Judgments() {
  const [judgments, setJudgments] = useState([])
  const [loading, setLoading] = useState(true)
  const [soulFilter, setSoulFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')

  useEffect(() => {
    fetchJudgments()
  }, [])

  const fetchJudgments = async () => {
    try {
      const res = await fetch('/api/v1/judgments?limit=100')
      if (res.ok) {
        const data = await res.json()
        setJudgments(data)
      }
    } catch (err) {
      console.error('Failed to fetch judgments:', err)
    } finally {
      setLoading(false)
    }
  }

  const filteredJudgments = judgments.filter((j) => {
    if (statusFilter !== 'all' && j.status !== statusFilter) return false
    if (soulFilter && !j.soul_name?.toLowerCase().includes(soulFilter.toLowerCase())) return false
    return true
  })

  const getStatusBadge = (status) => {
    const styles = {
      alive: 'bg-green-400/10 text-green-400 border-green-400/20',
      degraded: 'bg-amber-400/10 text-amber-400 border-amber-400/20',
      dead: 'bg-red-400/10 text-red-400 border-red-400/20',
    }
    return styles[status] || 'bg-slate-400/10 text-slate-400'
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-display text-2xl font-bold text-amber-400">Judgments</h2>
          <p className="text-slate-400">View all check results</p>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-4">
        <input
          type="text"
          placeholder="Filter by soul..."
          value={soulFilter}
          onChange={(e) => setSoulFilter(e.target.value)}
          className="bg-slate-800 border border-slate-700 rounded-lg px-4 py-2 text-sm w-64"
        />
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="bg-slate-800 border border-slate-700 rounded-lg px-4 py-2 text-sm"
        >
          <option value="all">All Status</option>
          <option value="alive">Healthy</option>
          <option value="degraded">Degraded</option>
          <option value="dead">Dead</option>
        </select>
        <button
          onClick={fetchJudgments}
          className="btn-egyptian"
        >
          Refresh
        </button>
      </div>

      {/* Judgments Table */}
      <div className="card-egyptian p-6">
        {loading ? (
          <div className="text-center py-12 text-slate-500 loading-gold">
            Loading judgments...
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="table-egyptian w-full">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Soul</th>
                  <th>Status</th>
                  <th>Duration</th>
                  <th>Status Code</th>
                  <th>Message</th>
                </tr>
              </thead>
              <tbody>
                {filteredJudgments.length === 0 ? (
                  <tr>
                    <td colSpan="6" className="text-center py-12 text-slate-500">
                      No judgments found
                    </td>
                  </tr>
                ) : (
                  filteredJudgments.map((j) => (
                    <tr key={j.id}>
                      <td className="text-sm text-slate-400">
                        {new Date(j.timestamp).toLocaleString()}
                      </td>
                      <td className="font-medium">{j.soul_name || j.soul_id}</td>
                      <td>
                        <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border ${getStatusBadge(j.status)}`}>
                          {j.status === 'alive' && '✓'}
                          {j.status === 'degraded' && '⚠'}
                          {j.status === 'dead' && '✕'}
                          <span className="capitalize">{j.status}</span>
                        </span>
                      </td>
                      <td className="font-mono text-sm">
                        {j.duration ? `${(j.duration / 1000000).toFixed(0)}ms` : '-'}
                      </td>
                      <td>
                        {j.status_code ? (
                          <span className="px-2 py-1 bg-slate-800 rounded text-xs font-mono">
                            {j.status_code}
                          </span>
                        ) : (
                          '-'
                        )}
                      </td>
                      <td className="max-w-xs truncate text-slate-400">{j.message || '-'}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}

export default Judgments
