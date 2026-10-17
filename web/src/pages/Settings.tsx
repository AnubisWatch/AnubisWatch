import { useState, useEffect, useCallback, useMemo } from 'react'
import {
  Settings as SettingsIcon,
  Shield,
  Bell,
  Database,
  Globe,
  Save,
  Check,
  Mail,
  MessageSquare,
  Moon,
  Sun,
  Monitor,
  Clock,
  ChevronRight,
  RefreshCw,
  Loader2,
  AlertCircle,
  X
} from 'lucide-react'
import { api } from '../api/client'
import { useAuth, useStats } from '../api/hooks'
import { ChangePasswordForm } from '../components/ChangePasswordForm'
import { useCan } from '../utils/permissions'
import { listTimeZones, setDisplayTimeZone } from '../utils/date'
import { useThemeStore, applyTheme } from '../stores/themeStore'
import { Link } from 'react-router-dom'
import { useSafeTimeout } from '../hooks/useSafeTimeout'

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
  read_only_keys?: string[]
}

// The only settings PUT /config accepts; everything else comes from the
// config file and is shown read-only.
const editableKeys = ['instance_name', 'timezone', 'language', 'theme'] as const

// Efficient shallow comparison for config objects (all values are primitives)
function hasConfigChanged(original: ConfigData, edited: ConfigData): boolean {
  const allKeys = Object.keys(original || {}) as Array<keyof ConfigData>
  for (const key of allKeys) {
    if (original[key] !== edited[key]) return true
  }
  // Check for new keys in edited that don't exist in original
  for (const key of Object.keys(edited || {}) as Array<keyof ConfigData>) {
    if (!(key in (original || {})) && edited[key] !== undefined) return true
  }
  return false
}

const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'local'

export function Settings() {
  const later = useSafeTimeout()
  const [activeTab, setActiveTab] = useState<TabId>('general')
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [config, setConfig] = useState<ConfigData>({})
  const [editedConfig, setEditedConfig] = useState<ConfigData>({})
  const [hasChanges, setHasChanges] = useState(false)
  // Every IANA zone the browser knows, plus a saved value it does not.
  const timeZoneOptions = useMemo(() => {
    const zones = listTimeZones()
    const saved = config.timezone
    return saved && !zones.includes(saved) ? [saved, ...zones] : zones
  }, [config.timezone])

  const { user } = useAuth()
  const can = useCan()
  const { data: statsData } = useStats()
  const { theme, setTheme } = useThemeStore()

  // Fetch configuration
  const fetchConfig = useCallback(async () => {
    try {
      setLoading(true)
      const result = await api.get<ConfigData>('/config')
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

  // Track changes using efficient comparison instead of JSON.stringify
  useEffect(() => {
    setHasChanges(hasConfigChanged(config, editedConfig))
  }, [config, editedConfig])

  const handleSave = async () => {
    setSaving(true)
    setSaved(false)
    setError(null)
    try {
      const changes: Record<string, unknown> = {}
      for (const key of editableKeys) {
        if (editedConfig[key] !== config[key]) changes[key] = editedConfig[key] ?? null
      }
      const result = await api.put<ConfigData>('/config', changes)
      const next = result ?? editedConfig
      setConfig(next)
      setEditedConfig(next)
      setDisplayTimeZone(next.timezone)
      setSaved(true)
      later(() => setSaved(false), 2000)
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
          <h1 className="text-3xl font-cinzel font-bold gradient-gold-shine tracking-wider">Pharaoh's Chamber</h1>
          <p className="text-gray-400 mt-1 font-cormorant italic">Configure the sacred workings of the realm</p>
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
            hidden={!can('settings:write')}
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
                        aria-label="Timezone"
                        value={editedConfig.timezone || ''}
                        onChange={(e) => handleChange('timezone', e.target.value)}
                        className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                      >
                        <option value="">Viewer&apos;s local time ({browserTimeZone})</option>
                        {timeZoneOptions.map((zone) => (
                          <option key={zone} value={zone}>{zone}</option>
                        ))}
                      </select>
                      <p className="text-sm text-gray-500 mt-2">Dates across the dashboard are shown in this zone.</p>
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
                    {([
                      ['Authentication', 'Require login to access dashboard', editedConfig.auth_enabled],
                      ['MCP Server', 'Model Context Protocol endpoint', editedConfig.mcp_enabled],
                      ['WebSocket', 'Real-time WebSocket connections', editedConfig.websocket_enabled],
                    ] as const).map(([label, description, enabled]) => (
                      <div key={label} className="flex items-center justify-between py-2">
                        <div>
                          <p className="font-medium text-white">{label}</p>
                          <p className="text-sm text-gray-500">{description}</p>
                        </div>
                        <span className={`px-2.5 py-1 rounded-lg text-xs font-semibold ${enabled ? 'bg-emerald-500/10 text-emerald-400' : 'bg-gray-700/50 text-gray-400'}`}>
                          {enabled ? 'Enabled' : 'Disabled'}
                        </span>
                      </div>
                    ))}
                    <p className="text-sm text-gray-500">
                      These are set in the server config file (auth, mcp and server sections) and take effect on restart.
                    </p>
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

              <ChangePasswordForm />
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
                    { id: 'slack', name: 'Slack Notifications', desc: 'Send alerts to configured Slack channels', icon: MessageSquare, color: 'purple' as const },
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
                        <Link
                          to="/alerts"
                          className="px-3 py-1.5 bg-gray-800 text-gray-300 hover:text-white hover:bg-gray-700 rounded-lg text-sm transition-colors"
                        >
                          Configure
                        </Link>
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
                      type="text"
                      readOnly
                      value={editedConfig.retention_days ? String(editedConfig.retention_days) : 'Never pruned'}
                      className="w-full bg-gray-950/50 border border-gray-700/50 rounded-xl px-4 py-3 text-gray-300 cursor-default focus:outline-none"
                    />
                    <p className="text-sm text-gray-500 mt-2">
                      Raw judgments older than this are deleted. Set storage.retention_days in the config file.
                    </p>
                  </div>

                  <div>
                    <label className="block text-sm font-medium text-gray-300 mb-2">
                      Storage Path
                    </label>
                    <input
                      type="text"
                      readOnly
                      value={editedConfig.storage_path || ''}
                      className="w-full bg-gray-950/50 border border-gray-700/50 rounded-xl px-4 py-3 text-gray-300 font-mono cursor-default focus:outline-none"
                    />
                    <p className="text-sm text-gray-500 mt-2">Set storage.path in the config file.</p>
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
                <p className="text-gray-400 text-sm mb-6">Review API authentication and external integration endpoints</p>

                <div className="space-y-6">
                  <div className="rounded-xl border border-amber-500/20 bg-amber-500/5 p-4">
                    <p className="font-medium text-white">API authentication</p>
                    <p className="mt-1 text-sm text-gray-400">
                      This installation authenticates browser and API requests with the active session cookie or bearer token. It does not issue standalone API keys.
                    </p>
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div>
                      <label className="block text-sm font-medium text-gray-300 mb-2">
                        MCP Server Endpoint
                      </label>
                      <input
                        type="text"
                        value={`${window.location.origin}/api/v1/mcp`}
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
