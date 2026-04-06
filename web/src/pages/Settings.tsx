import { useState } from 'react'
import { Settings as SettingsIcon, Shield, Bell, Database, Globe, Key, Save, Check } from 'lucide-react'

export function Settings() {
  const [activeTab, setActiveTab] = useState('general')
  const [saved, setSaved] = useState(false)

  const handleSave = () => {
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  const tabs = [
    { id: 'general', label: 'General', icon: SettingsIcon },
    { id: 'security', label: 'Security', icon: Shield },
    { id: 'notifications', label: 'Notifications', icon: Bell },
    { id: 'storage', label: 'Storage', icon: Database },
    { id: 'integrations', label: 'Integrations', icon: Globe },
  ]

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Settings</h1>
          <p className="text-text-muted mt-1">Configure your AnubisWatch instance</p>
        </div>
        <button
          onClick={handleSave}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg transition-colors ${
            saved
              ? 'bg-success text-white'
              : 'bg-primary text-white hover:bg-primary-dark'
          }`}
        >
          {saved ? <Check className="w-4 h-4" /> : <Save className="w-4 h-4" />}
          {saved ? 'Saved!' : 'Save Changes'}
        </button>
      </div>

      <div className="flex gap-6">
        {/* Sidebar */}
        <div className="w-64 shrink-0">
          <nav className="space-y-1">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`w-full flex items-center gap-3 px-4 py-3 rounded-lg transition-colors text-left ${
                  activeTab === tab.id
                    ? 'bg-primary text-white'
                    : 'text-text-secondary hover:bg-bg-hover hover:text-text-primary'
                }`}
              >
                <tab.icon className="w-5 h-5" />
                <span className="font-medium">{tab.label}</span>
              </button>
            ))}
          </nav>
        </div>

        {/* Content */}
        <div className="flex-1">
          {activeTab === 'general' && (
            <div className="bg-bg-card rounded-lg border border-bg-hover p-6 space-y-6">
              <h2 className="text-lg font-semibold text-text-primary">General Settings</h2>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    Instance Name
                  </label>
                  <input
                    type="text"
                    defaultValue="AnubisWatch Production"
                    className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary focus:outline-none focus:border-primary"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    Timezone
                  </label>
                  <select className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary focus:outline-none focus:border-primary">
                    <option value="UTC">UTC</option>
                    <option value="America/New_York">America/New_York</option>
                    <option value="Europe/London">Europe/London</option>
                    <option value="Asia/Tokyo">Asia/Tokyo</option>
                  </select>
                </div>

                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    Default Dashboard Theme
                  </label>
                  <select className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary focus:outline-none focus:border-primary">
                    <option value="dark">Dark (Tomb Interior)</option>
                    <option value="light">Light (Desert Sun)</option>
                    <option value="system">System Default</option>
                  </select>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'security' && (
            <div className="bg-bg-card rounded-lg border border-bg-hover p-6 space-y-6">
              <h2 className="text-lg font-semibold text-text-primary">Security Settings</h2>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    Session Timeout (minutes)
                  </label>
                  <input
                    type="number"
                    defaultValue="60"
                    className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary focus:outline-none focus:border-primary"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    Maximum Login Attempts
                  </label>
                  <input
                    type="number"
                    defaultValue="5"
                    className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary focus:outline-none focus:border-primary"
                  />
                </div>

                <div className="flex items-center justify-between py-4 border-t border-bg-hover">
                  <div>
                    <p className="font-medium text-text-primary">Require Two-Factor Authentication</p>
                    <p className="text-sm text-text-muted">Enforce 2FA for all admin users</p>
                  </div>
                  <label className="relative inline-flex items-center cursor-pointer">
                    <input type="checkbox" className="sr-only peer" />
                    <div className="w-11 h-6 bg-bg-hover peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary"></div>
                  </label>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'notifications' && (
            <div className="bg-bg-card rounded-lg border border-bg-hover p-6 space-y-6">
              <h2 className="text-lg font-semibold text-text-primary">Notification Settings</h2>

              <div className="space-y-4">
                <div className="flex items-center justify-between py-4 border-b border-bg-hover">
                  <div>
                    <p className="font-medium text-text-primary">Email Notifications</p>
                    <p className="text-sm text-text-muted">Receive email alerts for critical events</p>
                  </div>
                  <label className="relative inline-flex items-center cursor-pointer">
                    <input type="checkbox" defaultChecked className="sr-only peer" />
                    <div className="w-11 h-6 bg-bg-hover peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary"></div>
                  </label>
                </div>

                <div className="flex items-center justify-between py-4 border-b border-bg-hover">
                  <div>
                    <p className="font-medium text-text-primary">Slack Notifications</p>
                    <p className="text-sm text-text-muted">Send alerts to configured Slack channels</p>
                  </div>
                  <label className="relative inline-flex items-center cursor-pointer">
                    <input type="checkbox" defaultChecked className="sr-only peer" />
                    <div className="w-11 h-6 bg-bg-hover peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary"></div>
                  </label>
                </div>

                <div className="flex items-center justify-between py-4">
                  <div>
                    <p className="font-medium text-text-primary">Digest Emails</p>
                    <p className="text-sm text-text-muted">Daily summary of all activities</p>
                  </div>
                  <label className="relative inline-flex items-center cursor-pointer">
                    <input type="checkbox" className="sr-only peer" />
                    <div className="w-11 h-6 bg-bg-hover peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary"></div>
                  </label>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'storage' && (
            <div className="bg-bg-card rounded-lg border border-bg-hover p-6 space-y-6">
              <h2 className="text-lg font-semibold text-text-primary">Storage Settings</h2>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    Data Retention (days)
                  </label>
                  <input
                    type="number"
                    defaultValue="90"
                    className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary focus:outline-none focus:border-primary"
                  />
                  <p className="text-sm text-text-muted mt-1">Judgments and logs older than this will be deleted</p>
                </div>

                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    Storage Path
                  </label>
                  <input
                    type="text"
                    defaultValue="/var/lib/anubis"
                    className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary focus:outline-none focus:border-primary"
                  />
                </div>

                <div className="pt-4 border-t border-bg-hover">
                  <div className="flex items-center justify-between">
                    <div>
                      <p className="font-medium text-text-primary">Storage Usage</p>
                      <p className="text-sm text-text-muted">2.4 GB of 10 GB used</p>
                    </div>
                    <div className="w-32 h-2 bg-bg-hover rounded-full overflow-hidden">
                      <div className="w-1/4 h-full bg-primary rounded-full" />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'integrations' && (
            <div className="bg-bg-card rounded-lg border border-bg-hover p-6 space-y-6">
              <h2 className="text-lg font-semibold text-text-primary">API & Integrations</h2>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    API Key
                  </label>
                  <div className="flex gap-2">
                    <input
                      type="password"
                      defaultValue="anb_live_xxxxxxxxxxxxxxxx"
                      readOnly
                      className="flex-1 bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary focus:outline-none focus:border-primary"
                    />
                    <button className="px-4 py-2 bg-bg-hover text-text-primary rounded-lg hover:bg-bg-hover/80">
                      <Key className="w-4 h-4" />
                    </button>
                  </div>
                </div>

                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    MCP Server Endpoint
                  </label>
                  <input
                    type="text"
                    defaultValue={`${window.location.origin}/mcp`}
                    readOnly
                    className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    WebSocket Endpoint
                  </label>
                  <input
                    type="text"
                    defaultValue={`${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`}
                    readOnly
                    className="w-full bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-text-primary"
                  />
                </div>

                <div className="pt-4 border-t border-bg-hover">
                  <p className="font-medium text-text-primary mb-2">Connected Integrations</p>
                  <div className="space-y-2">
                    {['Slack', 'Discord', 'PagerDuty'].map((integration) => (
                      <div key={integration} className="flex items-center justify-between py-2 px-3 bg-bg-hover/50 rounded-lg">
                        <span className="text-text-secondary">{integration}</span>
                        <span className="text-xs bg-success/10 text-success px-2 py-1 rounded">Connected</span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
