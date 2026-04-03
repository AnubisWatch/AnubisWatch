import { useState, useEffect } from 'react'

function Cluster() {
  const [status, setStatus] = useState(null)
  const [peers, setPeers] = useState([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchClusterStatus()
    fetchClusterPeers()
  }, [])

  const fetchClusterStatus = async () => {
    try {
      const res = await fetch('/api/v1/cluster/status')
      if (res.ok) {
        const data = await res.json()
        setStatus(data)
      }
    } catch (err) {
      console.error('Failed to fetch cluster status:', err)
    } finally {
      setLoading(false)
    }
  }

  const fetchClusterPeers = async () => {
    try {
      const res = await fetch('/api/v1/cluster/peers')
      if (res.ok) {
        const data = await res.json()
        setPeers(data.peers || [])
      }
    } catch (err) {
      console.error('Failed to fetch cluster peers:', err)
    }
  }

  const getRoleBadge = (role) => {
    const styles = {
      leader: 'bg-amber-400/10 text-amber-400 border-amber-400/20',
      follower: 'bg-blue-400/10 text-blue-400 border-blue-400/20',
      candidate: 'bg-purple-400/10 text-purple-400 border-purple-400/20',
    }
    return styles[role] || 'bg-slate-400/10 text-slate-400'
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-display text-2xl font-bold text-amber-400">Necropolis Cluster</h2>
          <p className="text-slate-400">Distributed monitoring nodes</p>
        </div>
        <button
          onClick={() => { fetchClusterStatus(); fetchClusterPeers(); }}
          className="btn-egyptian"
        >
          Refresh
        </button>
      </div>

      {/* Cluster Status */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div className="card-egyptian p-6">
          <div className="flex items-center gap-3 mb-2">
            <span className="text-2xl">𓂀</span>
            <span className="text-slate-400">Current Role</span>
          </div>
          <div className="text-2xl font-bold capitalize">
            {status?.role || 'Unknown'}
          </div>
          <div className="text-sm text-slate-500 mt-1">
            Term: {status?.term || '-'}
          </div>
        </div>

        <div className="card-egyptian p-6">
          <div className="flex items-center gap-3 mb-2">
            <span className="text-2xl">𓃥</span>
            <span className="text-slate-400">Jackal Nodes</span>
          </div>
          <div className="text-2xl font-bold">
            {peers.length + 1}
          </div>
          <div className="text-sm text-slate-500 mt-1">
            {peers.length} peers connected
          </div>
        </div>

        <div className="card-egyptian p-6">
          <div className="flex items-center gap-3 mb-2">
            <span className="text-2xl">⚖</span>
            <span className="text-slate-400">Leader</span>
          </div>
          <div className="text-lg font-bold font-mono truncate">
            {status?.leader_id || 'Unknown'}
          </div>
          <div className="text-sm text-slate-500 mt-1">
            {status?.role === 'leader' ? 'This node' : 'Remote node'}
          </div>
        </div>
      </div>

      {/* Peers Table */}
      <div className="card-egyptian p-6">
        <h3 className="font-display text-lg font-semibold text-amber-400 mb-4">
          Cluster Nodes
        </h3>
        {loading ? (
          <div className="text-center py-12 text-slate-500 loading-gold">
            Loading cluster status...
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="table-egyptian w-full">
              <thead>
                <tr>
                  <th>Node ID</th>
                  <th>Role</th>
                  <th>Address</th>
                  <th>Region</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {/* Current Node */}
                {status && (
                  <tr className="bg-amber-400/5">
                    <td className="font-mono text-sm">{status.node_id}</td>
                    <td>
                      <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border ${getRoleBadge(status.role)}`}>
                        <span>{status.role === 'leader' ? '👑' : status.role === 'follower' ? '⛨' : '🗳'}</span>
                        <span className="capitalize">{status.role}</span>
                      </span>
                    </td>
                    <td className="font-mono text-sm">localhost</td>
                    <td>{status.region || 'default'}</td>
                    <td>
                      <span className="inline-flex items-center gap-1.5 text-green-400">
                        <span className="w-2 h-2 rounded-full bg-green-500"></span>
                        Connected
                      </span>
                    </td>
                  </tr>
                )}
                {/* Peers */}
                {peers.map((peer) => (
                  <tr key={peer.id}>
                    <td className="font-mono text-sm">{peer.id}</td>
                    <td>
                      <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border ${getRoleBadge(peer.role || 'follower')}`}>
                        <span>⛨</span>
                        <span className="capitalize">{peer.role || 'follower'}</span>
                      </span>
                    </td>
                    <td className="font-mono text-sm">{peer.address}</td>
                    <td>{peer.region || 'default'}</td>
                    <td>
                      <span className="inline-flex items-center gap-1.5 text-green-400">
                        <span className="w-2 h-2 rounded-full bg-green-500"></span>
                        Connected
                      </span>
                    </td>
                  </tr>
                ))}
                {peers.length === 0 && !status && (
                  <tr>
                    <td colSpan="5" className="text-center py-12 text-slate-500">
                      No cluster nodes found. Running in standalone mode.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Cluster Actions */}
      <div className="card-egyptian p-6">
        <h3 className="font-display text-lg font-semibold text-amber-400 mb-4">
          Cluster Actions
        </h3>
        <div className="flex flex-wrap gap-4">
          <button className="btn-egyptian" disabled>
            Join Cluster
          </button>
          <button className="px-4 py-2 bg-slate-800 border border-slate-700 rounded-lg text-slate-400 hover:text-slate-200 transition-colors" disabled>
            Leave Cluster
          </button>
          <button className="px-4 py-2 bg-slate-800 border border-slate-700 rounded-lg text-slate-400 hover:text-slate-200 transition-colors" disabled>
            Transfer Leadership
          </button>
        </div>
        <p className="text-sm text-slate-500 mt-4">
          Cluster management actions are currently disabled. Configure clustering in settings to enable.
        </p>
      </div>
    </div>
  )
}

export default Cluster
