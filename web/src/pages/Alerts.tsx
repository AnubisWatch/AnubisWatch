import { useState } from 'react'
import {
  Bell,
  Plus,
  Check,
  AlertTriangle,
  AlertCircle,
  Info,
  Trash2,
  Edit,
  TestTube,
  Search,
  Filter,
  ChevronDown,
  RefreshCw,
  Mail,
  MessageSquare,
  Phone,
  Webhook,
  MoreHorizontal,
  Settings,
  BellRing,
  ToggleRight,
  Loader2,
  X
} from 'lucide-react'
import { useChannels, useRules } from '../api/hooks'

type Severity = 'critical' | 'warning' | 'info'
type ChannelType = 'slack' | 'email' | 'pagerduty' | 'webhook' | 'discord'

interface AlertHistoryItem {
  id: string
  rule: string
  soul: string
  severity: Severity
  status: 'active' | 'resolved' | 'acknowledged'
  triggered_at: string
  resolved_at?: string
  message: string
}

const severityConfig: Record<Severity, { icon: typeof AlertCircle; color: string; bg: string; label: string }> = {
  critical: { icon: AlertCircle, color: 'text-rose-400', bg: 'bg-rose-500/10', label: 'Critical' },
  warning: { icon: AlertTriangle, color: 'text-amber-400', bg: 'bg-amber-500/10', label: 'Warning' },
  info: { icon: Info, color: 'text-blue-400', bg: 'bg-blue-500/10', label: 'Info' },
}

const channelConfig: Record<ChannelType, { icon: typeof Mail; color: string; bg: string; label: string }> = {
  slack: { icon: MessageSquare, color: 'text-purple-400', bg: 'bg-purple-500/10', label: 'Slack' },
  email: { icon: Mail, color: 'text-blue-400', bg: 'bg-blue-500/10', label: 'Email' },
  pagerduty: { icon: Phone, color: 'text-rose-400', bg: 'bg-rose-500/10', label: 'PagerDuty' },
  webhook: { icon: Webhook, color: 'text-emerald-400', bg: 'bg-emerald-500/10', label: 'Webhook' },
  discord: { icon: MessageSquare, color: 'text-indigo-400', bg: 'bg-indigo-500/10', label: 'Discord' },
}

// Mock alert history - backend doesn't have this yet
const alertHistory: AlertHistoryItem[] = []

export function Alerts() {
  const [activeTab, setActiveTab] = useState<'rules' | 'channels' | 'history'>('rules')
  const [search, setSearch] = useState('')
  const [severityFilter, setSeverityFilter] = useState('all')
  const [showChannelModal, setShowChannelModal] = useState(false)
  const [showRuleModal, setShowRuleModal] = useState(false)
  const [testingChannel, setTestingChannel] = useState<string | null>(null)
  const [testResult, setTestResult] = useState<{ id: string; success: boolean; message: string } | null>(null)

  const {
    channels,
    loading: channelsLoading,
    error: channelsError,
    refetch: refetchChannels,
    updateChannel,
    deleteChannel,
    testChannel
  } = useChannels()

  const {
    rules,
    loading: rulesLoading,
    error: rulesError,
    refetch: refetchRules,
    updateRule,
    deleteRule
  } = useRules()

  const handleRefresh = async () => {
    if (activeTab === 'channels') await refetchChannels()
    if (activeTab === 'rules') await refetchRules()
  }

  const handleTestChannel = async (id: string) => {
    setTestingChannel(id)
    setTestResult(null)
    try {
      await testChannel(id)
      setTestResult({ id, success: true, message: 'Test notification sent successfully!' })
    } catch (err) {
      setTestResult({ id, success: false, message: err instanceof Error ? err.message : 'Test failed' })
    } finally {
      setTestingChannel(null)
      setTimeout(() => setTestResult(null), 5000)
    }
  }

  const handleToggleChannel = async (id: string, enabled: boolean) => {
    await updateChannel(id, { enabled: !enabled })
  }

  const handleToggleRule = async (id: string, enabled: boolean) => {
    await updateRule(id, { enabled: !enabled })
  }

  const handleDeleteChannel = async (id: string) => {
    if (!confirm('Are you sure you want to delete this channel?')) return
    await deleteChannel(id)
  }

  const handleDeleteRule = async (id: string) => {
    if (!confirm('Are you sure you want to delete this rule?')) return
    await deleteRule(id)
  }

  const stats = {
    totalRules: rules.length,
    activeRules: rules.filter(r => r.enabled).length,
    totalChannels: channels.length,
    activeChannels: channels.filter(c => c.enabled).length,
    activeAlerts: alertHistory.filter(a => a.status === 'active').length,
    criticalAlerts: alertHistory.filter(a => a.severity === 'critical' && a.status === 'active').length,
  }

  const filteredRules = rules.filter(rule => {
    const matchesSearch = rule.name.toLowerCase().includes(search.toLowerCase())
    const matchesSeverity = severityFilter === 'all' || rule.severity === severityFilter
    return matchesSearch && matchesSeverity
  })

  const getSeverityIcon = (severity: Severity) => {
    const Icon = severityConfig[severity].icon
    return <Icon className={`w-5 h-5 ${severityConfig[severity].color}`} />
  }

  const getSeverityBadge = (severity: Severity) => {
    const config = severityConfig[severity]
    const Icon = config.icon
    return (
      <span className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold ${config.bg} ${config.color}`}>
        <Icon className="w-3.5 h-3.5" />
        {config.label}
      </span>
    )
  }

  const loading = activeTab === 'channels' ? channelsLoading : activeTab === 'rules' ? rulesLoading : false
  const error = activeTab === 'channels' ? channelsError : activeTab === 'rules' ? rulesError : null

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white tracking-tight">Alerts</h1>
          <p className="text-gray-400 mt-1 text-sm">Configure alert rules and notification channels</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            className={`p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all ${loading ? 'animate-spin' : ''}`}
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          {activeTab === 'rules' && (
            <button
              onClick={() => setShowRuleModal(true)}
              className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20"
            >
              <Plus className="w-4 h-4" />
              Add Rule
            </button>
          )}
          {activeTab === 'channels' && (
            <button
              onClick={() => setShowChannelModal(true)}
              className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20"
            >
              <Plus className="w-4 h-4" />
              Add Channel
            </button>
          )}
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Total Rules</p>
              <p className="text-2xl font-bold text-white mt-1">{stats.totalRules}</p>
            </div>
            <div className="w-10 h-10 bg-gray-800 rounded-xl flex items-center justify-center">
              <Settings className="w-5 h-5 text-gray-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Active Rules</p>
              <p className="text-2xl font-bold text-emerald-400 mt-1">{stats.activeRules}</p>
            </div>
            <div className="w-10 h-10 bg-emerald-500/10 rounded-xl flex items-center justify-center">
              <ToggleRight className="w-5 h-5 text-emerald-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Channels</p>
              <p className="text-2xl font-bold text-amber-400 mt-1">{stats.totalChannels}</p>
            </div>
            <div className="w-10 h-10 bg-amber-500/10 rounded-xl flex items-center justify-center">
              <Bell className="w-5 h-5 text-amber-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Active Alerts</p>
              <p className="text-2xl font-bold text-rose-400 mt-1">{stats.activeAlerts}</p>
            </div>
            <div className="w-10 h-10 bg-rose-500/10 rounded-xl flex items-center justify-center">
              <BellRing className="w-5 h-5 text-rose-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Critical</p>
              <p className="text-2xl font-bold text-rose-500 mt-1">{stats.criticalAlerts}</p>
            </div>
            <div className="w-10 h-10 bg-rose-500/20 rounded-xl flex items-center justify-center">
              <AlertCircle className="w-5 h-5 text-rose-500" />
            </div>
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex items-center gap-2 border-b border-gray-700/50">
        {[
          { id: 'rules' as const, label: 'Alert Rules', count: stats.totalRules },
          { id: 'channels' as const, label: 'Channels', count: stats.totalChannels },
          { id: 'history' as const, label: 'History', count: alertHistory.length },
        ].map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`px-6 py-3 font-medium text-sm transition-all border-b-2 flex items-center gap-2 ${
              activeTab === tab.id
                ? 'text-amber-400 border-amber-400'
                : 'text-gray-400 border-transparent hover:text-white'
            }`}
          >
            {tab.label}
            <span className={`px-2 py-0.5 rounded-full text-xs ${
              activeTab === tab.id ? 'bg-amber-500/10 text-amber-400' : 'bg-gray-800 text-gray-400'
            }`}>
              {tab.count}
            </span>
          </button>
        ))}
      </div>

      {/* Error State */}
      {error && (
        <div className="bg-rose-500/10 border border-rose-500/20 rounded-2xl p-6 text-center">
          <AlertCircle className="w-12 h-12 text-rose-500 mx-auto mb-3" />
          <p className="text-rose-400">{error}</p>
          <button
            onClick={handleRefresh}
            className="mt-4 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
          >
            Try Again
          </button>
        </div>
      )}

      {/* Search & Filter */}
      {activeTab === 'rules' && (
        <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-4">
          <div className="relative flex-1 max-w-md">
            <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
            <input
              type="text"
              placeholder="Search alert rules..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full bg-gray-900 border border-gray-700/50 rounded-xl pl-11 pr-4 py-3 text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50 transition-colors"
            />
          </div>
          <div className="relative">
            <Filter className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
            <select
              value={severityFilter}
              onChange={(e) => setSeverityFilter(e.target.value)}
              className="bg-gray-900 border border-gray-700/50 rounded-xl pl-10 pr-8 py-3 text-sm text-white focus:outline-none focus:border-amber-500/50 appearance-none cursor-pointer"
            >
              <option value="all">All Severities</option>
              <option value="critical">Critical</option>
              <option value="warning">Warning</option>
              <option value="info">Info</option>
            </select>
            <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 pointer-events-none" />
          </div>
        </div>
      )}

      {/* Content */}
      {activeTab === 'rules' && (
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden">
          {filteredRules.length === 0 ? (
            <div className="text-center py-16">
              <Settings className="w-12 h-12 text-gray-600 mx-auto mb-4" />
              <h3 className="text-lg font-semibold text-white mb-2">No alert rules yet</h3>
              <p className="text-gray-400 mb-4">Create your first alert rule to get notified when issues occur</p>
              <button
                onClick={() => setShowRuleModal(true)}
                className="px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
              >
                Create Rule
              </button>
            </div>
          ) : (
            <table className="w-full">
              <thead className="bg-gray-800/50">
                <tr>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Rule</th>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Condition</th>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Severity</th>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Channels</th>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Status</th>
                  <th className="text-right text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-700/50">
                {filteredRules.map((rule) => (
                  <tr key={rule.id} className="hover:bg-gray-800/30 transition-colors group">
                    <td className="px-6 py-4">
                      <div>
                        <p className="font-semibold text-white">{rule.name}</p>
                        {rule.created_at && (
                          <p className="text-xs text-gray-500 mt-1">Created: {new Date(rule.created_at).toLocaleDateString()}</p>
                        )}
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-gray-400 text-sm">{rule.condition} (threshold: {rule.threshold}ms)</span>
                    </td>
                    <td className="px-6 py-4">
                      {getSeverityBadge(rule.severity as Severity)}
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex gap-1.5 flex-wrap">
                        {rule.channels.map(channelId => {
                          const channel = channels.find(c => c.id === channelId)
                          return (
                            <span key={channelId} className="px-2.5 py-1 bg-gray-800 text-gray-400 text-xs rounded-lg font-medium">
                              {channel?.name || channelId}
                            </span>
                          )
                        })}
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <button
                        onClick={() => handleToggleRule(rule.id, rule.enabled)}
                        className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                          rule.enabled ? 'bg-emerald-500' : 'bg-gray-700'
                        }`}
                      >
                        <span
                          className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                            rule.enabled ? 'translate-x-6' : 'translate-x-1'
                          }`}
                        />
                      </button>
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center justify-end gap-1">
                        <button className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors" title="Edit">
                          <Edit className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => handleDeleteRule(rule.id)}
                          className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                          title="Delete"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {activeTab === 'channels' && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {channels.map((channel) => {
            const config = channelConfig[channel.type as ChannelType] || channelConfig.webhook
            const Icon = config.icon
            return (
              <div key={channel.id} className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-5 hover:border-gray-600 transition-all group">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-4">
                    <div className={`w-12 h-12 rounded-xl flex items-center justify-center ${config.bg}`}>
                      <Icon className={`w-6 h-6 ${config.color}`} />
                    </div>
                    <div>
                      <p className="font-semibold text-white">{channel.name}</p>
                      <p className="text-sm text-gray-500 uppercase tracking-wider mt-0.5">{config.label}</p>
                      <p className="text-xs text-gray-400 mt-1">
                        {Object.entries(channel.config).map(([k, v]) => `${k}: ${v}`).join(', ')}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className={`px-2.5 py-1 rounded-lg text-xs font-semibold ${
                      channel.enabled ? 'bg-emerald-500/10 text-emerald-400' : 'bg-gray-800 text-gray-500'
                    }`}>
                      {channel.enabled ? 'Active' : 'Disabled'}
                    </span>
                  </div>
                </div>

                {testResult?.id === channel.id && (
                  <div className={`mt-4 p-3 rounded-lg text-sm ${
                    testResult.success ? 'bg-emerald-500/10 text-emerald-400' : 'bg-rose-500/10 text-rose-400'
                  }`}>
                    {testResult.message}
                  </div>
                )}

                <div className="mt-4 pt-4 border-t border-gray-700/50 flex justify-end gap-1">
                  <button
                    onClick={() => handleTestChannel(channel.id)}
                    disabled={testingChannel === channel.id}
                    className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors disabled:opacity-50"
                    title="Test"
                  >
                    {testingChannel === channel.id ? <Loader2 className="w-4 h-4 animate-spin" /> : <TestTube className="w-4 h-4" />}
                  </button>
                  <button
                    onClick={() => handleToggleChannel(channel.id, channel.enabled)}
                    className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                    title={channel.enabled ? 'Disable' : 'Enable'}
                  >
                    <ToggleRight className={`w-4 h-4 ${channel.enabled ? 'text-emerald-400' : ''}`} />
                  </button>
                  <button className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors" title="Edit">
                    <Edit className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleDeleteChannel(channel.id)}
                    className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                    title="Delete"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            )
          })}
          <button
            onClick={() => setShowChannelModal(true)}
            className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-dashed border-gray-700/50 rounded-2xl p-6 flex flex-col items-center justify-center gap-4 hover:border-amber-500 transition-all text-gray-500 min-h-[180px]"
          >
            <div className="w-14 h-14 rounded-full bg-gray-800 flex items-center justify-center">
              <Plus className="w-7 h-7" />
            </div>
            <div className="text-center">
              <p className="font-medium text-white">Add Channel</p>
              <p className="text-sm text-gray-500 mt-1">Configure new notification channel</p>
            </div>
          </button>
        </div>
      )}

      {activeTab === 'history' && (
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden">
          {alertHistory.length === 0 ? (
            <div className="text-center py-16">
              <Bell className="w-12 h-12 text-gray-600 mx-auto mb-4" />
              <h3 className="text-lg font-semibold text-white mb-2">No alert history yet</h3>
              <p className="text-gray-400">Triggered alerts will appear here</p>
            </div>
          ) : (
            <table className="w-full">
              <thead className="bg-gray-800/50">
                <tr>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Alert</th>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Soul</th>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Severity</th>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Status</th>
                  <th className="text-left text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Triggered</th>
                  <th className="text-right text-xs font-semibold text-gray-400 uppercase tracking-wider px-6 py-4">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-700/50">
                {alertHistory.map((alert) => (
                  <tr key={alert.id} className="hover:bg-gray-800/30 transition-colors">
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-3">
                        {getSeverityIcon(alert.severity)}
                        <div>
                          <p className="font-semibold text-white">{alert.rule}</p>
                          <p className="text-xs text-gray-500 mt-0.5">{alert.message}</p>
                        </div>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-gray-400">{alert.soul}</span>
                    </td>
                    <td className="px-6 py-4">
                      {getSeverityBadge(alert.severity)}
                    </td>
                    <td className="px-6 py-4">
                      <span className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold ${
                        alert.status === 'active' ? 'bg-rose-500/10 text-rose-400' :
                        alert.status === 'resolved' ? 'bg-emerald-500/10 text-emerald-400' :
                        'bg-amber-500/10 text-amber-400'
                      }`}>
                        <span className={`w-1.5 h-1.5 rounded-full ${
                          alert.status === 'active' ? 'bg-rose-500' :
                          alert.status === 'resolved' ? 'bg-emerald-500' :
                          'bg-amber-500'
                        }`} />
                        {alert.status.charAt(0).toUpperCase() + alert.status.slice(1)}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-gray-400 text-sm">{new Date(alert.triggered_at).toLocaleString()}</span>
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center justify-end gap-2">
                        {alert.status === 'active' && (
                          <button className="flex items-center gap-2 px-3 py-1.5 bg-emerald-500/10 text-emerald-400 rounded-lg text-sm font-medium hover:bg-emerald-500/20 transition-colors">
                            <Check className="w-4 h-4" />
                            Acknowledge
                          </button>
                        )}
                        <button className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors">
                          <MoreHorizontal className="w-4 h-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* Channel Modal Placeholder */}
      {showChannelModal && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50">
          <div className="bg-gray-900 border border-gray-700/50 rounded-2xl p-6 w-full max-w-lg">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-semibold text-white">Add Notification Channel</h2>
              <button onClick={() => setShowChannelModal(false)} className="p-2 text-gray-400 hover:text-white">
                <X className="w-5 h-5" />
              </button>
            </div>
            <p className="text-gray-400">Channel creation form coming soon...</p>
          </div>
        </div>
      )}

      {/* Rule Modal Placeholder */}
      {showRuleModal && (
        <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50">
          <div className="bg-gray-900 border border-gray-700/50 rounded-2xl p-6 w-full max-w-lg">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-semibold text-white">Add Alert Rule</h2>
              <button onClick={() => setShowRuleModal(false)} className="p-2 text-gray-400 hover:text-white">
                <X className="w-5 h-5" />
              </button>
            </div>
            <p className="text-gray-400">Rule creation form coming soon...</p>
          </div>
        </div>
      )}
    </div>
  )
}
