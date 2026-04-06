import { useState } from 'react'
import { Network, Crown, Users, Activity, Server, CheckCircle, XCircle, Clock } from 'lucide-react'

export function Cluster() {
  const [clusterInfo] = useState({
    node_id: 'jackal-01',
    state: 'leader',
    is_leader: true,
    term: 42,
    leader: 'jackal-01',
    peer_count: 3,
  })

  const nodes = [
    { id: 'jackal-01', region: 'us-east', status: 'healthy', role: 'leader', last_contact: 'now', uptime: '14d 3h', version: 'v1.0.0' },
    { id: 'jackal-02', region: 'eu-west', status: 'healthy', role: 'follower', last_contact: '2s ago', uptime: '14d 2h', version: 'v1.0.0' },
    { id: 'jackal-03', region: 'ap-south', status: 'healthy', role: 'follower', last_contact: '1s ago', uptime: '13d 22h', version: 'v1.0.0' },
  ]

  const stats = {
    total_checks: 2847293,
    checks_per_minute: 245,
    active_souls: 42,
    replicated_logs: 2847293,
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-text-primary">Necropolis Cluster</h1>
        <p className="text-text-muted mt-1">Distributed monitoring nodes</p>
      </div>

      {/* Cluster Stats */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <StatCard label="Node ID" value={clusterInfo.node_id} icon={Server} />
        <StatCard label="Role" value={clusterInfo.state} icon={Crown} color={clusterInfo.is_leader ? 'text-accent' : 'text-text-secondary'} />
        <StatCard label="Term" value={clusterInfo.term.toString()} icon={Activity} />
        <StatCard label="Peers" value={clusterInfo.peer_count.toString()} icon={Users} />
      </div>

      {/* Stats Row */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div className="bg-bg-card rounded-lg p-6 border border-bg-hover">
          <p className="text-text-muted text-sm">Total Checks</p>
          <p className="text-3xl font-bold text-text-primary mt-2">{stats.total_checks.toLocaleString()}</p>
        </div>
        <div className="bg-bg-card rounded-lg p-6 border border-bg-hover">
          <p className="text-text-muted text-sm">Checks / Minute</p>
          <p className="text-3xl font-bold text-text-primary mt-2">{stats.checks_per_minute}</p>
        </div>
        <div className="bg-bg-card rounded-lg p-6 border border-bg-hover">
          <p className="text-text-muted text-sm">Active Souls</p>
          <p className="text-3xl font-bold text-text-primary mt-2">{stats.active_souls}</p>
        </div>
      </div>

      {/* Nodes Table */}
      <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
        <div className="p-4 border-b border-bg-hover">
          <h2 className="font-semibold text-text-primary flex items-center gap-2">
            <Network className="w-5 h-5 text-primary" />
            Jackals (Nodes)
          </h2>
        </div>
        <table className="w-full">
          <thead className="bg-bg-hover/50">
            <tr>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Node ID</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Region</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Status</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Role</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Uptime</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Version</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Last Contact</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-bg-hover">
            {nodes.map((node) => (
              <tr key={node.id} className="hover:bg-bg-hover/30">
                <td className="px-6 py-4">
                  <div className="flex items-center gap-3">
                    <div className={`w-2 h-2 rounded-full ${node.status === 'healthy' ? 'bg-success' : 'bg-error'}`} />
                    <span className="font-medium text-text-primary">{node.id}</span>
                    {node.id === clusterInfo.node_id && (
                      <span className="px-2 py-0.5 bg-primary/10 text-primary text-xs rounded">You</span>
                    )}
                  </div>
                </td>
                <td className="px-6 py-4 text-text-secondary">{node.region}</td>
                <td className="px-6 py-4">
                  <span className={`inline-flex items-center gap-1.5 text-sm ${
                    node.status === 'healthy' ? 'text-success' : 'text-error'
                  }`}>
                    {node.status === 'healthy' ? <CheckCircle className="w-4 h-4" /> : <XCircle className="w-4 h-4" />}
                    <span className="capitalize">{node.status}</span>
                  </span>
                </td>
                <td className="px-6 py-4">
                  <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${
                    node.role === 'leader'
                      ? 'bg-accent/10 text-accent'
                      : 'bg-bg-hover text-text-secondary'
                  }`}>
                    {node.role === 'leader' && <Crown className="w-3 h-3" />}
                    <span className="capitalize">{node.role}</span>
                  </span>
                </td>
                <td className="px-6 py-4 text-text-secondary">{node.uptime}</td>
                <td className="px-6 py-4 text-text-secondary">{node.version}</td>
                <td className="px-6 py-4">
                  <span className="text-sm text-text-secondary flex items-center gap-1">
                    <Clock className="w-3 h-3" />
                    {node.last_contact}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Raft Info */}
      <div className="bg-bg-card rounded-lg border border-bg-hover p-6">
        <h2 className="font-semibold text-text-primary mb-4">Raft Consensus</h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div>
            <p className="text-sm text-text-muted">Current Term</p>
            <p className="text-2xl font-bold text-text-primary">{clusterInfo.term}</p>
          </div>
          <div>
            <p className="text-sm text-text-muted">Log Index</p>
            <p className="text-2xl font-bold text-text-primary">{stats.replicated_logs.toLocaleString()}</p>
          </div>
          <div>
            <p className="text-sm text-text-muted">Commit Index</p>
            <p className="text-2xl font-bold text-text-primary">{stats.replicated_logs.toLocaleString()}</p>
          </div>
        </div>
      </div>
    </div>
  )
}

function StatCard({ label, value, icon: Icon, color }: { label: string; value: string; icon: typeof Network; color?: string }) {
  return (
    <div className="bg-bg-card rounded-lg p-6 border border-bg-hover">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-text-muted text-sm">{label}</p>
          <p className={`text-xl font-bold mt-1 ${color || 'text-text-primary'}`}>{value}</p>
        </div>
        <Icon className="w-6 h-6 text-text-muted" />
      </div>
    </div>
  )
}
