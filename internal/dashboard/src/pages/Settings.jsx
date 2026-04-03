import { useState } from 'react'

function Settings() {
  const [activeTab, setActiveTab] = useState('general')
  const [saving, setSaving] = useState(false)
  const [showToast, setShowToast] = useState(false)

  const handleSave = () => {
    setSaving(true)
    setTimeout(() => {
      setSaving(false)
      setShowToast(true)
      setTimeout(() => setShowToast(false), 3000)
    }, 1000)
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-display text-2xl font-bold text-amber-400">Settings</h2>
          <p className="text-slate-400">Configure AnubisWatch</p>
        </div>
        <button
          onClick={handleSave}
          disabled={saving}
          className="btn-egyptian disabled:opacity-50"
        >
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
      </div>

      {/* Toast */}
      {showToast && (
        <div className="fixed top-20 right-6 bg-green-500 text-white px-4 py-2 rounded-lg shadow-lg animate-fade-in">
          Settings saved successfully!
        </div>
      )}

      {/* Settings Layout */}
      <div className="flex gap-6">
        {/* Sidebar */}
        <div className="w-64 flex-shrink-0">
          <nav className="space-y-1">
            {[
              { id: 'general', label: 'General', icon: '⚙' },
              { id: 'notifications', label: 'Notifications', icon: '🔔' },
              { id: 'cluster', label: 'Cluster', icon: '𓂀' },
              { id: 'storage', label: 'Storage', icon: '💾' },
              { id: 'security', label: 'Security', icon: '🔒' },
              { id: 'api', label: 'API', icon: '🔌' },
            ].map((item) => (
              <button
                key={item.id}
                onClick={() => setActiveTab(item.id)}
                className={`w-full flex items-center gap-3 px-4 py-3 rounded-lg transition-all text-left ${
                  activeTab === item.id
                    ? 'bg-amber-500/10 text-amber-400'
                    : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/50'
                }`}
              >
                <span>{item.icon}</span>
                <span className="font-medium">{item.label}</span>
              </button>
            ))}
          </nav>
        </div>

        {/* Content */}
        <div className="flex-1 card-egyptian p-6">
          {activeTab === 'general' && (
            <div className="space-y-6">
              <h3 className="font-display text-lg font-semibold text-amber-400">General Settings</h3>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Instance Name
                  </label>
                  <input
                    type="text"
                    defaultValue="AnubisWatch"
                    className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Region
                  </label>
                  <select className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2">
                    <option>us-east-1</option>
                    <option>us-west-2</option>
                    <option>eu-west-1</option>
                    <option>ap-southeast-1</option>
                  </select>
                </div>

                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Timezone
                  </label>
                  <select className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2">
                    <option>UTC</option>
                    <option>America/New_York</option>
                    <option>Europe/London</option>
                    <option>Asia/Tokyo</option>
                  </select>
                </div>

                <div className="flex items-center gap-3">
                  <input type="checkbox" id="telemetry" className="w-4 h-4 rounded border-slate-700" />
                  <label htmlFor="telemetry" className="text-slate-300">
                    Enable anonymous telemetry
                  </label>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'notifications' && (
            <div className="space-y-6">
              <h3 className="font-display text-lg font-semibold text-amber-400">Notification Settings</h3>

              <div className="space-y-4">
                <div className="flex items-center justify-between p-4 bg-slate-800/50 rounded-lg">
                  <div>
                    <h4 className="font-medium">Email Notifications</h4>
                    <p className="text-sm text-slate-400">Receive alerts via email</p>
                  </div>
                  <input type="checkbox" defaultChecked className="w-5 h-5 rounded border-slate-700" />
                </div>

                <div className="flex items-center justify-between p-4 bg-slate-800/50 rounded-lg">
                  <div>
                    <h4 className="font-medium">Digest Mode</h4>
                    <p className="text-sm text-slate-400">Group alerts into periodic digests</p>
                  </div>
                  <input type="checkbox" className="w-5 h-5 rounded border-slate-700" />
                </div>

                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Digest Frequency
                  </label>
                  <select className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2">
                    <option>Every 5 minutes</option>
                    <option>Every 15 minutes</option>
                    <option>Every hour</option>
                    <option>Daily</option>
                  </select>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'cluster' && (
            <div className="space-y-6">
              <h3 className="font-display text-lg font-semibold text-amber-400">Cluster Configuration</h3>

              <div className="space-y-4">
                <div className="flex items-center gap-3">
                  <input type="checkbox" id="cluster" className="w-4 h-4 rounded border-slate-700" />
                  <label htmlFor="cluster" className="text-slate-300">
                    Enable clustering
                  </label>
                </div>

                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Node ID
                  </label>
                  <input
                    type="text"
                    placeholder="auto-generated"
                    className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Cluster Secret
                  </label>
                  <input
                    type="password"
                    placeholder="Enter cluster secret"
                    className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Seed Nodes
                  </label>
                  <textarea
                    placeholder="192.168.1.100:7946&#10;192.168.1.101:7946"
                    rows={3}
                    className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2 font-mono text-sm"
                  />
                  <p className="text-sm text-slate-500 mt-1">One address per line</p>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'storage' && (
            <div className="space-y-6">
              <h3 className="font-display text-lg font-semibold text-amber-400">Storage Settings</h3>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Data Directory
                  </label>
                  <input
                    type="text"
                    defaultValue="/var/lib/anubiswatch"
                    className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2 font-mono"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Retention Period
                  </label>
                  <select className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2">
                    <option>7 days</option>
                    <option>30 days</option>
                    <option>90 days</option>
                    <option>1 year</option>
                    <option>Unlimited</option>
                  </select>
                </div>

                <div className="flex items-center gap-3">
                  <input type="checkbox" id="encryption" className="w-4 h-4 rounded border-slate-700" />
                  <label htmlFor="encryption" className="text-slate-300">
                    Enable at-rest encryption
                  </label>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'security' && (
            <div className="space-y-6">
              <h3 className="font-display text-lg font-semibold text-amber-400">Security Settings</h3>

              <div className="space-y-4">
                <div className="flex items-center justify-between p-4 bg-slate-800/50 rounded-lg">
                  <div>
                    <h4 className="font-medium">Require Authentication</h4>
                    <p className="text-sm text-slate-400">Require login for dashboard access</p>
                  </div>
                  <input type="checkbox" defaultChecked className="w-5 h-5 rounded border-slate-700" />
                </div>

                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Session Timeout
                  </label>
                  <select className="w-full bg-slate-800 border border-slate-700 rounded-lg px-4 py-2">
                    <option>15 minutes</option>
                    <option>30 minutes</option>
                    <option>1 hour</option>
                    <option>24 hours</option>
                  </select>
                </div>

                <div className="flex items-center justify-between p-4 bg-slate-800/50 rounded-lg">
                  <div>
                    <h4 className="font-medium">API Rate Limiting</h4>
                    <p className="text-sm text-slate-400">Limit API requests per minute</p>
                  </div>
                  <input type="checkbox" defaultChecked className="w-5 h-5 rounded border-slate-700" />
                </div>
              </div>
            </div>
          )}

          {activeTab === 'api' && (
            <div className="space-y-6">
              <h3 className="font-display text-lg font-semibold text-amber-400">API Configuration</h3>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    API Key
                  </label>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value="aw_live_••••••••••••••••••••••••"
                      readOnly
                      className="flex-1 bg-slate-800 border border-slate-700 rounded-lg px-4 py-2 font-mono text-sm"
                    />
                    <button className="px-4 py-2 bg-slate-800 border border-slate-700 rounded-lg hover:bg-slate-700 transition-colors">
                      Regenerate
                    </button>
                  </div>
                </div>

                <div className="flex items-center justify-between p-4 bg-slate-800/50 rounded-lg">
                  <div>
                    <h4 className="font-medium">Enable REST API</h4>
                    <p className="text-sm text-slate-400">HTTP REST API access</p>
                  </div>
                  <input type="checkbox" defaultChecked className="w-5 h-5 rounded border-slate-700" />
                </div>

                <div className="flex items-center justify-between p-4 bg-slate-800/50 rounded-lg">
                  <div>
                    <h4 className="font-medium">Enable WebSocket</h4>
                    <p className="text-sm text-slate-400">Real-time updates</p>
                  </div>
                  <input type="checkbox" defaultChecked className="w-5 h-5 rounded border-slate-700" />
                </div>

                <div className="flex items-center justify-between p-4 bg-slate-800/50 rounded-lg">
                  <div>
                    <h4 className="font-medium">Enable gRPC</h4>
                    <p className="text-sm text-slate-400">High-performance RPC</p>
                  </div>
                  <input type="checkbox" defaultChecked className="w-5 h-5 rounded border-slate-700" />
                </div>

                <div className="flex items-center justify-between p-4 bg-slate-800/50 rounded-lg">
                  <div>
                    <h4 className="font-medium">Enable MCP</h4>
                    <p className="text-sm text-slate-400">Model Context Protocol for AI integration</p>
                  </div>
                  <input type="checkbox" defaultChecked className="w-5 h-5 rounded border-slate-700" />
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export default Settings
