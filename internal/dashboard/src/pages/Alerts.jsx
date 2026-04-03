import { useState, useEffect } from 'react'

function Alerts() {
  const [channels, setChannels] = useState([])
  const [rules, setRules] = useState([])
  const [incidents, setIncidents] = useState([])
  const [activeTab, setActiveTab] = useState('channels')

  useEffect(() => {
    fetchChannels()
    fetchRules()
    fetchIncidents()
  }, [])

  const fetchChannels = async () => {
    try {
      const res = await fetch('/api/v1/channels')
      if (res.ok) {
        const data = await res.json()
        setChannels(data)
      }
    } catch (err) {
      console.error('Failed to fetch channels:', err)
    }
  }

  const fetchRules = async () => {
    try {
      const res = await fetch('/api/v1/rules')
      if (res.ok) {
        const data = await res.json()
        setRules(data)
      }
    } catch (err) {
      console.error('Failed to fetch rules:', err)
    }
  }

  const fetchIncidents = async () => {
    try {
      const res = await fetch('/api/v1/incidents')
      if (res.ok) {
        const data = await res.json()
        setIncidents(data)
      }
    } catch (err) {
      console.error('Failed to fetch incidents:', err)
    }
  }

  const getChannelIcon = (type) => {
    const icons = {
      slack: '💬',
      discord: '🎮',
      email: '✉',
      pagerduty: '🚨',
      opsgenie: '📟',
      ntfy: '📱',
      webhook: '🔗',
      sms: '📞',
    }
    return icons[type] || '📢'
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-display text-2xl font-bold text-amber-400">Alerts</h2>
          <p className="text-slate-400">Manage notifications and incidents</p>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex items-center gap-2 border-b border-slate-800">
        {['channels', 'rules', 'incidents'].map((tab) => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            className={`px-4 py-2 text-sm font-medium capitalize transition-colors ${
              activeTab === tab
                ? 'text-amber-400 border-b-2 border-amber-400'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            {tab}
          </button>
        ))}
      </div>

      {/* Channels Tab */}
      {activeTab === 'channels' && (
        <div className="card-egyptian p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-display text-lg font-semibold text-amber-400">
              Alert Channels
            </h3>
            <button className="btn-egyptian">+ Add Channel</button>
          </div>
          {channels.length === 0 ? (
            <div className="text-center py-12 text-slate-500">
              No channels configured. Add one to receive alerts.
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {channels.map((ch) => (
                <div key={ch.id} className="bg-slate-800/50 rounded-lg p-4 border border-slate-700">
                  <div className="flex items-start justify-between">
                    <div className="flex items-center gap-3">
                      <span className="text-2xl">{getChannelIcon(ch.type)}</span>
                      <div>
                        <h4 className="font-medium">{ch.name}</h4>
                        <span className="text-xs text-slate-400 uppercase">{ch.type}</span>
                      </div>
                    </div>
                    <span className={`w-2 h-2 rounded-full ${ch.enabled ? 'bg-green-500' : 'bg-slate-500'}`}></span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Rules Tab */}
      {activeTab === 'rules' && (
        <div className="card-egyptian p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-display text-lg font-semibold text-amber-400">
              Alert Rules
            </h3>
            <button className="btn-egyptian">+ Add Rule</button>
          </div>
          {rules.length === 0 ? (
            <div className="text-center py-12 text-slate-500">
              No rules configured. Create rules to trigger alerts.
            </div>
          ) : (
            <div className="space-y-4">
              {rules.map((rule) => (
                <div key={rule.id} className="bg-slate-800/50 rounded-lg p-4 border border-slate-700">
                  <div className="flex items-center justify-between">
                    <div>
                      <h4 className="font-medium">{rule.name}</h4>
                      <p className="text-sm text-slate-400">
                        Scope: {rule.scope?.type || 'all'} • Channels: {rule.channels?.length || 0}
                      </p>
                    </div>
                    <span className={`px-2 py-1 rounded text-xs ${rule.enabled ? 'bg-green-400/10 text-green-400' : 'bg-slate-400/10 text-slate-400'}`}>
                      {rule.enabled ? 'Active' : 'Disabled'}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Incidents Tab */}
      {activeTab === 'incidents' && (
        <div className="card-egyptian p-6">
          <h3 className="font-display text-lg font-semibold text-amber-400 mb-4">
            Active Incidents
          </h3>
          {incidents.length === 0 ? (
            <div className="text-center py-12 text-slate-500">
              No active incidents. All systems operational.
            </div>
          ) : (
            <div className="space-y-4">
              {incidents.map((inc) => (
                <div key={inc.id} className="bg-red-500/5 rounded-lg p-4 border border-red-500/20">
                  <div className="flex items-center justify-between">
                    <div>
                      <h4 className="font-medium">{inc.soul_name || inc.soul_id}</h4>
                      <p className="text-sm text-slate-400">
                        Started: {new Date(inc.started_at).toLocaleString()}
                      </p>
                    </div>
                    <div className="flex items-center gap-2">
                      <button className="px-3 py-1 bg-amber-500/20 text-amber-400 rounded text-sm">
                        Acknowledge
                      </button>
                      <button className="px-3 py-1 bg-green-500/20 text-green-400 rounded text-sm">
                        Resolve
                      </button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default Alerts
