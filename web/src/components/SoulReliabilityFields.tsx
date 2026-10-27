import { type FlapMode, type SoulFormData, MAX_SOUL_RETRIES } from '../utils/soulForm'

interface SoulReliabilityFieldsProps {
  formData: SoulFormData
  setFormData: (data: SoulFormData) => void
}

const inputClass =
  'w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50 disabled:opacity-50'

const numberOr = (value: string, fallback: number) => {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

/**
 * Confirmation retries and flap detection, behind an "Advanced" disclosure.
 * Heartbeat souls do not dial anything, so retries do not apply to them.
 */
export function SoulReliabilityFields({ formData, setFormData }: SoulReliabilityFieldsProps) {
  const isHeartbeat = formData.type === 'heartbeat'

  return (
    <details className="group rounded-xl border border-gray-700/60 bg-gray-800/30" data-testid="soul-advanced">
      <summary className="cursor-pointer select-none px-4 py-3 text-sm font-medium text-gray-300">
        Advanced
      </summary>
      <div className="space-y-4 px-4 pb-4">
        {!isHeartbeat && (
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label htmlFor="soul-retries" className="block text-sm font-medium text-gray-300 mb-2">Retries</label>
              <input
                id="soul-retries"
                type="number"
                min="0"
                max={MAX_SOUL_RETRIES}
                value={formData.retries}
                onChange={(e) => setFormData({ ...formData, retries: numberOr(e.target.value, 0) })}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-gray-500">Re-check a failure up to this many times before marking the soul dead (0-{MAX_SOUL_RETRIES}).</p>
            </div>
            <div>
              <label htmlFor="soul-retry-interval" className="block text-sm font-medium text-gray-300 mb-2">Retry interval (seconds)</label>
              <input
                id="soul-retry-interval"
                type="number"
                min="1"
                value={formData.retryInterval}
                disabled={formData.retries <= 0}
                onChange={(e) => setFormData({ ...formData, retryInterval: numberOr(e.target.value, 5) })}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-gray-500">Must be shorter than the check interval.</p>
            </div>
          </div>
        )}

        <div>
          <label htmlFor="soul-flap-mode" className="block text-sm font-medium text-gray-300 mb-2">Flap detection</label>
          <select
            id="soul-flap-mode"
            value={formData.flapMode}
            onChange={(e) => setFormData({ ...formData, flapMode: e.target.value as FlapMode })}
            className={inputClass}
          >
            <option value="inherit">Server default</option>
            <option value="enabled">Enabled</option>
            <option value="disabled">Disabled</option>
          </select>
          <p className="mt-1 text-xs text-gray-500">
            A flapping soul keeps being checked, but status alerts pause until it stabilizes.
          </p>
        </div>

        {formData.flapMode === 'enabled' && (
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label htmlFor="soul-flap-threshold" className="block text-sm font-medium text-gray-300 mb-2">Flap threshold (changes)</label>
              <input
                id="soul-flap-threshold"
                type="number"
                min="0"
                max="50"
                value={formData.flapThreshold}
                onChange={(e) => setFormData({ ...formData, flapThreshold: numberOr(e.target.value, 0) })}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-gray-500">0 = default (5).</p>
            </div>
            <div>
              <label htmlFor="soul-flap-window" className="block text-sm font-medium text-gray-300 mb-2">Flap window (minutes)</label>
              <input
                id="soul-flap-window"
                type="number"
                min="0"
                value={formData.flapWindowMinutes}
                onChange={(e) => setFormData({ ...formData, flapWindowMinutes: numberOr(e.target.value, 0) })}
                className={inputClass}
              />
              <p className="mt-1 text-xs text-gray-500">0 = default (10).</p>
            </div>
          </div>
        )}
      </div>
    </details>
  )
}

/** Small badge shown next to a soul's status while it is flapping. */
export function FlappingBadge({ flapping }: { flapping?: boolean }) {
  if (!flapping) return null
  return (
    <span
      data-testid="flapping-badge"
      title="Status is changing repeatedly; status alerts are paused until it stabilizes"
      className="inline-flex items-center rounded-full border border-orange-500/40 bg-orange-500/10 px-2 py-0.5 text-xs font-medium text-orange-300"
    >
      Flapping
    </span>
  )
}
