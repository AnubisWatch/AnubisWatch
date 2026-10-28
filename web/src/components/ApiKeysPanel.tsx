import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Check, Copy, KeyRound, Loader2, Plus, Trash2 } from 'lucide-react'
import { api, type ApiKey, type CreatedApiKey } from '../api/client'
import { getLastKnownUser } from '../api/authEvents'
import { formatDate, formatDistanceToNow } from '../utils/date'
import { useCan } from '../utils/permissions'
import { ConfirmDialog } from './ConfirmDialog'

// Mirrors core.RoleWithin: a key may not rank above its creator.
const roleRank: Record<string, number> = { viewer: 1, api: 2, editor: 3, admin: 4, owner: 5 }
const allRoles = ['viewer', 'api', 'editor', 'admin', 'owner']

const expiryOptions = [
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: '180', label: '180 days' },
  { value: '365', label: '1 year' },
  { value: '0', label: 'Never' },
]

const statusStyle: Record<ApiKey['status'], string> = {
  active: 'bg-emerald-500/10 text-emerald-400',
  expired: 'bg-amber-500/10 text-amber-400',
  revoked: 'bg-gray-700/50 text-gray-400',
}

/**
 * Settings → API & Integrations: lists the workspace's API keys, issues new
 * ones (the plaintext is revealed once) and revokes them.
 */
export function ApiKeysPanel() {
  const can = useCan()
  const allowed = can('apikeys:write')
  const myRole = getLastKnownUser()?.role ?? 'viewer'
  const roles = allRoles.filter((r) => (roleRank[r] ?? 0) <= (roleRank[myRole] ?? 0))

  const [keys, setKeys] = useState<ApiKey[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [role, setRole] = useState('viewer')
  const [expiry, setExpiry] = useState('90')
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<CreatedApiKey | null>(null)
  const [copied, setCopied] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<ApiKey | null>(null)

  const load = useCallback(async () => {
    try {
      const result = await api.get<ApiKey[]>('/api-keys')
      setKeys(Array.isArray(result) ? result : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load API keys')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (allowed) void load()
    else setLoading(false)
  }, [allowed, load])

  const create = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim()) {
      setError('Name is required')
      return
    }
    setCreating(true)
    setError(null)
    try {
      const result = await api.post<CreatedApiKey>('/api-keys', {
        name: name.trim(),
        role,
        expires_in_days: Number(expiry),
      })
      setCreated(result)
      setCopied(false)
      setName('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create API key')
    } finally {
      setCreating(false)
    }
  }

  const copy = async () => {
    if (!created) return
    try {
      await navigator.clipboard.writeText(created.key)
      setCopied(true)
    } catch {
      setError('Copy failed; select the key and copy it manually')
    }
  }

  const revoke = async () => {
    const target = revokeTarget
    setRevokeTarget(null)
    if (!target) return
    try {
      await api.delete(`/api-keys/${encodeURIComponent(target.id)}`)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke API key')
    }
  }

  return (
    <div className="space-y-4" aria-labelledby="api-keys-heading">
      <div className="flex items-center gap-2">
        <KeyRound className="w-5 h-5 text-amber-400" aria-hidden="true" />
        <h3 id="api-keys-heading" className="font-medium text-white">API Keys</h3>
      </div>
      <p className="text-sm text-gray-400">
        Long-lived credentials for scripts, CI and integrations. Send one as{' '}
        <code className="text-amber-400">Authorization: Bearer anb_…</code> to the REST, gRPC, WebSocket or MCP
        endpoints, or set <code className="text-amber-400">ANUBIS_API_TOKEN</code> for CLI client commands. A key
        acts with its role in this workspace and cannot manage keys, change passwords or switch workspaces.
      </p>

      {!allowed ? (
        <p className="text-sm text-gray-500">Only workspace admins and owners can manage API keys.</p>
      ) : (
        <>
          {error && (
            <p role="alert" className="text-sm text-rose-400">
              {error}
            </p>
          )}

          {created && (
            <div className="rounded-xl border border-emerald-500/30 bg-emerald-500/5 p-4 space-y-2" role="status">
              <p className="text-sm font-medium text-white">
                Key “{created.name}” created. Copy it now — it will not be shown again.
              </p>
              <div className="flex gap-2">
                <input
                  aria-label="New API key"
                  readOnly
                  value={created.key}
                  onFocus={(e) => e.currentTarget.select()}
                  className="flex-1 bg-gray-950 border border-gray-700/50 rounded-xl px-3 py-2 text-white font-mono text-xs"
                />
                <button
                  type="button"
                  onClick={copy}
                  className="flex items-center gap-1 px-3 py-2 rounded-xl bg-gray-800 text-gray-200 hover:bg-gray-700 text-sm"
                >
                  {copied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                  {copied ? 'Copied' : 'Copy'}
                </button>
                <button
                  type="button"
                  onClick={() => setCreated(null)}
                  className="px-3 py-2 rounded-xl text-gray-400 hover:text-white text-sm"
                >
                  Done
                </button>
              </div>
            </div>
          )}

          <form onSubmit={create} className="grid grid-cols-1 md:grid-cols-4 gap-3 items-end">
            <div className="md:col-span-2">
              <label htmlFor="api-key-name" className="block text-sm font-medium text-gray-300 mb-1">
                Name
              </label>
              <input
                id="api-key-name"
                value={name}
                maxLength={100}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. deploy pipeline"
                className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-3 py-2 text-white text-sm"
              />
            </div>
            <div>
              <label htmlFor="api-key-role" className="block text-sm font-medium text-gray-300 mb-1">
                Role
              </label>
              <select
                id="api-key-role"
                value={role}
                onChange={(e) => setRole(e.target.value)}
                className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-3 py-2 text-white text-sm"
              >
                {roles.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label htmlFor="api-key-expiry" className="block text-sm font-medium text-gray-300 mb-1">
                Expires
              </label>
              <select
                id="api-key-expiry"
                value={expiry}
                onChange={(e) => setExpiry(e.target.value)}
                className="w-full bg-gray-950 border border-gray-700/50 rounded-xl px-3 py-2 text-white text-sm"
              >
                {expiryOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="md:col-span-4">
              <button
                type="submit"
                disabled={creating}
                className="flex items-center gap-2 px-4 py-2 rounded-xl bg-amber-500 text-gray-950 font-medium text-sm hover:bg-amber-400 disabled:opacity-50"
              >
                {creating ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}
                Create API key
              </button>
            </div>
          </form>

          {loading ? (
            <p className="text-sm text-gray-500">Loading API keys…</p>
          ) : keys.length === 0 ? (
            <p className="text-sm text-gray-500">No API keys yet.</p>
          ) : (
            <ul className="space-y-2" aria-label="API keys">
              {keys.map((k) => (
                <li
                  key={k.id}
                  className="flex flex-wrap items-center justify-between gap-2 py-2 px-4 bg-gray-950 rounded-xl"
                >
                  <div className="min-w-0">
                    <p className="text-sm text-white">
                      {k.name}{' '}
                      <span className={`ml-1 px-2 py-0.5 rounded-full text-xs ${statusStyle[k.status] ?? statusStyle.revoked}`}>
                        {k.status}
                      </span>
                    </p>
                    <p className="text-xs text-gray-500 font-mono truncate">
                      {k.prefix}_… · {k.role} · {k.expires_at ? `expires ${formatDate(k.expires_at)}` : 'no expiry'} ·{' '}
                      {k.last_used_at ? `used ${formatDistanceToNow(k.last_used_at)}` : 'never used'}
                    </p>
                  </div>
                  {k.status !== 'revoked' && (
                    <button
                      type="button"
                      onClick={() => setRevokeTarget(k)}
                      aria-label={`Revoke ${k.name}`}
                      className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-rose-400 hover:bg-rose-500/10 text-sm"
                    >
                      <Trash2 className="w-4 h-4" />
                      Revoke
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </>
      )}

      <ConfirmDialog
        open={revokeTarget !== null}
        title="Revoke API key"
        message={
          <>
            Revoke <strong>{revokeTarget?.name}</strong>? Anything using it stops working immediately. This cannot be
            undone.
          </>
        }
        confirmLabel="Revoke"
        onConfirm={revoke}
        onCancel={() => setRevokeTarget(null)}
      />
    </div>
  )
}
