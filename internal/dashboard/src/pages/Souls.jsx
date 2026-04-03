import { useState, useEffect } from 'react'

function Souls() {
  const [souls, setSouls] = useState([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState('all')

  useEffect(() => {
    fetchSouls()
  }, [])

  const fetchSouls = async () => {
    try {
      const res = await fetch('/api/v1/souls')
      if (res.ok) {
        const data = await res.json()
        setSouls(data)
      }
    } catch (err) {
      console.error('Failed to fetch souls:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleForceCheck = async (soulId) => {
    try {
      await fetch(`/api/v1/souls/${soulId}/check`, { method: 'POST' })
      // Refresh after a delay
      setTimeout(fetchSouls, 2000)
    } catch (err) {
      console.error('Failed to force check:', err)
    }
  }

  const filteredSouls = souls.filter((soul) => {
    if (filter === 'all') return true
    // In production, filter by actual status
    return true
  })

  const getStatusColor = (status) => {
    switch (status) {
      case 'alive':
        return 'bg-green-500'
      case 'degraded':
        return 'bg-amber-500'
      case 'dead':
        return 'bg-red-500'
      default:
        return 'bg-slate-500'
    }
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-display text-2xl font-bold text-amber-400">Souls</h2>
          <p className="text-slate-400">Manage monitored targets</p>
        </div>
        <div className="flex items-center gap-4">
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="bg-slate-800 border border-slate-700 rounded-lg px-4 py-2 text-sm"
          >
            <option value="all">All Status</option>
            <option value="alive">Healthy</option>
            <option value="degraded">Degraded</option>
            <option value="dead">Dead</option>
          </select>
          <button className="btn-egyptian">
            + Add Soul
          </button>
        </div>
      </div>

      {/* Souls Table */}
      <div className="card-egyptian p-6">
        {loading ? (
          <div className="text-center py-12 text-slate-500 loading-gold">
            Loading souls...
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="table-egyptian w-full">
              <thead>
                <tr>
                  <th>Status</th>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Target</th>
                  <th>Interval</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {filteredSouls.length === 0 ? (
                  <tr>
                    <td colSpan="6" className="text-center py-12 text-slate-500">
                      No souls found. Create one to get started.
                    </td>
                  </tr>
                ) : (
                  filteredSouls.map((soul) => (
                    <tr key={soul.id}>
                      <td>
                        <span className={`inline-block w-3 h-3 rounded-full ${getStatusColor('alive')} shadow-lg`}></span>
                      </td>
                      <td className="font-medium">{soul.name}</td>
                      <td>
                        <span className="px-2 py-1 bg-slate-800 rounded text-xs uppercase">
                          {soul.type}
                        </span>
                      </td>
                      <td className="font-mono text-sm text-slate-400">{soul.target}</td>
                      <td className="text-slate-400">{soul.weight || '30s'}</td>
                      <td>
                        <div className="flex items-center gap-2">
                          <button
                            onClick={() => handleForceCheck(soul.id)}
                            className="p-2 text-slate-400 hover:text-amber-400 transition-colors"
                            title="Force Check"
                          >
                            ⟳
                          </button>
                          <button className="p-2 text-slate-400 hover:text-amber-400 transition-colors">
                            ✎
                          </button>
                          <button className="p-2 text-slate-400 hover:text-red-400 transition-colors">
                            🗑
                          </button>
                        </div>
                      </td>
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

export default Souls
