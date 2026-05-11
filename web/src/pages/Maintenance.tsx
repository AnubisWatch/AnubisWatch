import { useState, useCallback, useEffect } from 'react'
import {
  Wrench,
  Plus,
  Edit,
  Trash2,
  Clock,
  Calendar,
  Search,
  Filter,
  ChevronDown,
  RefreshCw,
  Play,
  Pause,
  AlertCircle,
  CheckCircle2,
  X,
  Loader2,
  Check
} from 'lucide-react'
import { api } from '../api/client'
import { useSouls } from '../api/hooks'

interface MaintenanceWindow {
  id: string
  name: string
  description: string
  workspace_id?: string
  soul_ids: string[]
  tags: string[]
  start_time: string
  end_time: string
  recurring: string
  enabled: boolean
  created_at?: string
  updated_at?: string
}

function useMaintenance() {
  const [data, setData] = useState<MaintenanceWindow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.get<MaintenanceWindow[]>('/maintenance')
      setData(result || [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load maintenance windows')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetch() }, [fetch])

  const create = async (w: Omit<MaintenanceWindow, 'id'>) => {
    await api.post<MaintenanceWindow>('/maintenance', w)
    await fetch()
  }

  const update = async (id: string, w: Partial<MaintenanceWindow>) => {
    await api.put<MaintenanceWindow>(`/maintenance/${id}`, w)
    await fetch()
  }

  const remove = async (id: string) => {
    await api.delete(`/maintenance/${id}`)
    await fetch()
  }

  return { data, loading, error, refetch: fetch, create, update, remove }
}

export function Maintenance() {
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [showModal, setShowModal] = useState(false)
  const [editing, setEditing] = useState<MaintenanceWindow | null>(null)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  // Form state
  const [formName, setFormName] = useState('')
  const [formDescription, setFormDescription] = useState('')
  const [formStartTime, setFormStartTime] = useState('')
  const [formEndTime, setFormEndTime] = useState('')
  const [formRecurring, setFormRecurring] = useState('none')
  const [formEnabled, setFormEnabled] = useState(true)
  const [formTags, setFormTags] = useState('')
  const [formAllServices, setFormAllServices] = useState(false)
  const [formSelectedSouls, setFormSelectedSouls] = useState<string[]>([])

  const { data, loading, error, refetch, create, update, remove } = useMaintenance()
  const { souls } = useSouls()

  const resetForm = () => {
    setFormName('')
    setFormDescription('')
    setFormStartTime('')
    setFormEndTime('')
    setFormRecurring('none')
    setFormEnabled(true)
    setFormTags('')
    setFormAllServices(false)
    setFormSelectedSouls([])
    setEditing(null)
    setFormError(null)
    setSaving(false)
  }

  const handleOpenCreate = () => {
    resetForm()
    setShowModal(true)
  }

  const handleOpenEdit = (w: MaintenanceWindow) => {
    setEditing(w)
    setFormName(w.name)
    setFormDescription(w.description)
    setFormStartTime(w.start_time.slice(0, 16))
    setFormEndTime(w.end_time.slice(0, 16))
    setFormRecurring(w.recurring || 'none')
    setFormEnabled(w.enabled)
    setFormTags((w.tags || []).join(', '))
    setFormAllServices((w.soul_ids || []).length === 0 && (w.tags || []).length === 0)
    setFormSelectedSouls(w.soul_ids || [])
    setFormError(null)
    setShowModal(true)
  }

  const handleSave = async () => {
    if (!formName.trim() || !formStartTime || !formEndTime) {
      setFormError('Name, start time, and end time are required')
      return
    }
    if (new Date(formEndTime) <= new Date(formStartTime)) {
      setFormError('End time must be after start time')
      return
    }
    const tags = formTags.split(',').map(t => t.trim()).filter(Boolean)
    if (!formAllServices && formSelectedSouls.length === 0 && tags.length === 0) {
      setFormError('Select at least one service or tag, or choose all services')
      return
    }
    setFormError(null)
    setSaving(true)
    try {
      const payload = {
        name: formName,
        description: formDescription,
        start_time: new Date(formStartTime).toISOString(),
        end_time: new Date(formEndTime).toISOString(),
        recurring: formRecurring === 'none' ? '' : formRecurring,
        enabled: formEnabled,
        tags: formAllServices ? [] : tags,
        soul_ids: formAllServices ? [] : formSelectedSouls
      }

      if (editing) {
        await update(editing.id, payload)
      } else {
        await create(payload)
      }
      setShowModal(false)
      resetForm()
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  const toggleSoul = (soulId: string) => {
    setFormAllServices(false)
    setFormSelectedSouls(prev =>
      prev.includes(soulId) ? prev.filter(id => id !== soulId) : [...prev, soulId]
    )
  }

  const handleToggle = async (w: MaintenanceWindow) => {
    await update(w.id, { enabled: !w.enabled })
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this maintenance window?')) return
    await remove(id)
  }

  const filtered = data.filter(w => {
    const matchesSearch = w.name.toLowerCase().includes(search.toLowerCase()) ||
      w.description?.toLowerCase().includes(search.toLowerCase())
    const isActive = new Date() >= new Date(w.start_time) && new Date() <= new Date(w.end_time) && w.enabled
    if (filter === 'active') return matchesSearch && isActive
    if (filter === 'scheduled') return matchesSearch && !isActive && w.enabled
    if (filter === 'disabled') return matchesSearch && !w.enabled
    return matchesSearch
  })

  const isActive = (w: MaintenanceWindow) =>
    new Date() >= new Date(w.start_time) && new Date() <= new Date(w.end_time) && w.enabled

  const stats = {
    total: data.length,
    active: data.filter(isActive).length,
    scheduled: data.filter(w => w.enabled && !isActive).length,
    disabled: data.filter(w => !w.enabled).length
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-32">
        <div className="w-10 h-10 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
      </div>
    )
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-cinzel font-bold gradient-gold-shine tracking-wider">Sacred Rest</h1>
          <p className="text-gray-400 mt-1 font-cormorant italic">Scheduled temple rest periods — silence the warnings during sacred ceremonies</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={refetch}
            className="p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl transition-all"
            aria-label="Refresh maintenance windows"
          >
            <RefreshCw className="w-5 h-5" />
          </button>
          <button
            onClick={handleOpenCreate}
            className="flex items-center gap-2 px-4 py-2.5 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-all font-medium shadow-lg shadow-amber-600/20"
          >
            <Plus className="w-4 h-4" />
            Add Window
          </button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Total</p>
              <p className="text-2xl font-bold text-white mt-1">{stats.total}</p>
            </div>
            <div className="w-10 h-10 bg-gray-800 rounded-xl flex items-center justify-center">
              <Wrench className="w-5 h-5 text-gray-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Active Now</p>
              <p className="text-2xl font-bold text-emerald-400 mt-1">{stats.active}</p>
            </div>
            <div className="w-10 h-10 bg-emerald-500/10 rounded-xl flex items-center justify-center">
              <Play className="w-5 h-5 text-emerald-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Scheduled</p>
              <p className="text-2xl font-bold text-amber-400 mt-1">{stats.scheduled}</p>
            </div>
            <div className="w-10 h-10 bg-amber-500/10 rounded-xl flex items-center justify-center">
              <Calendar className="w-5 h-5 text-amber-400" />
            </div>
          </div>
        </div>

        <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-5">
          <div className="flex items-start justify-between">
            <div>
              <p className="text-gray-400 text-sm font-medium">Disabled</p>
              <p className="text-2xl font-bold text-gray-400 mt-1">{stats.disabled}</p>
            </div>
            <div className="w-10 h-10 bg-gray-700 rounded-xl flex items-center justify-center">
              <Pause className="w-5 h-5 text-gray-400" />
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
            placeholder="Search maintenance windows..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-gray-900 border border-gray-700/50 rounded-xl pl-11 pr-4 py-3 text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50 transition-colors"
          />
        </div>
        <div className="relative">
          <Filter className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="bg-gray-900 border border-gray-700/50 rounded-xl pl-10 pr-8 py-3 text-sm text-white focus:outline-none focus:border-amber-500/50 appearance-none cursor-pointer"
          >
            <option value="all">All Windows</option>
            <option value="active">Active Now</option>
            <option value="scheduled">Scheduled</option>
            <option value="disabled">Disabled</option>
          </select>
          <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 pointer-events-none" />
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="bg-rose-500/10 border border-rose-500/20 rounded-2xl p-4 flex items-center gap-3">
          <AlertCircle className="w-5 h-5 text-rose-400" />
          <p className="text-rose-400">{error}</p>
        </div>
      )}

      {/* List */}
      {filtered.length > 0 ? (
        <div className="space-y-3">
          {filtered.map((w) => {
            const active = isActive(w)
            return (
              <div
                key={w.id}
                className={`bg-gradient-to-br from-gray-900 to-gray-800/50 border rounded-2xl p-5 transition-all ${
                  active
                    ? 'border-emerald-500/30 shadow-lg shadow-emerald-500/5'
                    : 'border-gray-700/50'
                }`}
              >
                <div className="flex items-start justify-between">
                  <div className="flex items-start gap-4 flex-1">
                    <div className={`w-12 h-12 rounded-xl flex items-center justify-center shrink-0 ${
                      active ? 'bg-emerald-500/10' : 'bg-gray-800'
                    }`}>
                      <Wrench className={`w-6 h-6 ${active ? 'text-emerald-400' : 'text-gray-500'}`} />
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-3">
                        <h3 className="font-semibold text-white">{w.name}</h3>
                        <span className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-lg text-xs font-semibold ${
                          active
                            ? 'bg-emerald-500/10 text-emerald-400'
                            : w.enabled
                              ? 'bg-amber-500/10 text-amber-400'
                              : 'bg-gray-800 text-gray-500'
                        }`}>
                          {active ? <CheckCircle2 className="w-3 h-3" /> : <Clock className="w-3 h-3" />}
                          {active ? 'Active' : w.enabled ? 'Scheduled' : 'Disabled'}
                        </span>
                      </div>
                      {w.description && <p className="text-sm text-gray-400 mt-1">{w.description}</p>}
                      <div className="flex items-center gap-4 mt-3 text-xs text-gray-500">
                        <span className="flex items-center gap-1">
                          <Calendar className="w-3.5 h-3.5" />
                          {new Date(w.start_time).toLocaleString()}
                        </span>
                        <span className="text-gray-600">→</span>
                        <span className="flex items-center gap-1">
                          <Calendar className="w-3.5 h-3.5" />
                          {new Date(w.end_time).toLocaleString()}
                        </span>
                        {w.recurring && (
                          <span className="text-amber-400">Recurring: {w.recurring}</span>
                        )}
                      </div>
                      {w.tags && w.tags.length > 0 && (
                        <div className="flex gap-1.5 mt-2 flex-wrap">
                          {w.tags.map(t => (
                            <span key={t} className="px-2 py-0.5 bg-gray-800 text-gray-400 text-xs rounded-lg">{t}</span>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-2 ml-4">
                    <button
                      onClick={() => handleToggle(w)}
                      role="switch"
                      aria-checked={w.enabled}
                      aria-label={`${w.enabled ? 'Disable' : 'Enable'} maintenance window ${w.name}`}
                      className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                        w.enabled ? 'bg-emerald-500' : 'bg-gray-700'
                      }`}
                    >
                      <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                        w.enabled ? 'translate-x-6' : 'translate-x-1'
                      }`} />
                    </button>
                    <button
                      onClick={() => handleOpenEdit(w)}
                      className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
                      aria-label={`Edit maintenance window ${w.name}`}
                      title="Edit"
                    >
                      <Edit className="w-4 h-4" />
                    </button>
                    <button
                      onClick={() => handleDelete(w.id)}
                      className="p-2 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-colors"
                      aria-label={`Delete maintenance window ${w.name}`}
                      title="Delete"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      ) : !loading ? (
        <div className="text-center py-16">
          <Wrench className="w-16 h-16 text-gray-600 mx-auto mb-4" />
          <h3 className="text-xl font-semibold text-white mb-2">No maintenance windows</h3>
          <p className="text-gray-400 mb-6 max-w-md mx-auto">
            Schedule maintenance windows to suppress alerts during planned downtime or deployments.
          </p>
          <button
            onClick={handleOpenCreate}
            className="px-6 py-3 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-colors"
          >
            Create Maintenance Window
          </button>
        </div>
      ) : null}

      {/* Create/Edit Modal */}
      {showModal && (
        <div
          className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50"
          role="dialog"
          aria-modal="true"
          aria-labelledby="maintenance-modal-title"
          onKeyDown={(e) => { if (e.key === 'Escape') { setShowModal(false); resetForm() } }}
        >
          <div className="bg-gray-900 border border-gray-700/50 rounded-2xl w-full max-w-lg max-h-[90vh] flex flex-col">
            <div className="flex items-center justify-between p-6 border-b border-gray-700/50">
              <div>
                <h2 id="maintenance-modal-title" className="text-xl font-semibold text-white">{editing ? 'Edit' : 'Create'} Maintenance Window</h2>
                <p className="text-sm text-gray-400 mt-1">Suppress alerts during planned downtime</p>
              </div>
              <button onClick={() => { setShowModal(false); resetForm() }} className="p-2 text-gray-400 hover:text-white rounded-lg hover:bg-gray-800 transition-colors" aria-label="Close dialog">
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto p-6 space-y-5">
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Name</label>
                <input
                  type="text"
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="e.g., Database Migration"
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Description</label>
                <textarea
                  value={formDescription}
                  onChange={(e) => setFormDescription(e.target.value)}
                  placeholder="What maintenance is being performed..."
                  rows={2}
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Start Time</label>
                  <input
                    type="datetime-local"
                    value={formStartTime}
                    onChange={(e) => setFormStartTime(e.target.value)}
                    className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">End Time</label>
                  <input
                    type="datetime-local"
                    value={formEndTime}
                    onChange={(e) => setFormEndTime(e.target.value)}
                    className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                  />
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Recurrence</label>
                <select
                  value={formRecurring}
                  onChange={(e) => setFormRecurring(e.target.value)}
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
                >
                  <option value="none">None (one-time)</option>
                  <option value="daily">Daily</option>
                  <option value="weekly">Weekly</option>
                  <option value="monthly">Monthly</option>
                </select>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Tags (comma-separated)</label>
                <input
                  type="text"
                  value={formTags}
                  onChange={(e) => { setFormTags(e.target.value); if (e.target.value.trim()) setFormAllServices(false) }}
                  placeholder="e.g., database, production"
                  className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Services</label>
                <label className="mb-3 flex items-center gap-3 cursor-pointer rounded-xl border border-gray-700/50 bg-gray-950 px-4 py-3">
                  <input
                    type="checkbox"
                    checked={formAllServices}
                    onChange={(e) => {
                      setFormAllServices(e.target.checked)
                      if (e.target.checked) {
                        setFormSelectedSouls([])
                        setFormTags('')
                      }
                    }}
                    className="w-5 h-5 rounded border-gray-600 bg-gray-800 text-amber-500 focus:ring-amber-500"
                  />
                  <span className="text-sm text-gray-300">All services</span>
                </label>
                {souls.length === 0 ? (
                  <p className="text-sm text-gray-500 py-4 text-center border border-dashed border-gray-700/50 rounded-xl">
                    No souls configured.
                  </p>
                ) : (
                  <div className="space-y-2 max-h-44 overflow-y-auto">
                    {souls.map((soul) => {
                      const selected = formSelectedSouls.includes(soul.id)
                      return (
                        <button
                          key={soul.id}
                          type="button"
                          onClick={() => toggleSoul(soul.id)}
                          disabled={formAllServices}
                          className={`w-full flex items-center gap-3 px-4 py-2.5 rounded-xl transition-all text-left ${
                            selected
                              ? 'bg-amber-500/10 border border-amber-500/30'
                              : 'bg-gray-950 border border-gray-700/50 hover:border-gray-600'
                          } disabled:opacity-50 disabled:cursor-not-allowed`}
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

              <label className="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={formEnabled}
                  onChange={(e) => setFormEnabled(e.target.checked)}
                  className="w-5 h-5 rounded border-gray-600 bg-gray-800 text-emerald-500 focus:ring-emerald-500"
                />
                <span className="text-sm text-gray-300">Enabled</span>
              </label>

              {formError && (
                <div className="bg-rose-500/10 border border-rose-500/20 rounded-xl p-3 flex items-center gap-3">
                  <AlertCircle className="w-4 h-4 text-rose-400 shrink-0" />
                  <p className="text-sm text-rose-400">{formError}</p>
                </div>
              )}
            </div>

            <div className="flex items-center justify-end gap-3 p-6 border-t border-gray-700/50">
              <button
                onClick={() => { setShowModal(false); resetForm() }}
                className="px-5 py-2.5 text-gray-400 hover:text-white transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                className="px-5 py-2.5 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-xl transition-colors font-medium"
              >
                {saving ? (
                  <span className="flex items-center gap-2"><Loader2 className="w-4 h-4 animate-spin" /> Saving...</span>
                ) : editing ? 'Save Changes' : 'Create Window'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
