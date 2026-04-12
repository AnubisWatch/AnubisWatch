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
import type { AlertChannel, AlertRule } from '../api/client'

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
  const [saving, setSaving] = useState(false)

  // Channel form state
  const [chName, setChName] = useState('')
  const [chType, setChType] = useState<ChannelType>('webhook')
  const [chWebhookUrl, setChWebhookUrl] = useState('')
  const [chEmail, setChEmail] = useState('')
  const [chSlackUrl, setChSlackUrl] = useState('')
  const [chEnabled, setChEnabled] = useState(true)

  // Rule form state
  const [ruleName, setRuleName] = useState('')
  const [ruleCondition, setRuleCondition] = useState('response_time')
  const [ruleThreshold, setRuleThreshold] = useState(5000)
  const [ruleSeverity, setRuleSeverity] = useState<Severity>('critical')
  const [ruleConsecutive, setRuleConsecutive] = useState(3)
  const [ruleDuration, setRuleDuration] = useState(60)
  const [ruleEnabled, setRuleEnabled] = useState(true)

  const resetChannelForm = () => {
    setChName('')
    setChType('webhook')
    setChWebhookUrl('')
    setChEmail('')
    setChSlackUrl('')
    setChEnabled(true)
    setSaving(false)
  }

  const resetRuleForm = () => {
    setRuleName('')
    setRuleCondition('response_time')
    setRuleThreshold(5000)
    setRuleSeverity('critical')
    setRuleConsecutive(3)
    setRuleDuration(60)
    setRuleEnabled(true)
    setSaving(false)
  }

  const {
    channels,
    loading: channelsLoading,
    error: channelsError,
    refetch: refetchChannels,
    createChannel,
    updateChannel,
    deleteChannel,
    testChannel
  } = useChannels()

  const {
    rules,
    loading: rulesLoading,
    error: rulesError,
    refetch: refetchRules,
    createRule,
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

  const handleCreateChannel = async () => {
    if (!chName.trim()) return
    setSaving(true)
    try {
      const config: Record<string, string> = {}
      if (chType === 'webhook') config.url = chWebhookUrl
      else if (chType === 'email') config.email = chEmail
      else if (chType === 'slack') config.webhook_url = chSlackUrl

      await createChannel({
        name: chName,
        type: chType,
        config,
        enabled: chEnabled
      } as Omit<AlertChannel, 'id'>)
      setShowChannelModal(false)
      resetChannelForm()
    } catch {
      // Failed to create channel
    } finally {
      setSaving(false)
    }
  }

  const handleCreateRule = async () => {
    if (!ruleName.trim()) return
    setSaving(true)
    try {
      await createRule({
        name: ruleName,
        condition: ruleCondition,
        threshold: ruleThreshold,
        severity: ruleSeverity,
        consecutive: ruleConsecutive,
        duration: ruleDuration,
        enabled: ruleEnabled,
        channels: []
      } as Omit<AlertRule, 'id'>)
      setShowRuleModal(false)
      resetRuleForm()
    } catch {
      // Failed to create rule
    } finally {
      setSaving(false)
    }
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
            aria-label="Refresh"
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          {activeTab === 'rules' && (
            <button
              onClick={() => { resetRuleForm(); setShowRuleModal(true) }}
              className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20"
            >
              <Plus className="w-4 h-4" />
              Add Rule
            </button>
          )}
          {activeTab === 'channels' && (
            <button
              onClick={() => { resetChannelForm(); setShowChannelModal(true) }}
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
      <div className="flex items-center gap-2 border-b border-gray-700/50" role="tablist" aria-label="Alerts sections">
        {[
          { id: 'rules' as const, label: 'Alert Rules', count: stats.totalRules },
          { id: 'channels' as const, label: 'Channels', count: stats.totalChannels },
          { id: 'history' as const, label: 'History', count: alertHistory.length },
        ].map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            role="tab"
            aria-selected={activeTab === tab.id}
            aria-controls={`alerts-panel-${tab.id}`}
            id={`alerts-tab-${tab.id}`}
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
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden" role="tabpanel" id="alerts-panel-rules" aria-labelledby="alerts-tab-rules">
          {filteredRules.length === 0 ? (
            <div className="text-center py-16">
              <Settings className="w-12 h-12 text-gray-600 mx-auto mb-4" />
              <h3 className="text-lg font-semibold text-white mb-2">No alert rules yet</h3>
              <p className="text-gray-400 mb-4">Create your first alert rule to get notified when issues occur</p>
              <button
                onClick={() => { resetRuleForm(); setShowRuleModal(true) }}
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
                        role="switch"
                        aria-checked={rule.enabled}
                        aria-label={`${rule.enabled ? 'Disable' : 'Enable'} rule ${rule.name}`}
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
                        <button className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors" aria-label={`Edit rule`} title="Edit">
                          <Edit className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => handleDeleteRule(rule.id)}
                          className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                          aria-label={`Delete rule`}
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
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4" role="tabpanel" id="alerts-panel-channels" aria-labelledby="alerts-tab-channels">
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
                    aria-label={`Test channel ${channel.name}`}
                    title="Test"
                  >
                    {testingChannel === channel.id ? <Loader2 className="w-4 h-4 animate-spin" /> : <TestTube className="w-4 h-4" />}
                  </button>
                  <button
                    onClick={() => handleToggleChannel(channel.id, channel.enabled)}
                    className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                    aria-label={channel.enabled ? `Disable ${channel.name}` : `Enable ${channel.name}`}
                    title={channel.enabled ? 'Disable' : 'Enable'}
                  >
                    <ToggleRight className={`w-4 h-4 ${channel.enabled ? 'text-emerald-400' : ''}`} />
                  </button>
                  <button
                    className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                    aria-label={`Edit channel ${channel.name}`}
                    title="Edit"
                  >
                    <Edit className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleDeleteChannel(channel.id)}
                    className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                    aria-label={`Delete channel ${channel.name}`}
                    title="Delete"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            )
          })}
          <button
            onClick={() => { resetChannelForm(); setShowChannelModal(true) }}
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
        <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden" role="tabpanel" id="alerts-panel-history" aria-labelledby="alerts-tab-history">
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

      {/* Channel Modal */}
      {showChannelModal && (
        <div
          className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50"
          role="dialog"
          aria-modal="true"
          aria-labelledby="channel-modal-title"
          onKeyDown={(e) => { if (e.key === 'Escape') setShowChannelModal(false) }}
        >
          <div className="bg-gray-900 border border-gray-700/50 rounded-2xl w-full max-w-lg">
            <div className="flex items-center justify-between p-6 border-b border-gray-700/50">
              <div>
                <h2 id="channel-modal-title" className="text-xl font-semibold text-white">Add Notification Channel</h2>
                <p className="text-sm text-gray-400 mt-1">Where alerts will be sent</p>
              </div>
              <button onClick={() => setShowChannelModal(false)} className="p-2 text-gray-400 hover:text-white rounded-lg hover:bg-gray-800 transition-colors" aria-label="Close dialog">
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="p-6 space-y-5">
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Name</label>
                <input
                  type="text"
                  value={chName}
                  onChange={(e) => setChName(e.target.value)}
                  placeholder="e.g., Ops Slack"
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-3">Type</label>
                <div className="grid grid-cols-3 gap-3">
                  {([
                    { value: 'webhook' as ChannelType, label: 'Webhook' },
                    { value: 'slack' as ChannelType, label: 'Slack' },
                    { value: 'email' as ChannelType, label: 'Email' },
                    { value: 'discord' as ChannelType, label: 'Discord' },
                    { value: 'pagerduty' as ChannelType, label: 'PagerDuty' },
                  ]).map((t) => (
                    <button
                      key={t.value}
                      onClick={() => setChType(t.value)}
                      className={`p-3 rounded-xl text-sm font-medium transition-all ${
                        chType === t.value
                          ? 'bg-amber-500/10 border-2 border-amber-500 text-amber-400'
                          : 'bg-gray-950 border border-gray-700/50 text-gray-400 hover:border-gray-600'
                      }`}
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
              </div>

              {chType === 'webhook' && (
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Webhook URL</label>
                  <input
                    type="url"
                    value={chWebhookUrl}
                    onChange={(e) => setChWebhookUrl(e.target.value)}
                    placeholder="https://example.com/webhook"
                    className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                  />
                </div>
              )}

              {chType === 'slack' && (
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Slack Webhook URL</label>
                  <input
                    type="url"
                    value={chSlackUrl}
                    onChange={(e) => setChSlackUrl(e.target.value)}
                    placeholder="https://hooks.slack.com/services/..."
                    className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                  />
                </div>
              )}

              {chType === 'email' && (
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Email Address</label>
                  <input
                    type="email"
                    value={chEmail}
                    onChange={(e) => setChEmail(e.target.value)}
                    placeholder="ops@example.com"
                    className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                  />
                </div>
              )}

              <label className="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={chEnabled}
                  onChange={(e) => setChEnabled(e.target.checked)}
                  className="w-5 h-5 rounded border-gray-600 bg-gray-800 text-emerald-500 focus:ring-emerald-500"
                />
                <span className="text-sm text-gray-300">Enabled</span>
              </label>
            </div>

            <div className="flex items-center justify-end gap-3 p-6 border-t border-gray-700/50">
              <button onClick={() => setShowChannelModal(false)} className="px-5 py-2.5 text-gray-400 hover:text-white transition-colors">Cancel</button>
              <button
                onClick={handleCreateChannel}
                disabled={saving || !chName.trim()}
                className="px-5 py-2.5 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-xl transition-colors font-medium"
              >
                {saving ? 'Creating...' : 'Add Channel'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Rule Modal */}
      {showRuleModal && (
        <div
          className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50"
          role="dialog"
          aria-modal="true"
          aria-labelledby="rule-modal-title"
          onKeyDown={(e) => { if (e.key === 'Escape') setShowRuleModal(false) }}
        >
          <div className="bg-gray-900 border border-gray-700/50 rounded-2xl w-full max-w-lg">
            <div className="flex items-center justify-between p-6 border-b border-gray-700/50">
              <div>
                <h2 id="rule-modal-title" className="text-xl font-semibold text-white">Add Alert Rule</h2>
                <p className="text-sm text-gray-400 mt-1">Define when to trigger alerts</p>
              </div>
              <button onClick={() => setShowRuleModal(false)} className="p-2 text-gray-400 hover:text-white rounded-lg hover:bg-gray-800 transition-colors" aria-label="Close dialog">
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="p-6 space-y-5">
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Name</label>
                <input
                  type="text"
                  value={ruleName}
                  onChange={(e) => setRuleName(e.target.value)}
                  placeholder="e.g., High Latency"
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Condition</label>
                <select
                  value={ruleCondition}
                  onChange={(e) => setRuleCondition(e.target.value)}
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                >
                  <option value="response_time">Response Time &gt; threshold</option>
                  <option value="error_rate">Error Rate &gt; threshold</option>
                  <option value="downtime">Service Down</option>
                  <option value="ssl_expiry">SSL Certificate Expiring</option>
                </select>
              </div>

              <div className="grid grid-cols-3 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Threshold (ms)</label>
                  <input
                    type="number"
                    value={ruleThreshold}
                    onChange={(e) => setRuleThreshold(parseInt(e.target.value) || 5000)}
                    min={100}
                    className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Duration (s)</label>
                  <input
                    type="number"
                    value={ruleDuration}
                    onChange={(e) => setRuleDuration(parseInt(e.target.value) || 60)}
                    min={1}
                    className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Consecutive</label>
                  <input
                    type="number"
                    value={ruleConsecutive}
                    onChange={(e) => setRuleConsecutive(parseInt(e.target.value) || 3)}
                    min={1}
                    max={10}
                    className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                  />
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-3">Severity</label>
                <div className="grid grid-cols-3 gap-3">
                  {([
                    { value: 'critical' as Severity, label: 'Critical', color: 'text-rose-400 bg-rose-500/10' },
                    { value: 'warning' as Severity, label: 'Warning', color: 'text-amber-400 bg-amber-500/10' },
                    { value: 'info' as Severity, label: 'Info', color: 'text-blue-400 bg-blue-500/10' },
                  ]).map((s) => (
                    <button
                      key={s.value}
                      onClick={() => setRuleSeverity(s.value)}
                      className={`p-3 rounded-xl text-sm font-medium transition-all ${
                        ruleSeverity === s.value
                          ? `${s.color} border-2 border-current`
                          : 'bg-gray-950 border border-gray-700/50 text-gray-400 hover:border-gray-600'
                      }`}
                    >
                      {s.label}
                    </button>
                  ))}
                </div>
              </div>

              <label className="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={ruleEnabled}
                  onChange={(e) => setRuleEnabled(e.target.checked)}
                  className="w-5 h-5 rounded border-gray-600 bg-gray-800 text-emerald-500 focus:ring-emerald-500"
                />
                <span className="text-sm text-gray-300">Enabled</span>
              </label>
            </div>

            <div className="flex items-center justify-end gap-3 p-6 border-t border-gray-700/50">
              <button onClick={() => setShowRuleModal(false)} className="px-5 py-2.5 text-gray-400 hover:text-white transition-colors">Cancel</button>
              <button
                onClick={handleCreateRule}
                disabled={saving || !ruleName.trim()}
                className="px-5 py-2.5 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-xl transition-colors font-medium"
              >
                {saving ? 'Creating...' : 'Add Rule'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
