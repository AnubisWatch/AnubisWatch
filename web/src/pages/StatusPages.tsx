import { useState } from 'react'
import {
  Globe,
  Plus,
  Edit,
  Trash2,
  Copy,
  CheckCircle2,
  XCircle,
  AlertCircle,
  Users,
  Layout,
  Link2,
  Search,
  ChevronDown,
  RefreshCw,
  Eye,
  Share2,
  Palette,
  X,
  Check
} from 'lucide-react'
import { useStatusPages, useSouls } from '../api/hooks'
import type { StatusPage } from '../api/client'

interface ServiceStatus {
  id: string
  name: string
  status: 'operational' | 'degraded' | 'down'
  uptime: string
}

export function StatusPages() {
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  // Create form state
  const [formName, setFormName] = useState('')
  const [formSlug, setFormSlug] = useState('')
  const [formDescription, setFormDescription] = useState('')
  const [formTheme, setFormTheme] = useState('dark')
  const [formSelectedSouls, setFormSelectedSouls] = useState<string[]>([])

  const {
    pages,
    loading,
    error,
    refetch,
    createPage,
    deletePage
  } = useStatusPages()

  const { souls } = useSouls()

  const resetForm = () => {
    setFormName('')
    setFormSlug('')
    setFormDescription('')
    setFormTheme('dark')
    setFormSelectedSouls([])
    setSaving(false)
  }

  const handleOpenCreateModal = () => {
    resetForm()
    setShowCreateModal(true)
  }

  const handleCreatePage = async () => {
    if (!formName.trim() || !formSlug.trim()) return
    setSaving(true)
    try {
      await createPage({
        name: formName,
        slug: formSlug,
        description: formDescription,
        theme: formTheme,
        enabled: true,
        souls: formSelectedSouls,
        subscribers: 0,
        uptime_days: 90
      } as Omit<StatusPage, 'id'>)
      setShowCreateModal(false)
      resetForm()
    } catch {
      // Failed to create page
    } finally {
      setSaving(false)
    }
  }

  const toggleSoul = (soulId: string) => {
    setFormSelectedSouls(prev =>
      prev.includes(soulId) ? prev.filter(id => id !== soulId) : [...prev, soulId]
    )
  }

  const handleRefresh = async () => {
    setRefreshing(true)
    await refetch()
    setTimeout(() => setRefreshing(false), 500)
  }

  const handleCopyUrl = (url: string, id: string) => {
    navigator.clipboard.writeText(url)
    setCopiedId(id)
    setTimeout(() => setCopiedId(null), 2000)
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this status page?')) return
    await deletePage(id)
  }

  const filteredPages = pages.filter(page => {
    const matchesSearch = page.name.toLowerCase().includes(search.toLowerCase()) ||
                         page.description?.toLowerCase().includes(search.toLowerCase())
    const matchesFilter = filter === 'all' ||
                         (filter === 'enabled' && page.enabled) ||
                         (filter === 'disabled' && !page.enabled)
    return matchesSearch && matchesFilter
  })

  const stats = {
    total: pages.length,
    active: pages.filter(p => p.enabled).length,
    subscribers: pages.reduce((acc, p) => acc + (p.subscribers || 0), 0),
    domains: pages.filter(p => p.domain).length
  }

  // Map souls to service status
  const soulStatus: ServiceStatus[] = souls.map(soul => ({
    id: soul.id,
    name: soul.name,
    status: soul.enabled ? 'operational' : 'down',
    uptime: '99.9%'
  }))

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'operational': return <CheckCircle2 className="w-5 h-5 text-emerald-400" />
      case 'degraded': return <AlertCircle className="w-5 h-5 text-amber-400" />
      case 'down': return <XCircle className="w-5 h-5 text-rose-400" />
      default: return <AlertCircle className="w-5 h-5 text-gray-400" />
    }
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white tracking-tight">Status Pages</h1>
          <p className="text-gray-400 mt-1 text-sm">Public status pages for your services</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            className={`p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all ${refreshing ? 'animate-spin' : ''}`}
            aria-label="Refresh status pages"
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          <button
            onClick={handleOpenCreateModal}
            className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20"
          >
            <Plus className="w-4 h-4" />
            Create Page
          </button>
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-32" role="status" aria-label="Loading status pages">
          <div className="w-10 h-10 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
        </div>
      ) : error ? (
        <div className="text-center py-16">
          <AlertCircle className="w-12 h-12 text-rose-500 mx-auto mb-4" />
          <p className="text-gray-400">{error}</p>
          <button
            onClick={handleRefresh}
            className="mt-4 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg transition-colors"
          >
            Try Again
          </button>
        </div>
      ) : (
        <>
      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Total Pages</p>
              <p className="text-3xl font-bold text-white mt-1">{stats.total}</p>
            </div>
            <div className="w-10 h-10 bg-gray-800 rounded-xl flex items-center justify-center">
              <Layout className="w-5 h-5 text-amber-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Active</p>
              <p className="text-3xl font-bold text-emerald-400 mt-1">{stats.active}</p>
            </div>
            <div className="w-10 h-10 bg-emerald-500/10 rounded-xl flex items-center justify-center">
              <CheckCircle2 className="w-5 h-5 text-emerald-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Subscribers</p>
              <p className="text-3xl font-bold text-blue-400 mt-1">{stats.subscribers}</p>
            </div>
            <div className="w-10 h-10 bg-blue-500/10 rounded-xl flex items-center justify-center">
              <Users className="w-5 h-5 text-blue-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Custom Domains</p>
              <p className="text-3xl font-bold text-purple-400 mt-1">{stats.domains}</p>
            </div>
            <div className="w-10 h-10 bg-purple-500/10 rounded-xl flex items-center justify-center">
              <Link2 className="w-5 h-5 text-purple-400" />
            </div>
          </div>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-4">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <input
            type="text"
            placeholder="Search status pages..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-gray-900 border border-gray-700/50 rounded-xl pl-11 pr-4 py-3 text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50 transition-colors"
          />
        </div>

        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="bg-gray-900 border border-gray-700/50 rounded-xl pl-10 pr-8 py-3 text-sm text-white focus:outline-none focus:border-amber-500/50 appearance-none cursor-pointer"
          >
            <option value="all">All Pages</option>
            <option value="enabled">Active Only</option>
            <option value="disabled">Disabled Only</option>
          </select>
          <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 pointer-events-none" />
        </div>
      </div>

      {/* Status Pages Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
        {filteredPages.map((page) => (
          <div
            key={page.id}
            className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl overflow-hidden hover:border-gray-600 transition-all group"
          >
            <div className="p-5">
              <div className="flex items-start justify-between mb-4">
                <div className="flex items-center gap-4">
                  <div className={`w-14 h-14 rounded-xl flex items-center justify-center ${
                    page.enabled ? 'bg-amber-500/10' : 'bg-gray-800'
                  }`}>
                    <Globe className={`w-7 h-7 ${page.enabled ? 'text-amber-400' : 'text-gray-500'}`} />
                  </div>
                  <div>
                    <h3 className="font-semibold text-white text-lg">{page.name}</h3>
                    <p className="text-sm text-gray-500 mt-0.5">
                      {page.domain || `${window.location.origin}/status/${page.slug}`}
                    </p>
                    {page.description && (
                      <p className="text-sm text-gray-400 mt-1">{page.description}</p>
                    )}
                  </div>
                </div>
                <span className={`px-2.5 py-1 rounded-lg text-xs font-semibold ${
                  page.enabled ? 'bg-emerald-500/10 text-emerald-400' : 'bg-gray-800 text-gray-500'
                }`}>
                  {page.enabled ? 'Active' : 'Disabled'}
                </span>
              </div>

              {/* Services Preview */}
              <div className="mb-4">
                <p className="text-sm text-gray-400 mb-3 flex items-center gap-2">
                  <Layout className="w-4 h-4" />
                  Services Status
                </p>
                <div className="space-y-2">
                  {(page.souls || []).slice(0, 3).map((soulId: string) => {
                    const soul = soulStatus.find(s => s.id === soulId)
                    return soul ? (
                      <div key={soulId} className="flex items-center justify-between py-2.5 px-3 bg-gray-800/50 rounded-xl">
                        <div className="flex items-center gap-3">
                          {getStatusIcon(soul.status)}
                          <span className="text-sm text-gray-300">{soul.name}</span>
                        </div>
                        <span className="text-xs text-gray-500 font-mono">{soul.uptime}</span>
                      </div>
                    ) : null
                  })}
                  {(page.souls || []).length > 3 && (
                    <p className="text-sm text-gray-500 text-center py-2">
                      +{(page.souls || []).length - 3} more services
                    </p>
                  )}
                  {(page.souls || []).length === 0 && (
                    <p className="text-sm text-gray-500 text-center py-2">
                      No services linked
                    </p>
                  )}
                </div>
              </div>

              {/* Stats Row */}
              <div className="grid grid-cols-3 gap-4 pt-4 border-t border-gray-700/50">
                <div>
                  <p className="text-xs text-gray-500 mb-1">Subscribers</p>
                  <p className="text-lg font-semibold text-blue-400">{page.subscribers || 0}</p>
                </div>
                <div>
                  <p className="text-xs text-gray-500 mb-1">Theme</p>
                  <div className="flex items-center gap-2">
                    <Palette className="w-4 h-4 text-purple-400" />
                    <p className="text-lg font-semibold text-purple-400 capitalize">{page.theme || 'dark'}</p>
                  </div>
                </div>
                <div>
                  <p className="text-xs text-gray-500 mb-1">Services</p>
                  <p className="text-lg font-semibold text-white">{(page.souls || []).length}</p>
                </div>
              </div>
            </div>

            {/* Actions */}
            <div className="px-5 py-3 bg-gray-800/30 flex items-center justify-between">
              <div className="flex items-center gap-2">
                <button
                  onClick={() => handleCopyUrl(page.domain || `${window.location.origin}/status/${page.slug}`, page.id)}
                  className="flex items-center gap-2 px-3 py-1.5 text-sm text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                >
                  {copiedId === page.id ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                  {copiedId === page.id ? 'Copied!' : 'Copy URL'}
                </button>
                <a
                  href={page.domain ? `https://${page.domain}` : `/status/${page.slug}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-2 px-3 py-1.5 text-sm text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                >
                  <Eye className="w-4 h-4" />
                  View
                </a>
              </div>
              <div className="flex items-center gap-1">
                <button className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors" aria-label={`Edit status page ${page.name}`} title="Edit">
                  <Edit className="w-4 h-4" />
                </button>
                <button className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors" aria-label={`Share status page ${page.name}`} title="Share">
                  <Share2 className="w-4 h-4" />
                </button>
                <button
                  onClick={() => handleDelete(page.id)}
                  className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                  aria-label={`Delete status page ${page.name}`}
                  title="Delete"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
          </div>
        ))}

        {/* Create New Card */}
        <button
          onClick={handleOpenCreateModal}
          className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-dashed border-gray-700/50 rounded-2xl p-6 flex flex-col items-center justify-center gap-4 hover:border-amber-500 transition-all text-gray-500 min-h-[400px]"
        >
          <div className="w-16 h-16 rounded-full bg-gray-800 flex items-center justify-center">
            <Plus className="w-8 h-8" />
          </div>
          <div className="text-center">
            <p className="font-medium text-white">Create Status Page</p>
            <p className="text-sm text-gray-500 mt-1">Set up a public status page</p>
          </div>
        </button>
      </div>

      {/* Empty State */}
      {pages.length === 0 && !loading && (
        <div className="text-center py-16">
          <Globe className="w-16 h-16 text-gray-600 mx-auto mb-4" />
          <h3 className="text-xl font-semibold text-white mb-2">No status pages yet</h3>
          <p className="text-gray-400 mb-6">Create your first public status page to communicate service status to your users</p>
          <button
            onClick={handleOpenCreateModal}
            className="px-6 py-3 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-colors"
          >
            Create Your First Page
          </button>
        </div>
      )}
        </>
      )}

      {/* Create Modal */}
      {showCreateModal && (
        <div
          className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50"
          role="dialog"
          aria-modal="true"
          aria-labelledby="statuspage-modal-title"
          onKeyDown={(e) => { if (e.key === 'Escape') { setShowCreateModal(false); resetForm() } }}
        >
          <div className="bg-gray-900 border border-gray-700/50 rounded-2xl w-full max-w-xl max-h-[90vh] flex flex-col">
            <div className="flex items-center justify-between p-6 border-b border-gray-700/50">
              <div>
                <h2 id="statuspage-modal-title" className="text-xl font-semibold text-white">Create Status Page</h2>
                <p className="text-sm text-gray-400 mt-1">Public status page for your services</p>
              </div>
              <button onClick={() => setShowCreateModal(false)} className="p-2 text-gray-400 hover:text-white rounded-lg hover:bg-gray-800 transition-colors" aria-label="Close dialog">
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto p-6 space-y-5">
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Name</label>
                <input
                  type="text"
                  value={formName}
                  onChange={(e) => { setFormName(e.target.value); if (!formSlug) setFormSlug(e.target.value.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '')) }}
                  placeholder="e.g., API Status"
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Slug</label>
                <div className="flex items-center gap-2">
                  <span className="text-sm text-gray-500 font-mono">/status/</span>
                  <input
                    type="text"
                    value={formSlug}
                    onChange={(e) => setFormSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                    placeholder="api-status"
                    className="flex-1 bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white font-mono placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                  />
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Description</label>
                <textarea
                  value={formDescription}
                  onChange={(e) => setFormDescription(e.target.value)}
                  placeholder="Real-time status of all our services..."
                  rows={2}
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-3">Theme</label>
                <div className="grid grid-cols-3 gap-3">
                  {(['dark', 'light', 'auto'] as const).map((theme) => (
                    <button
                      key={theme}
                      onClick={() => setFormTheme(theme)}
                      className={`p-3 rounded-xl text-center text-sm font-medium transition-all ${
                        formTheme === theme
                          ? 'bg-amber-500/10 border-2 border-amber-500 text-amber-400'
                          : 'bg-gray-950 border border-gray-700/50 text-gray-400 hover:border-gray-600'
                      }`}
                    >
                      {theme.charAt(0).toUpperCase() + theme.slice(1)}
                    </button>
                  ))}
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Linked Services</label>
                {souls.length === 0 ? (
                  <p className="text-sm text-gray-500 py-4 text-center border border-dashed border-gray-700/50 rounded-xl">
                    No souls configured. Add services first.
                  </p>
                ) : (
                  <div className="space-y-2 max-h-48 overflow-y-auto">
                    {souls.map((soul) => {
                      const selected = formSelectedSouls.includes(soul.id)
                      return (
                        <button
                          key={soul.id}
                          onClick={() => toggleSoul(soul.id)}
                          className={`w-full flex items-center gap-3 px-4 py-2.5 rounded-xl transition-all text-left ${
                            selected
                              ? 'bg-amber-500/10 border border-amber-500/30'
                              : 'bg-gray-950 border border-gray-700/50 hover:border-gray-600'
                          }`}
                        >
                          <div className={`w-4 h-4 rounded border flex items-center justify-center ${
                            selected ? 'bg-amber-500 border-amber-500' : 'border-gray-600'
                          }`}>
                            {selected && <Check className="w-3 h-3 text-white" />}
                          </div>
                          <span className="text-sm text-white">{soul.name}</span>
                          <span className="text-xs text-gray-500 ml-auto">{soul.type}</span>
                        </button>
                      )
                    })}
                  </div>
                )}
              </div>
            </div>

            <div className="flex items-center justify-end gap-3 p-6 border-t border-gray-700/50">
              <button
                onClick={() => setShowCreateModal(false)}
                className="px-5 py-2.5 text-gray-400 hover:text-white transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleCreatePage}
                disabled={saving || !formName.trim() || !formSlug.trim()}
                className="px-5 py-2.5 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-xl transition-colors font-medium"
              >
                {saving ? 'Creating...' : 'Create Status Page'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
