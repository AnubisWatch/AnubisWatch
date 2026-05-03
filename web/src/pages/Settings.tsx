import { useState, useEffect, useCallback } from 'react'
import {
  Settings as SettingsIcon,
  Shield,
  Bell,
  Database,
  Globe,
  Save,
  Check,
  Mail,
  Slack,
  MessageSquare,
  Moon,
  Sun,
  Monitor,
  Clock,
  ChevronRight,
  RefreshCw,
  Eye,
  EyeOff,
  Loader2,
  AlertCircle,
  Copy,
  X
} from 'lucide-react'
import { api } from '../api/client'
import { useAuth, useStats } from '../api/hooks'
import { useThemeStore, applyTheme } from '../stores/themeStore'

type TabId = 'general' | 'security' | 'notifications' | 'storage' | 'integrations'

interface ConfigData {
  instance_name?: string
  timezone?: string
  language?: string
  theme?: 'dark' | 'light' | 'system'
  retention_days?: number
  storage_path?: string
  auth_enabled?: boolean
  mcp_enabled?: boolean
  websocket_enabled?: boolean
}

export function Settings() {
  const [activeTab, setActiveTab] = useState<TabId>('general')
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [showApiKey, setShowApiKey] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [config, setConfig] = useState<ConfigData>({})
  const [editedConfig, setEditedConfig] = useState<ConfigData>({})
  const [hasChanges, setHasChanges] = useState(false)

  const { user } = useAuth()
  const { data: statsData } = useStats()
  const { theme, setTheme } = useThemeStore()

  // Fetch configuration
  const fetchConfig = useCallback(async () => {
    try {
      setLoading(true)
      const result = await api.get<ConfigData>('/api/v1/config')
      setConfig(result)
      setEditedConfig(result)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load configuration')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchConfig()
  }, [fetchConfig])

  // Track changes
  useEffect(() => {
    setHasChanges(JSON.stringify(config) !== JSON.stringify(editedConfig))
  }, [config, editedConfig])

  const handleSave = async () => {
    setSaving(true)
    try {
      await api.put('/api/v1/config', editedConfig)
      setConfig(editedConfig)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save configuration')
    } finally {
      setSaving(false)
    }
  }

  const handleChange = (key: keyof ConfigData, value: unknown) => {
    if (key === 'theme' && typeof value === 'string') {
      setTheme(value as 'dark' | 'light' | 'system')
      applyTheme(value as 'dark' | 'light' | 'system')
    }
    setEditedConfig(prev => ({ ...prev, [key]: value }))
  }

  const handleCopyApiKey = () => {
    if (user?.id) {
      navigator.clipboard.writeText(`anb_live_${user.id}`)
    }
  }

  const tabs = [
    { id: 'general' as TabId, label: 'General', icon: SettingsIcon, description: 'Basic configuration' },
    { id: 'security' as TabId, label: 'Security', icon: Shield, description: 'Authentication & access' },
    { id: 'notifications' as TabId, label: 'Notifications', icon: Bell, description: 'Alerts & channels' },
    { id: 'storage' as TabId, label: 'Storage', icon: Database, description: 'Data & retention' },
    { id: 'integrations' as TabId, label: 'Integrations', icon: Globe, description: 'API & webhooks' },
  ]

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white tracking-tight">Settings</h1>
          <p className="text-gray-400 mt-1 text-sm">Configure your AnubisWatch instance</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={fetchConfig}
            className="p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all"
            aria-label="Refresh configuration"
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          <button
            onClick={handleSave}
            disabled={!hasChanges || saving}
            className={`flex items-center gap-2 px-5 py-2.5 rounded-xl transition-all font-medium ${
              saved
                ? 'bg-emerald-600 text-white'
                : hasChanges
                ? 'bg-amber-600 hover:bg-amber-500 text-white shadow-lg shadow-amber-600/20'
                : 'bg-gray-700 text-gray-400 cursor-not-allowed'
            }`}
          >
            {saving ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : saved ? (
              <Check className="w-4 h-4" />
            ) : (
              <Save className="w-4 h-4" />
            )}
            {saving ? 'Saving...' : saved ? 'Saved!' : 'Save Changes'}
          </button>
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-32" role="status" aria-label="Loading settings">
          <div className="w-10 h-10 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
        </div>
      ) : (
        <>
      {/* Error */}
      {error && (
        <div className="bg-rose-500/10 border border-rose-500/20 rounded-2xl p-4 flex items-center gap-3">
          <AlertCircle className="w-5 h-5 text-rose-400" />
          <p className="text-rose-400">{error}</p>
          <button
            onClick={() => setError(null)}
            className="ml-auto p-1 text-rose-400 hover:text-rose-300"
            aria-label="Dismiss error"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      )}

      <div className="flex flex-col lg:flex-row gap-8">
        {/* Sidebar */}
        <div className="w-full lg:w-72 shrink-0">
          <nav className="space-y-1" role="tablist" aria-label="Settings sections">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                role="tab"
                aria-selected={activeTab === tab.id}
                aria-controls={`settings-panel-${tab.id}`}
                id={`settings-tab-${tab.id}`}
                className={`w-full flex items-center gap-4 px-4 py-3.5 rounded-xl transition-all text-left group ${
                  activeTab === tab.id
                    ? 'bg-amber-600/10 text-amber-400 border border-amber-500/20'
                    : 'text-gray-400 hover:bg-gray-800 hover:text-white'
                }`}
              >
                <tab.icon className={`w-5 h-5 ${activeTab === tab.id ? 'text-amber-400' : 'text-gray-500 group-hover:text-gray-400'}`} />
                <div className="flex-1">
                  <p className="font-medium">{tab.label}</p>
                  <p className="text-xs text-gray-500">{tab.description}</p>
                </div>
                <ChevronRight className={`w-4 h-4 transition-transform ${activeTab === tab.id ? 'rotate-90 text-amber-400' : ''}`} />
              </button>
            ))}
          </nav>
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0">
          {activeTab === 'general' && (
            <div className="space-y-6" role="tabpanel" id="settings-panel-general" aria-labelledby="settings-tab-general">
              <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
                <h2 className="text-lg font-semibold text-white mb-1">General Settings</h2>
                <p className="text-gray-400 text-sm mb-6">Configure basic instance settings</p>

                <div className="space-y-6">
                  <div>
                    <label className="block text-sm font-medium text-gray-300 mb-2">
                      Instance Name
                    </label>
                    <input
                      type="text"
                      value={editedConfig.instance_name || ''}
                      onChange={(e) => handleChange('instance_name', e.target.value)}
                      placeholder="AnubisWatch Production"
                      className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50 transition-colors"
                    />
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div>
                      <label className="block text-sm font-medium text-gray-300 mb-2">
                        Timezone
                      </label>
                      <select
                        value={editedConfig.timezone || 'UTC'}
                        onChange={(e) => handleChange('timezone', e.target.value)}
                        className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                      >
                        <option value="UTC">UTC</option>
                        <option value="America/New_York">America/New_York</option>
                        <option value="Europe/London">Europe/London</option>
                        <option value="Europe/Istanbul">Europe/Istanbul</option>
                        <option value="Asia/Tokyo">Asia/Tokyo</option>
                      </select>
                    </div>

                    <div>
                      <label className="block text-sm font-medium text-gray-300 mb-2">
                        Default Language
                      </label>
                      <select
                        value={editedConfig.language || 'en'}
                        onChange={(e) => handleChange('language', e.target.value)}
                        className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                      >
                        <option value="en">English</option>
                        <option value="tr">Türkçe</option>
                        <option value="de">Deutsch</option>
                        <option value="fr">Français</option>
                      </select>
                    </div>
                  </div>

                  <div>
                    <label className="block text-sm font-medium text-gray-300 mb-3">
                      Dashboard Theme
                    </label>
                    <div className="grid grid-cols-3 gap-4">
                      {(['dark', 'light', 'system'] as const).map((themeOption) => (
                        <button
                          key={themeOption}
                          onClick={() => {
                            setTheme(themeOption)
                            applyTheme(themeOption)
                            handleChange('theme', themeOption)
                          }}
                          className={`p-4 rounded-xl text-left transition-all ${
                            theme === themeOption
                              ? 'bg-amber-500/10 border-2 border-amber-500'
                              : 'bg-gray-950 border border-gray-700/50 hover:border-gray-600'
                          }`}
                        >
                          {themeOption === 'dark' && <Moon className={`w-6 h-6 mb-2 ${theme === themeOption ? 'text-amber-400' : 'text-gray-400'}`} />}
                          {themeOption === 'light' && <Sun className={`w-6 h-6 mb-2 ${theme === themeOption ? 'text-amber-400' : 'text-gray-400'}`} />}
                          {themeOption === 'system' && <Monitor className={`w-6 h-6 mb-2 ${theme === themeOption ? 'text-amber-400' : 'text-gray-400'}`} />}
                          <p className={`font-medium ${theme === themeOption ? 'text-white' : 'text-gray-300'}`}>
                            {themeOption.charAt(0).toUpperCase() + themeOption.slice(1)}
                          </p>
                        </button>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'security' && (
            <div className="space-y-6" role="tabpanel" id="settings-panel-security" aria-labelledby="settings-tab-security">
              <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
                <h2 className="text-lg font-semibold text-white mb-1">Security Settings</h2>
                <p className="text-gray-400 text-sm mb-6">Configure authentication and access control</p>

                <div className="space-y-6">
                  <div className="pt-6 border-t border-gray-700/50 space-y-4">
                    <div className="flex items-center justify-between py-2">
                      <div>
                        <p className="font-medium text-white">Authentication</p>
                        <p className="text-sm text-gray-500">Require login to access dashboard</p>
                      </div>
                      <button
                        onClick={() => handleChange('auth_enabled', !editedConfig.auth_enabled)}
                        role="switch"
                        aria-checked={editedConfig.auth_enabled}
                        className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                          editedConfig.auth_enabled ? 'bg-emerald-500' : 'bg-gray-700'
                        }`}
                      >
                        <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                          editedConfig.auth_enabled ? 'translate-x-6' : 'translate-x-1'
                        }`} />
                      </button>
                    </div>

                    <div className="flex items-center justify-between py-2">
                      <div>
                        <p className="font-medium text-white">MCP Server</p>
                        <p className="text-sm text-gray-500">Enable Model Context Protocol endpoint</p>
                      </div>
                      <button
                        onClick={() => handleChange('mcp_enabled', !editedConfig.mcp_enabled)}
                        role="switch"
                        aria-checked={editedConfig.mcp_enabled}
                        className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                          editedConfig.mcp_enabled ? 'bg-emerald-500' : 'bg-gray-700'
                        }`}
                      >
                        <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                          editedConfig.mcp_enabled ? 'translate-x-6' : 'translate-x-1'
                        }`} />
                      </button>
                    </div>

                    <div className="flex items-center justify-between py-2">
                      <div>
                        <p className="font-medium text-white">WebSocket</p>
                        <p className="text-sm text-gray-500">Enable real-time WebSocket connections</p>
                      </div>
                      <button
                        onClick={() => handleChange('websocket_enabled', !editedConfig.websocket_enabled)}
                        role="switch"
                        aria-checked={editedConfig.websocket_enabled}
                        className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                          editedConfig.websocket_enabled ? 'bg-emerald-500' : 'bg-gray-700'
                        }`}
                      >
                        <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                          editedConfig.websocket_enabled ? 'translate-x-6' : 'translate-x-1'
                        }`} />
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              {/* User Info */}
              <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
                <h2 className="text-lg font-semibold text-white mb-4">Current User</h2>
                <div className="space-y-3">
                  <div className="flex items-center justify-between py-2">
                    <span className="text-gray-400">Email</span>
                    <span className="text-white">{user?.email || 'Not logged in'}</span>
                  </div>
                  <div className="flex items-center justify-between py-2">
                    <span className="text-gray-400">Role</span>
                    <span className="text-white capitalize">{user?.role || 'Unknown'}</span>
                  </div>
                  <div className="flex items-center justify-between py-2">
                    <span className="text-gray-400">Workspace</span>
                    <span className="text-white">{user?.workspace || 'Default'}</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'notifications' && (
            <div className="space-y-6" role="tabpanel" id="settings-panel-notifications" aria-labelledby="settings-tab-notifications">
              <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
                <h2 className="text-lg font-semibold text-white mb-1">Notification Settings</h2>
                <p className="text-gray-400 text-sm mb-6">Configure how and when you receive alerts</p>

                <div className="space-y-4">
                  {[
                    { id: 'email', name: 'Email Notifications', desc: 'Receive email alerts for critical events', icon: Mail, color: 'blue' as const },
                    { id: 'slack', name: 'Slack Notifications', desc: 'Send alerts to configured Slack channels', icon: Slack, color: 'purple' as const },
                    { id: 'webhook', name: 'Webhook Notifications', desc: 'POST alerts to custom endpoints', icon: MessageSquare, color: 'emerald' as const },
                    { id: 'digest', name: 'Digest Emails', desc: 'Daily summary of all activities', icon: Clock, color: 'amber' as const },
                  ].map((item) => {
                    const colorMap: Record<string, { bg: string; text: string }> = {
                      blue: { bg: 'bg-blue-500/10', text: 'text-blue-400' },
                      purple: { bg: 'bg-purple-500/10', text: 'text-purple-400' },
                      emerald: { bg: 'bg-emerald-500/10', text: 'text-emerald-400' },
                      amber: { bg: 'bg-amber-500/10', text: 'text-amber-400' },
                    };
                    const colors = colorMap[item.color];
                    return (
                      <div key={item.id} className="flex items-center justify-between py-4 border-b border-gray-700/50 last:border-0">
                        <div className="flex items-center gap-3">
                          <div className={`w-10 h-10 ${colors.bg} rounded-xl flex items-center justify-center`}>
                            <item.icon className={`w-5 h-5 ${colors.text}`} />
                          </div>
                          <div>
                            <p className="font-medium text-white">{item.name}</p>
                            <p className="text-sm text-gray-500">{item.desc}</p>
                          </div>
                        </div>
                        <button className="relative inline-flex h-6 w-11 items-center rounded-full bg-gray-700 transition-colors">
                          <span className="inline-block h-4 w-4 transform rounded-full bg-white translate-x-1 transition-transform" />
                        </button>
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          )}

          {activeTab === 'storage' && (
            <div className="space-y-6" role="tabpanel" id="settings-panel-storage" aria-labelledby="settings-tab-storage">
              <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
                <h2 className="text-lg font-semibold text-white mb-1">Storage Settings</h2>
                <p className="text-gray-400 text-sm mb-6">Manage data retention and storage configuration</p>

                <div className="space-y-6">
                  <div>
                    <label className="block text-sm font-medium text-gray-300 mb-2">
                      Data Retention (days)
                    </label>
                    <input
                      type="number"
                      value={editedConfig.retention_days || 90}
                      onChange={(e) => handleChange('retention_days', parseInt(e.target.value))}
                      className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                    />
                    <p className="text-sm text-gray-500 mt-2">
                      Judgments and logs older than this will be automatically deleted
                    </p>
                  </div>

                  <div>
                    <label className="block text-sm font-medium text-gray-300 mb-2">
                      Storage Path
                    </label>
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={editedConfig.storage_path || '/var/lib/anubis'}
                        onChange={(e) => handleChange('storage_path', e.target.value)}
                        className="flex-1 bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                      />
                      <button className="px-4 py-3 bg-gray-800 text-white rounded-xl hover:bg-gray-700 transition-colors">
                        Browse
                      </button>
                    </div>
                  </div>

                  <div className="pt-6 border-t border-gray-700/50">
                    <h3 className="font-medium text-white mb-4">Storage Usage</h3>
                    <div className="space-y-4">
                      <div>
                        <div className="flex items-center justify-between mb-2">
                          <span className="text-sm text-gray-400">Today's Judgments</span>
                          <span className="text-sm text-white">{statsData?.judgments?.today || 0}</span>
                        </div>
                        <div className="w-full h-2 bg-gray-800 rounded-full overflow-hidden">
                          <div className="w-3/4 h-full bg-amber-500 rounded-full" />
                        </div>
                      </div>

                      <div>
                        <div className="flex items-center justify-between mb-2">
                          <span className="text-sm text-gray-400">Active Souls</span>
                          <span className="text-sm text-white">{statsData?.souls?.total || 0}</span>
                        </div>
                        <div className="w-full h-2 bg-gray-800 rounded-full overflow-hidden">
                          <div className="w-1/4 h-full bg-emerald-500 rounded-full" />
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'integrations' && (
            <div className="space-y-6" role="tabpanel" id="settings-panel-integrations" aria-labelledby="settings-tab-integrations">
              <div className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6">
                <h2 className="text-lg font-semibold text-white mb-1">API & Integrations</h2>
                <p className="text-gray-400 text-sm mb-6">Manage API keys and external integrations</p>

                <div className="space-y-6">
                  <div>
                    <label className="block text-sm font-medium text-gray-300 mb-2">
                      API Key
                    </label>
                    <div className="flex gap-2">
                      <div className="relative flex-1">
                        <input
                          type={showApiKey ? 'text' : 'password'}
                          value={user ? `anb_live_${user.id}` : 'Not available'}
                          readOnly
                          className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50 font-mono"
                        />
                        <button
                          onClick={() => setShowApiKey(!showApiKey)}
                          className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-white"
                          aria-label={showApiKey ? 'Hide API key' : 'Show API key'}
                        >
                          {showApiKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                        </button>
                      </div>
                      <button
                        onClick={handleCopyApiKey}
                        className="px-4 py-3 bg-gray-800 text-white rounded-xl hover:bg-gray-700 transition-colors"
                        aria-label="Copy API key"
                      >
                        <Copy className="w-4 h-4" />
                      </button>
                    </div>
                    <p className="text-sm text-gray-500 mt-2">
                      Use this key to authenticate API requests
                    </p>
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div>
                      <label className="block text-sm font-medium text-gray-300 mb-2">
                        MCP Server Endpoint
                      </label>
                      <input
                        type="text"
                        value={`${window.location.origin}/mcp`}
                        readOnly
                        className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white font-mono text-sm"
                      />
                    </div>

                    <div>
                      <label className="block text-sm font-medium text-gray-300 mb-2">
                        WebSocket Endpoint
                      </label>
                      <input
                        type="text"
                        value={`${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`}
                        readOnly
                        className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white font-mono text-sm"
                      />
                    </div>
                  </div>

                  <div className="pt-6 border-t border-gray-700/50">
                    <h3 className="font-medium text-white mb-4">API Endpoints</h3>
                    <div className="space-y-2">
                      {[
                        { path: '/api/v1/souls', desc: 'Souls management' },
                        { path: '/api/v1/judgments', desc: 'Health check results' },
                        { path: '/api/v1/channels', desc: 'Alert channels' },
                        { path: '/api/v1/rules', desc: 'Alert rules' },
                        { path: '/api/v1/stats/overview', desc: 'Statistics' },
                      ].map((endpoint) => (
                        <div key={endpoint.path} className="flex items-center justify-between py-2 px-4 bg-gray-950 rounded-xl">
                          <code className="text-amber-400 text-sm">{endpoint.path}</code>
                          <span className="text-xs text-gray-500">{endpoint.desc}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
        </>
      )}
    </div>
  )
}
