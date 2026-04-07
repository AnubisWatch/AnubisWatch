import { useState } from 'react'
import {
  Network,
  Crown,
  Users,
  Server,
  CheckCircle2,
  XCircle,
  Clock,
  Activity,
  Database,
  Shield,
  Zap,
  RefreshCw,
  Cpu,
  Globe,
  Terminal,
  AlertCircle
} from 'lucide-react'
import { useClusterStatus, useStats } from '../api/hooks'

export function Cluster() {
  const [refreshing, setRefreshing] = useState(false)

  const {
    data: clusterData,
    loading: clusterLoading,
    error: clusterError,
    refetch: refetchCluster
  } = useClusterStatus()

  const {
    data: statsData,
    refetch: refetchStats
  } = useStats()

  const handleRefresh = async () => {
    setRefreshing(true)
    await Promise.all([refetchCluster(), refetchStats()])
    setTimeout(() => setRefreshing(false), 500)
  }

  // Default values when data is not available
  const isClustered = clusterData?.is_clustered ?? false
  const nodeId = clusterData?.node_id || 'standalone'
  const state = clusterData?.state || 'solo'
  const isLeader = state === 'leader'
  const term = clusterData?.term || 0
  const peerCount = clusterData?.peer_count || 0

  // Mock nodes data - backend doesn't provide per-node metrics yet
  const nodes = [
    {
      id: nodeId,
      region: 'default',
      status: 'healthy' as const,
      role: isLeader ? 'leader' as const : 'follower' as const,
      last_contact: 'now',
      uptime: '0d 0h',
      version: 'v1.0.0',
      cpu: 45,
      memory: 62,
      disk: 34
    }
  ]

  const stats = {
    total_checks: statsData?.judgments?.today || 0,
    checks_per_minute: 0,
    active_souls: statsData?.souls?.total || 0,
    replicated_logs: statsData?.judgments?.today || 0,
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'bg-emerald-500'
      case 'unhealthy': return 'bg-amber-500'
      case 'offline': return 'bg-rose-500'
      default: return 'bg-gray-500'
    }
  }

  const getStatusTextColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'text-emerald-400'
      case 'unhealthy': return 'text-amber-400'
      case 'offline': return 'text-rose-400'
      default: return 'text-gray-400'
    }
  }

  if (clusterLoading) {
    return (
      <div className="flex items-center justify-center py-32">
        <div className="w-10 h-10 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
      </div>
    )
  }

  if (clusterError) {
    return (
      <div className="text-center py-16">
        <AlertCircle className="w-12 h-12 text-rose-500 mx-auto mb-4" />
        <p className="text-gray-400">{clusterError}</p>
        <button
          onClick={handleRefresh}
          className="mt-4 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
        >
          Try Again
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white tracking-tight">Cluster</h1>
          <p className="text-gray-400 mt-1 text-sm">
            {isClustered ? 'Distributed monitoring nodes and Raft consensus' : 'Standalone node configuration'}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            className={`p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all ${refreshing ? 'animate-spin' : ''}`}
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          {isClustered && (
            <button className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20">
              <Network className="w-4 h-4" />
              Join Cluster
            </button>
          )}
        </div>
      </div>

      {/* Cluster Overview Stats */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Node ID</p>
              <p className="text-xl font-bold text-white mt-1">{nodeId}</p>
            </div>
            <div className="w-10 h-10 bg-gray-800 rounded-xl flex items-center justify-center">
              <Server className="w-5 h-5 text-gray-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Role</p>
              <div className="flex items-center gap-2 mt-1">
                <Crown className={`w-5 h-5 ${isLeader ? 'text-amber-400' : 'text-gray-400'}`} />
                <p className={`text-xl font-bold capitalize ${isLeader ? 'text-amber-400' : 'text-gray-400'}`}>
                  {state}
                </p>
              </div>
            </div>
            <div className="w-10 h-10 bg-amber-500/10 rounded-xl flex items-center justify-center">
              <Crown className="w-5 h-5 text-amber-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Term</p>
              <p className="text-xl font-bold text-white mt-1">{term}</p>
            </div>
            <div className="w-10 h-10 bg-blue-500/10 rounded-xl flex items-center justify-center">
              <Activity className="w-5 h-5 text-blue-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">{isClustered ? 'Peers' : 'Mode'}</p>
              <p className="text-xl font-bold text-white mt-1">
                {isClustered ? peerCount : 'Standalone'}
              </p>
            </div>
            <div className="w-10 h-10 bg-purple-500/10 rounded-xl flex items-center justify-center">
              {isClustered ? <Users className="w-5 h-5 text-purple-400" /> : <Server className="w-5 h-5 text-purple-400" />}
            </div>
          </div>
        </div>
      </div>

      {/* Performance Stats */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Today's Checks</p>
              <p className="text-2xl font-bold text-amber-400 mt-1">{stats.total_checks.toLocaleString()}</p>
            </div>
            <div className="w-12 h-12 bg-amber-500/10 rounded-xl flex items-center justify-center">
              <Activity className="w-6 h-6 text-amber-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Active Souls</p>
              <p className="text-2xl font-bold text-emerald-400 mt-1">{stats.active_souls}</p>
            </div>
            <div className="w-12 h-12 bg-emerald-500/10 rounded-xl flex items-center justify-center">
              <Database className="w-6 h-6 text-emerald-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Health Status</p>
              <p className="text-2xl font-bold text-blue-400 mt-1">
                {clusterData?.state === 'healthy' || clusterData?.state === 'leader' || clusterData?.state === 'follower' ? 'Healthy' : 'Unknown'}
              </p>
            </div>
            <div className="w-12 h-12 bg-blue-500/10 rounded-xl flex items-center justify-center">
              <Zap className="w-6 h-6 text-blue-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Cluster Mode</p>
              <p className="text-2xl font-bold text-purple-400 mt-1">{isClustered ? 'Enabled' : 'Disabled'}</p>
            </div>
            <div className="w-12 h-12 bg-purple-500/10 rounded-xl flex items-center justify-center">
              <Shield className="w-6 h-6 text-purple-400" />
            </div>
          </div>
        </div>
      </div>

      {/* Nodes Table */}
      <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden">
        <div className="p-5 border-b border-gray-700/50 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white flex items-center gap-2">
            <Network className="w-5 h-5 text-amber-400" />
            {isClustered ? 'Cluster Nodes' : 'Node Information'}
          </h2>
          <span className="text-sm text-gray-400">{nodes.length} node{nodes.length !== 1 ? 's' : ''}</span>
        </div>
        <table className="w-full">
          <thead className="bg-gray-800/50">
            <tr>
              <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Node</th>
              <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Status</th>
              <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Role</th>
              <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Region</th>
              <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Resources</th>
              <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Uptime</th>
              <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Version</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-700/50">
            {nodes.map((node) => (
              <tr key={node.id} className="hover:bg-gray-800/30 transition-colors">
                <td className="px-6 py-4">
                  <div className="flex items-center gap-3">
                    <div className={`w-2 h-2 rounded-full ${getStatusColor(node.status)}`} />
                    <div>
                      <p className="font-semibold text-white flex items-center gap-2">
                        {node.id}
                        <span className="px-2 py-0.5 bg-amber-500/10 text-amber-400 text-xs rounded font-medium">You</span>
                      </p>
                      <p className="text-xs text-gray-500 mt-0.5 flex items-center gap-1">
                        <Clock className="w-3 h-3" />
                        {node.last_contact}
                      </p>
                    </div>
                  </div>
                </td>
                <td className="px-6 py-4">
                  <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-semibold ${getStatusTextColor(node.status)} bg-gray-800`}>
                    {node.status === 'healthy' ? <CheckCircle2 className="w-3.5 h-3.5" /> : <XCircle className="w-3.5 h-3.5" />}
                    <span className="capitalize">{node.status}</span>
                  </span>
                </td>
                <td className="px-6 py-4">
                  <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-semibold ${
                    node.role === 'leader'
                      ? 'bg-amber-500/10 text-amber-400'
                      : 'bg-gray-800 text-gray-400'
                  }`}>
                    {node.role === 'leader' && <Crown className="w-3.5 h-3.5" />}
                    <span className="capitalize">{node.role}</span>
                  </span>
                </td>
                <td className="px-6 py-4">
                  <div className="flex items-center gap-2 text-gray-400">
                    <Globe className="w-4 h-4" />
                    <span className="text-sm">{node.region}</span>
                  </div>
                </td>
                <td className="px-6 py-4">
                  <div className="flex items-center gap-4">
                    <div className="flex items-center gap-2" title="CPU">
                      <Cpu className="w-4 h-4 text-gray-500" />
                      <div className="w-12 h-1.5 bg-gray-800 rounded-full overflow-hidden">
                        <div className={`h-full rounded-full ${node.cpu > 70 ? 'bg-rose-500' : node.cpu > 50 ? 'bg-amber-500' : 'bg-emerald-500'}`} style={{ width: `${node.cpu}%` }} />
                      </div>
                      <span className="text-xs text-gray-400">{node.cpu}%</span>
                    </div>
                    <div className="flex items-center gap-2" title="Memory">
                      <Database className="w-4 h-4 text-gray-500" />
                      <span className="text-xs text-gray-400">{node.memory}%</span>
                    </div>
                  </div>
                </td>
                <td className="px-6 py-4">
                  <span className="text-sm text-gray-400 font-mono">{node.uptime}</span>
                </td>
                <td className="px-6 py-4">
                  <span className="text-sm text-gray-400 font-mono">{node.version}</span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Raft Consensus */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
          <h2 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
            <Shield className="w-5 h-5 text-purple-400" />
            {isClustered ? 'Raft Consensus' : 'Node Status'}
          </h2>
          <div className="grid grid-cols-3 gap-4">
            <div className="p-4 bg-gray-800/50 rounded-xl">
              <p className="text-sm text-gray-500 mb-1">Current Term</p>
              <p className="text-2xl font-bold text-amber-400">{term}</p>
            </div>
            <div className="p-4 bg-gray-800/50 rounded-xl">
              <p className="text-sm text-gray-500 mb-1">State</p>
              <p className="text-2xl font-bold text-blue-400 capitalize">{state}</p>
            </div>
            <div className="p-4 bg-gray-800/50 rounded-xl">
              <p className="text-sm text-gray-500 mb-1">Peers</p>
              <p className="text-2xl font-bold text-emerald-400">{peerCount}</p>
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
          <h2 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
            <Terminal className="w-5 h-5 text-emerald-400" />
            Quick Actions
          </h2>
          <div className="grid grid-cols-2 gap-3">
            <button className="p-3 bg-gray-800/50 hover:bg-gray-800 rounded-xl text-left transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
              <p className="text-white font-medium">{isClustered ? 'Add Node' : 'Enable Clustering'}</p>
              <p className="text-sm text-gray-500">
                {isClustered ? 'Join new node to cluster' : 'Switch to clustered mode'}
              </p>
            </button>
            <button className="p-3 bg-gray-800/50 hover:bg-gray-800 rounded-xl text-left transition-colors disabled:opacity-50 disabled:cursor-not-allowed" disabled={!isClustered}>
              <p className="text-white font-medium">Remove Node</p>
              <p className="text-sm text-gray-500">Safely remove a node</p>
            </button>
            <button className="p-3 bg-gray-800/50 hover:bg-gray-800 rounded-xl text-left transition-colors">
              <p className="text-white font-medium">Backup Data</p>
              <p className="text-sm text-gray-500">Create cluster snapshot</p>
            </button>
            <button className="p-3 bg-gray-800/50 hover:bg-gray-800 rounded-xl text-left transition-colors">
              <p className="text-white font-medium">View Logs</p>
              <p className="text-sm text-gray-500">Raft operation logs</p>
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
