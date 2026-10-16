import { useState } from 'react'
import { api } from '../api/client'

const inputClass =
  'w-full bg-gray-950 border border-gray-700/50 rounded-xl px-4 py-3 text-white placeholder:text-gray-500 focus:outline-none focus:border-amber-500/50'

/** Changes the signed-in local account's password (PUT /auth/change-password). */
export function ChangePasswordForm() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (next !== confirm) {
      setMessage({ ok: false, text: 'The new passwords do not match.' })
      return
    }
    setBusy(true)
    setMessage(null)
    try {
      await api.put('/auth/change-password', { current_password: current, new_password: next })
      setCurrent('')
      setNext('')
      setConfirm('')
      setMessage({ ok: true, text: 'Password changed.' })
    } catch (err) {
      setMessage({ ok: false, text: err instanceof Error ? err.message : 'Failed to change password' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="bg-gradient-to-br from-gray-900 to-gray-800/50 border border-gray-700/50 rounded-2xl p-6 space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-white mb-1">Change Password</h2>
        <p className="text-gray-400 text-sm">Local accounts only. At least 12 characters, using three of: lowercase, uppercase, digits, symbols.</p>
      </div>
      <div>
        <label htmlFor="current-password" className="block text-sm font-medium text-gray-300 mb-2">Current password</label>
        <input id="current-password" type="password" required autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} className={inputClass} />
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div>
          <label htmlFor="new-password" className="block text-sm font-medium text-gray-300 mb-2">New password</label>
          <input id="new-password" type="password" required minLength={12} autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} className={inputClass} />
        </div>
        <div>
          <label htmlFor="confirm-password" className="block text-sm font-medium text-gray-300 mb-2">Confirm new password</label>
          <input id="confirm-password" type="password" required minLength={12} autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} className={inputClass} />
        </div>
      </div>
      {message && <p className={`text-sm ${message.ok ? 'text-emerald-400' : 'text-rose-400'}`}>{message.text}</p>}
      <button type="submit" disabled={busy} className="px-5 py-2.5 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white rounded-xl transition-colors font-medium">
        {busy ? 'Saving...' : 'Change Password'}
      </button>
    </form>
  )
}
