import { X, CheckCircle2 } from 'lucide-react'
import { SoulProtocolFields } from './SoulProtocolFields'
import { 
  type SoulFormData, 
  type SoulType, 
  nextSoulFormDataForType, 
  soulTargetHints, 
  soulTypeOptions 
} from '../utils/soulForm'

interface SoulCreateModalProps {
  open: boolean
  formData: SoulFormData
  formErrors: Record<string, string>
  loading: boolean
  onClose: () => void
  onSubmit: (e: React.FormEvent) => void
  onFormDataChange: (data: SoulFormData) => void
  onErrorsChange: (errors: Record<string, string>) => void
}

export function SoulCreateModal({
  open,
  formData,
  formErrors,
  loading,
  onClose,
  onSubmit,
  onFormDataChange: setFormData,
  onErrorsChange: setFormErrors,
}: SoulCreateModalProps) {
  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/50 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-labelledby="soul-modal-title"
      onKeyDown={(e) => { if (e.key === 'Escape') onClose() }}
    >
      <div className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl w-full max-w-2xl max-h-[90vh] overflow-auto">
        <div className="p-6 border-b border-gray-700/50 flex items-center justify-between">
          <h2 id="soul-modal-title" className="text-xl font-bold text-white">Add New Soul</h2>
          <button
            onClick={onClose}
            className="p-2 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg transition-colors"
            aria-label="Close dialog"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <form onSubmit={onSubmit} className="p-6 space-y-5">
          {/* Name */}
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Name</label>
            <input
              type="text"
              required
              value={formData.name}
              onChange={(e) => {
                setFormData({ ...formData, name: e.target.value })
                if (formErrors.name) setFormErrors({ ...formErrors, name: '' })
              }}
              placeholder="e.g., Production API"
              className={`w-full bg-gray-950 border rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50 ${
                formErrors.name ? 'border-rose-500' : 'border-gray-700'
              }`}
            />
            {formErrors.name && <p className="mt-1 text-xs text-rose-400">{formErrors.name}</p>}
          </div>

          {/* Type */}
          <div>
            <label htmlFor="soul-type" className="block text-sm font-medium text-gray-300 mb-2">Type</label>
            <select
              id="soul-type"
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

          {/* Target */}
          <div>
            <label htmlFor="soul-target" className="block text-sm font-medium text-gray-300 mb-2">
              {soulTargetHints[formData.type].label}
            </label>
            <input
              id="soul-target"
              data-testid="soul-target"
              type="text"
              required
              value={formData.target}
              onChange={(e) => {
                setFormData({ ...formData, target: e.target.value })
                if (formErrors.target) setFormErrors({ ...formErrors, target: '' })
              }}
              placeholder={soulTargetHints[formData.type].placeholder}
              className={`w-full bg-gray-950 border rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50 ${
                formErrors.target ? 'border-rose-500' : 'border-gray-700'
              }`}
            />
            {formErrors.target ? (
              <p className="mt-2 text-xs text-rose-400">{formErrors.target}</p>
            ) : (
              <p className="mt-2 text-xs text-gray-500">{soulTargetHints[formData.type].help}</p>
            )}
          </div>

          <SoulProtocolFields formData={formData} setFormData={setFormData} />

          {/* Interval & Timeout */}
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-300 mb-2">Interval (seconds)</label>
              <input
                type="number"
                min="10"
                value={formData.weight}
                onChange={(e) => setFormData({ ...formData, weight: parseInt(e.target.value) })}
                className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-300 mb-2">Timeout (seconds)</label>
              <input
                type="number"
                min="1"
                value={formData.timeout}
                onChange={(e) => setFormData({ ...formData, timeout: parseInt(e.target.value) })}
                className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
              />
            </div>
          </div>

          {/* Enabled Toggle */}
          <div className="flex items-center gap-3 p-4 bg-gray-800/50 rounded-xl">
            <input
              type="checkbox"
              id="enabled"
              checked={formData.enabled}
              onChange={(e) => setFormData({ ...formData, enabled: e.target.checked })}
              className="w-5 h-5 rounded border-gray-600 text-amber-500 focus:ring-amber-500/20"
            />
            <label htmlFor="enabled" className="text-sm text-gray-300">
              Enable monitoring immediately
            </label>
          </div>

          {/* Buttons */}
          <div className="flex gap-3 pt-4">
            <button
              type="button"
              onClick={onClose}
              className="flex-1 px-4 py-3 bg-gray-800 hover:bg-gray-700 text-white rounded-xl transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={loading}
              className="flex-1 px-4 py-3 bg-amber-600 hover:bg-amber-500 text-white rounded-xl transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
            >
              {loading ? (
                <>
                  <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                  Creating...
                </>
              ) : (
                <>
                  <CheckCircle2 className="w-4 h-4" />
                  Create Soul
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
