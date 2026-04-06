import { useEffect, useState } from 'react'
import { Plus, Search, Filter, Ghost, Play, Pause, Trash2, Edit, ExternalLink } from 'lucide-react'
import { useSoulStore } from '../stores/soulStore'
import { Link } from 'react-router-dom'

export function Souls() {
  const { souls, fetchSouls } = useSoulStore()
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')

  useEffect(() => {
    fetchSouls()
  }, [fetchSouls])

  // Mock data for demo
  const demoSouls = souls.length > 0 ? souls : [
    { id: '1', name: 'Production API', type: 'http', target: 'https://api.example.com/health', enabled: true, tags: ['production', 'api'], weight: 30, timeout: 10, workspace_id: 'default', created_at: new Date().toISOString(), updated_at: new Date().toISOString() },
    { id: '2', name: 'Database Primary', type: 'tcp', target: 'db.example.com:5432', enabled: true, tags: ['database', 'production'], weight: 15, timeout: 5, workspace_id: 'default', created_at: new Date().toISOString(), updated_at: new Date().toISOString() },
    { id: '3', name: 'Redis Cache', type: 'tcp', target: 'redis.example.com:6379', enabled: true, tags: ['cache', 'production'], weight: 30, timeout: 5, workspace_id: 'default', created_at: new Date().toISOString(), updated_at: new Date().toISOString() },
    { id: '4', name: 'CDN Edge', type: 'http', target: 'https://cdn.example.com/status', enabled: true, tags: ['cdn', 'edge'], weight: 60, timeout: 10, workspace_id: 'default', created_at: new Date().toISOString(), updated_at: new Date().toISOString() },
    { id: '5', name: 'SMTP Server', type: 'smtp', target: 'smtp.example.com:587', enabled: false, tags: ['email', 'staging'], weight: 300, timeout: 10, workspace_id: 'default', created_at: new Date().toISOString(), updated_at: new Date().toISOString() },
    { id: '6', name: 'DNS Resolver', type: 'dns', target: '8.8.8.8:53', enabled: true, tags: ['dns', 'external'], weight: 30, timeout: 5, workspace_id: 'default', created_at: new Date().toISOString(), updated_at: new Date().toISOString() },
  ]

  const filteredSouls = demoSouls.filter(soul => {
    const matchesSearch = soul.name.toLowerCase().includes(search.toLowerCase()) ||
                         soul.target.toLowerCase().includes(search.toLowerCase())
    const matchesFilter = filter === 'all' ||
                         (filter === 'enabled' && soul.enabled) ||
                         (filter === 'disabled' && !soul.enabled) ||
                         (filter === 'http' && soul.type === 'http') ||
                         (filter === 'tcp' && soul.type === 'tcp')
    return matchesSearch && matchesFilter
  })

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Souls</h1>
          <p className="text-text-muted mt-1">Manage your monitored targets</p>
        </div>
        <button className="flex items-center gap-2 px-4 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark transition-colors">
          <Plus className="w-4 h-4" />
          Add Soul
        </button>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-4 bg-bg-card p-4 rounded-lg border border-bg-hover">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted" />
          <input
            type="text"
            placeholder="Search souls..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-bg-dark border border-bg-hover rounded-lg pl-10 pr-4 py-2 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-primary"
          />
        </div>
        <div className="flex items-center gap-2">
          <Filter className="w-4 h-4 text-text-muted" />
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="bg-bg-dark border border-bg-hover rounded-lg px-4 py-2 text-sm text-text-primary focus:outline-none focus:border-primary"
          >
            <option value="all">All Types</option>
            <option value="enabled">Enabled</option>
            <option value="disabled">Disabled</option>
            <option value="http">HTTP</option>
            <option value="tcp">TCP</option>
          </select>
        </div>
      </div>

      {/* Souls Table */}
      <div className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
        <table className="w-full">
          <thead className="bg-bg-hover/50">
            <tr>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Soul</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Type</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Target</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Status</th>
              <th className="text-left text-sm font-medium text-text-secondary px-6 py-4">Interval</th>
              <th className="text-right text-sm font-medium text-text-secondary px-6 py-4">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-bg-hover">
            {filteredSouls.map((soul) => (
              <tr key={soul.id} className="hover:bg-bg-hover/30 transition-colors">
                <td className="px-6 py-4">
                  <div className="flex items-center gap-3">
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center ${soul.enabled ? 'bg-success/10' : 'bg-text-muted/10'}`}>
                      <Ghost className={`w-4 h-4 ${soul.enabled ? 'text-success' : 'text-text-muted'}`} />
                    </div>
                    <div>
                      <p className="font-medium text-text-primary">{soul.name}</p>
                      <div className="flex gap-1 mt-1">
                        {soul.tags.slice(0, 2).map(tag => (
                          <span key={tag} className="text-xs bg-bg-hover text-text-secondary px-2 py-0.5 rounded">
                            {tag}
                          </span>
                        ))}
                      </div>
                    </div>
                  </div>
                </td>
                <td className="px-6 py-4">
                  <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-bg-hover text-text-secondary uppercase">
                    {soul.type}
                  </span>
                </td>
                <td className="px-6 py-4">
                  <span className="text-text-secondary text-sm">{soul.target}</span>
                </td>
                <td className="px-6 py-4">
                  <span className={`inline-flex items-center gap-1.5 text-sm ${soul.enabled ? 'text-success' : 'text-text-muted'}`}>
                    <span className={`w-1.5 h-1.5 rounded-full ${soul.enabled ? 'bg-success' : 'bg-text-muted'}`} />
                    {soul.enabled ? 'Active' : 'Disabled'}
                  </span>
                </td>
                <td className="px-6 py-4">
                  <span className="text-text-secondary text-sm">{soul.weight}s</span>
                </td>
                <td className="px-6 py-4">
                  <div className="flex items-center justify-end gap-2">
                    <button className="p-2 text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg transition-colors">
                      <ExternalLink className="w-4 h-4" />
                    </button>
                    <Link to={`/souls/${soul.id}`} className="p-2 text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg transition-colors">
                      <Edit className="w-4 h-4" />
                    </Link>
                    <button className="p-2 text-text-secondary hover:text-warning hover:bg-warning/10 rounded-lg transition-colors">
                      {soul.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
                    </button>
                    <button className="p-2 text-text-secondary hover:text-error hover:bg-error/10 rounded-lg transition-colors">
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
