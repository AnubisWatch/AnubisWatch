import { useParams, useNavigate } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { ArrowLeft, Save, X } from 'lucide-react'
import { useSoul } from '../api/hooks'
import { SoulProtocolFields } from '../components/SoulProtocolFields'
import {
  buildSoulPayload,
  defaultSoulFormData,
  nextSoulFormDataForType,
  soulFormDataFromSoul,
  soulTargetHints,
  soulTypeOptions,
  type SoulFormData,
  type SoulType,
} from '../utils/soulForm'

export function SoulEdit() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { soul, loading, error, updateSoul } = useSoul(id)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const [formData, setFormData] = useState<SoulFormData>(defaultSoulFormData)

  useEffect(() => {
    if (soul) {
      setFormData(soulFormDataFromSoul(soul))
    }
  }, [soul])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!soul) return
    setSaving(true)
    setSaveError(null)
    try {
      await updateSoul(buildSoulPayload(formData, soul.workspace_id || 'default'))
      navigate(`/souls/${id}`)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-32">
        <div className="w-10 h-10 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
      </div>
    )
  }

  if (error || !soul) {
    return (
      <div className="text-center py-16">
        <p className="text-rose-400">{error || 'Soul not found'}</p>
        <button
          onClick={() => navigate('/souls')}
          className="mt-4 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg"
        >
          Back to Souls
        </button>
      </div>
    )
  }

  return (
    <div className="max-w-2xl mx-auto space-y-6">
      <div className="flex items-center gap-4">
        <button
          onClick={() => navigate(`/souls/${id}`)}
          className="p-2.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl"
        >
          <ArrowLeft className="w-5 h-5" />
        </button>
        <div>
          <h1 className="text-2xl font-bold text-white">Edit Soul</h1>
          <p className="text-gray-400 text-sm">{soul.name}</p>
        </div>
      </div>

      <form onSubmit={handleSubmit} className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-6 space-y-5">
        <div>
          <label className="block text-sm font-medium text-gray-300 mb-2">Name</label>
          <input
            type="text"
            required
            value={formData.name}
            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
          />
        </div>

        <div>
          <label htmlFor="edit-soul-type" className="block text-sm font-medium text-gray-300 mb-2">Type</label>
          <select
            id="edit-soul-type"
            aria-label="Soul type"
            value={formData.type}
            onChange={(e) => setFormData(nextSoulFormDataForType(formData, e.target.value as SoulType))}
            className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
          >
            {soulTypeOptions.map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
          </select>
        </div>

        <div>
          <label htmlFor="edit-soul-target" className="block text-sm font-medium text-gray-300 mb-2">
            {soulTargetHints[formData.type].label}
          </label>
          <input
            id="edit-soul-target"
            type="text"
            required
            value={formData.target}
            onChange={(e) => setFormData({ ...formData, target: e.target.value })}
            placeholder={soulTargetHints[formData.type].placeholder}
            className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
          />
          <p className="mt-2 text-xs text-gray-500">{soulTargetHints[formData.type].help}</p>
        </div>

        <SoulProtocolFields formData={formData} setFormData={setFormData} />

        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Interval (seconds)</label>
            <input
              type="number"
              min="10"
              value={formData.weight}
              onChange={(e) => setFormData({ ...formData, weight: parseInt(e.target.value) || 60 })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Timeout (seconds)</label>
            <input
              type="number"
              min="1"
              value={formData.timeout}
              onChange={(e) => setFormData({ ...formData, timeout: parseInt(e.target.value) || 10 })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            />
          </div>
        </div>

        <div className="flex items-center gap-3 p-4 bg-gray-800/50 rounded-xl">
          <input
            type="checkbox"
            id="enabled"
            checked={formData.enabled}
            onChange={(e) => setFormData({ ...formData, enabled: e.target.checked })}
            className="w-5 h-5 rounded border-gray-600 text-amber-500 focus:ring-amber-500/20"
          />
          <label htmlFor="enabled" className="text-sm text-gray-300">
            Enable monitoring
          </label>
        </div>

        {saveError && (
          <div className="p-4 bg-rose-500/10 border border-rose-500/30 rounded-xl text-rose-400 text-sm">
            {saveError}
          </div>
        )}

        <div className="flex gap-3 pt-4">
          <button
            type="button"
            onClick={() => navigate(`/souls/${id}`)}
            className="flex-1 px-4 py-3 bg-gray-800 hover:bg-gray-700 text-white rounded-xl transition-colors flex items-center justify-center gap-2"
          >
            <X className="w-4 h-4" />
            Cancel
          </button>
          <button
            type="submit"
            disabled={saving}
            className="flex-1 px-4 py-3 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
          >
            <Save className="w-4 h-4" />
            {saving ? 'Saving...' : 'Save Changes'}
          </button>
        </div>
      </form>
    </div>
  )
}
