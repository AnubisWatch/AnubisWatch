import { useState } from 'react'
import { Globe, Plus, ExternalLink, Edit, Trash2, Copy, CheckCircle, XCircle, AlertCircle } from 'lucide-react'

export function StatusPages() {
  const [, setShowCreateModal] = useState(false)

  const statusPages = [
    {
      id: '1',
      name: 'Production Status',
      slug: 'production',
      domain: 'status.example.com',
      enabled: true,
      souls: ['1', '2', '3', '4'],
      theme: 'dark',
      subscribers: 142,
    },
    {
      id: '2',
      name: 'API Status',
      slug: 'api',
      domain: null,
      enabled: true,
      souls: ['1', '4'],
      theme: 'light',
      subscribers: 89,
    },
    {
      id: '3',
      name: 'Infrastructure',
      slug: 'infra',
      domain: 'infra-status.example.com',
      enabled: false,
      souls: ['2', '3', '5'],
      theme: 'dark',
      subscribers: 23,
    },
  ]

  const soulStatus = [
    { id: '1', name: 'Production API', status: 'operational' },
    { id: '2', name: 'Database Primary', status: 'operational' },
    { id: '3', name: 'Redis Cache', status: 'operational' },
    { id: '4', name: 'CDN Edge', status: 'degraded' },
    { id: '5', name: 'SMTP Server', status: 'operational' },
  ]

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'operational': return <CheckCircle className="w-5 h-5 text-success" />
      case 'degraded': return <AlertCircle className="w-5 h-5 text-warning" />
      case 'down': return <XCircle className="w-5 h-5 text-error" />
      default: return <AlertCircle className="w-5 h-5 text-text-muted" />
    }
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Status Pages</h1>
          <p className="text-text-muted mt-1">Public status pages for your services</p>
        </div>
        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-primary text-white rounded-lg hover:bg-primary-dark transition-colors"
        >
          <Plus className="w-4 h-4" />
          Create Page
        </button>
      </div>

      {/* Status Pages Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {statusPages.map((page) => (
          <div key={page.id} className="bg-bg-card rounded-lg border border-bg-hover overflow-hidden">
            <div className="p-6">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-3">
                  <div className={`w-12 h-12 rounded-lg flex items-center justify-center ${
                    page.enabled ? 'bg-primary/10' : 'bg-text-muted/10'
                  }`}>
                    <Globe className={`w-6 h-6 ${page.enabled ? 'text-primary' : 'text-text-muted'}`} />
                  </div>
                  <div>
                    <h3 className="font-semibold text-text-primary">{page.name}</h3>
                    <p className="text-sm text-text-muted">
                      {page.domain || `${window.location.origin}/status/${page.slug}`}
                    </p>
                  </div>
                </div>
                <span className={`px-2 py-1 rounded text-xs ${
                  page.enabled ? 'bg-success/10 text-success' : 'bg-text-muted/10 text-text-muted'
                }`}>
                  {page.enabled ? 'Active' : 'Disabled'}
                </span>
              </div>

              {/* Status Overview */}
              <div className="mt-6">
                <div className="flex items-center gap-2 mb-3">
                  <span className="text-sm text-text-muted">Services Status:</span>
                </div>
                <div className="space-y-2">
                  {page.souls.slice(0, 3).map((soulId) => {
                    const soul = soulStatus.find(s => s.id === soulId)
                    return soul ? (
                      <div key={soulId} className="flex items-center justify-between py-2 px-3 bg-bg-hover/50 rounded-lg">
                        <span className="text-sm text-text-secondary">{soul.name}</span>
                        {getStatusIcon(soul.status)}
                      </div>
                    ) : null
                  })}
                  {page.souls.length > 3 && (
                    <p className="text-sm text-text-muted text-center py-2">
                      +{page.souls.length - 3} more services
                    </p>
                  )}
                </div>
              </div>

              <div className="mt-6 pt-4 border-t border-bg-hover grid grid-cols-2 gap-4">
                <div>
                  <p className="text-sm text-text-muted">Subscribers</p>
                  <p className="text-lg font-semibold text-text-primary">{page.subscribers}</p>
                </div>
                <div>
                  <p className="text-sm text-text-muted">Theme</p>
                  <p className="text-lg font-semibold text-text-primary capitalize">{page.theme}</p>
                </div>
              </div>
            </div>

            <div className="px-6 py-4 bg-bg-hover/30 flex justify-between items-center">
              <div className="flex items-center gap-2">
                <button
                  onClick={() => navigator.clipboard.writeText(page.domain || `${window.location.origin}/status/${page.slug}`)}
                  className="flex items-center gap-2 px-3 py-1.5 text-sm text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg transition-colors"
                >
                  <Copy className="w-4 h-4" />
                  Copy URL
                </button>
                <a
                  href={page.domain ? `https://${page.domain}` : `/status/${page.slug}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-2 px-3 py-1.5 text-sm text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg transition-colors"
                >
                  <ExternalLink className="w-4 h-4" />
                  View
                </a>
              </div>
              <div className="flex items-center gap-2">
                <button className="p-2 text-text-secondary hover:text-primary hover:bg-primary/10 rounded-lg transition-colors">
                  <Edit className="w-4 h-4" />
                </button>
                <button className="p-2 text-text-secondary hover:text-error hover:bg-error/10 rounded-lg transition-colors">
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
          </div>
        ))}

        {/* Create New Card */}
        <button
          onClick={() => setShowCreateModal(true)}
          className="bg-bg-card rounded-lg border border-dashed border-bg-hover p-6 flex flex-col items-center justify-center gap-4 hover:border-primary hover:text-primary transition-colors text-text-muted min-h-[400px]"
        >
          <div className="w-16 h-16 rounded-full bg-bg-hover flex items-center justify-center">
            <Plus className="w-8 h-8" />
          </div>
          <div className="text-center">
            <p className="font-medium text-text-primary">Create Status Page</p>
            <p className="text-sm text-text-muted mt-1">Set up a public status page</p>
          </div>
        </button>
      </div>
    </div>
  )
}
