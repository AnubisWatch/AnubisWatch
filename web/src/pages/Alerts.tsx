import { useState } from 'react'
import { Bell, Plus, Check, AlertTriangle, AlertCircle, Info, Trash2, Edit, TestTube } from 'lucide-react'

export function Alerts() {
  const [activeTab, setActiveTab] = useState('rules')

  const alertRules = [
    { id: '1', name: 'Service Down', enabled: true, condition: '3 consecutive failures', severity: 'critical', channels: ['ops-slack', 'pagerduty'] },
    { id: '2', name: 'High Latency', enabled: true, condition: 'Latency > 1000ms for 5 min', severity: 'warning', channels: ['ops-slack'] },
    { id: '3', name: 'Certificate Expiring', enabled: true, condition: 'TLS cert expires in 7 days', severity: 'warning', channels: ['ops-email'] },
    { id: '4', name: 'DNS Failure', enabled: false, condition: 'DNS resolution fails', severity: 'critical', channels: ['ops-slack', 'ops-email'] },
  ]

  const alertChannels = [
    { id: '1', name: 'ops-slack', type: 'slack', enabled: true, config: { webhook_url: 'https://hooks.slack.com/...' } },
    { id: '2', name: 'ops-email', type: 'email', enabled: true, config: { smtp_host: 'smtp.example.com', to: ['ops@example.com'] } },
    { id: '3', name: 'pagerduty', type: 'pagerduty', enabled: true, config: { service_key: '***' } },
  ]

  const recentAlerts = [
    { id: '1', rule: 'Service Down', soul: 'CDN Edge', severity: 'critical', status: 'active', triggered_at: new Date(Date.now() - 300000).toISOString() },
    { id: '2', rule: 'High Latency', soul: 'Production API', severity: 'warning', status: 'resolved', triggered_at: new Date(Date.now() - 3600000).toISOString(), resolved_at: new Date(Date.now() - 1800000).toISOString() },
  ]

  const getSeverityIcon = (severity: string) => {
    switch (severity) {
      case 'critical': return <AlertCircle className="w-5 h-5 text-error" />
      case 'warning': return <AlertTriangle className="w-5 h-5 text-warning" />
      case 'info': return <Info className="w-5 h-5 text-info" />
      default: return <Info className="w-5 h-5 text-text-muted" />
    }
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Alerts</h1>
          <p className="text-text-muted mt-1">Configure alert rules and channels</p>
        </div>
        <button className="flex items-center gap-2 px-4 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark transition-colors">
          <Plus className="w-4 h-4" />
          Add Rule
        </button>
      </div>

      {/* Tabs */}
      <div className="flex gap-2 border-b border-bg-hover">
        {['rules', 'channels', 'history'].map((tab) => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            className={`px-6 py-3 font-medium capitalize transition-colors border-b-2 ${
              activeTab === tab
                ? 'text-primary border-primary'
                : 'text-text-secondary border-transparent hover:text-text-primary'
            }`}
          >
            {tab}
          </button>
        ))}
      </div>

      {/* Content */}
      {activeTab === 'rules' && (
        <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
          <table className="w-full">
            <thead className="bg-bg-hover/50">
              <tr>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Name</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Condition</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Severity</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Channels</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Status</th>
                <th className="text-right text-sm font-medium text-text-secondary px-6 py-4">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-bg-hover">
              {alertRules.map((rule) => (
                <tr key={rule.id} className="hover:bg-bg-hover/30">
                  <td className="px-6 py-4">
                    <p className="font-medium text-text-primary">{rule.name}</p>
                  </td>
                  <td className="px-6 py-4">
                    <span className="text-text-secondary text-sm">{rule.condition}</span>
                  </td>
                  <td className="px-6 py-4">
                    <div className="flex items-center gap-2">
                      {getSeverityIcon(rule.severity)}
                      <span className="capitalize text-text-primary">{rule.severity}</span>
                    </div>
                  </td>
                  <td className="px-6 py-4">
                    <div className="flex gap-1 flex-wrap">
                      {rule.channels.map(channel => (
                        <span key={channel} className="px-2 py-0.5 bg-bg-hover text-text-secondary text-xs rounded">
                          {channel}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="px-6 py-4">
                    <span className={`inline-flex items-center gap-1.5 text-sm ${
                      rule.enabled ? 'text-success' : 'text-text-muted'
                    }`}>
                      <span className={`w-1.5 h-1.5 rounded-full ${rule.enabled ? 'bg-success' : 'bg-text-muted'}`} />
                      {rule.enabled ? 'Active' : 'Disabled'}
                    </span>
                  </td>
                  <td className="px-6 py-4">
                    <div className="flex items-center justify-end gap-2">
                      <button className="p-2 text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg">
                        <TestTube className="w-4 h-4" />
                      </button>
                      <button className="p-2 text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg">
                        <Edit className="w-4 h-4" />
                      </button>
                      <button className="p-2 text-text-secondary hover:text-error hover:bg-error/10 rounded-lg">
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {activeTab === 'channels' && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {alertChannels.map((channel) => (
            <div key={channel.id} className="bg-bg-card rounded-lg border border-bg-hover p-6">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 bg-primary/10 rounded-lg flex items-center justify-center">
                    <Bell className="w-5 h-5 text-primary" />
                  </div>
                  <div>
                    <p className="font-medium text-text-primary">{channel.name}</p>
                    <p className="text-sm text-text-muted uppercase">{channel.type}</p>
                  </div>
                </div>
                <span className={`px-2 py-1 rounded text-xs ${
                  channel.enabled ? 'bg-success/10 text-success' : 'bg-text-muted/10 text-text-muted'
                }`}>
                  {channel.enabled ? 'Active' : 'Disabled'}
                </span>
              </div>
              <div className="mt-4 pt-4 border-t border-bg-hover flex justify-end gap-2">
                <button className="p-2 text-text-secondary hover:text-primary rounded-lg">
                  <TestTube className="w-4 h-4" />
                </button>
                <button className="p-2 text-text-secondary hover:text-primary rounded-lg">
                  <Edit className="w-4 h-4" />
                </button>
                <button className="p-2 text-text-secondary hover:text-error rounded-lg">
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
          ))}
          <button className="bg-bg-card rounded-lg border border-dashed border-bg-hover p-6 flex flex-col items-center justify-center gap-3 hover:border-primary hover:text-primary transition-colors text-text-muted">
            <Plus className="w-8 h-8" />
            <span className="font-medium">Add Channel</span>
          </button>
        </div>
      )}

      {activeTab === 'history' && (
        <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
          <table className="w-full">
            <thead className="bg-bg-hover/50">
              <tr>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Alert</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Soul</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Severity</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Status</th>
                <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Triggered</th>
                <th className="text-right text-sm font-medium text-text-secondary px-6 py-4">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-bg-hover">
              {recentAlerts.map((alert) => (
                <tr key={alert.id} className="hover:bg-bg-hover/30">
                  <td className="px-6 py-4">
                    <div className="flex items-center gap-2">
                      {getSeverityIcon(alert.severity)}
                      <span className="font-medium text-text-primary">{alert.rule}</span>
                    </div>
                  </td>
                  <td className="px-6 py-4 text-text-secondary">{alert.soul}</td>
                  <td className="px-6 py-4">
                    <span className={`capitalize ${
                      alert.severity === 'critical' ? 'text-error' :
                      alert.severity === 'warning' ? 'text-warning' : 'text-info'
                    }`}>
                      {alert.severity}
                    </span>
                  </td>
                  <td className="px-6 py-4">
                    <span className={`inline-flex items-center gap-1.5 text-sm ${
                      alert.status === 'active' ? 'text-error' : 'text-success'
                    }`}>
                      <span className={`w-1.5 h-1.5 rounded-full ${alert.status === 'active' ? 'bg-error' : 'bg-success'}`} />
                      <span className="capitalize">{alert.status}</span>
                    </span>
                  </td>
                  <td className="px-6 py-4 text-text-secondary">
                    {new Date(alert.triggered_at).toLocaleString()}
                  </td>
                  <td className="px-6 py-4">
                    <div className="flex items-center justify-end gap-2">
                      {alert.status === 'active' && (
                        <button className="flex items-center gap-2 px-3 py-1.5 bg-success/10 text-success rounded-lg text-sm hover:bg-success/20">
                          <Check className="w-4 h-4" />
                          Acknowledge
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
