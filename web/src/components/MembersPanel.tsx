import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Check, Copy, Loader2, MailPlus, Trash2, UserPlus, Users } from 'lucide-react'
import { api, type CreatedInvitation, type Invitation, type Member } from '../api/client'
import { getLastKnownUser } from '../api/authEvents'
import { formatDate, formatDistanceToNow } from '../utils/date'
import { useCan } from '../utils/permissions'
import { ConfirmDialog } from './ConfirmDialog'

// Mirrors core.RoleWithin: nobody grants or manages a role above their own.
const roleRank: Record<string, number> = { viewer: 1, api: 2, editor: 3, admin: 4, owner: 5 }
const allRoles = ['viewer', 'api', 'editor', 'admin', 'owner']

const inputClass = 'w-full bg-gray-950 border border-gray-700/50 rounded-xl px-3 py-2 text-white text-sm'

type Pending =
  | { kind: 'remove'; member: Member }
  | { kind: 'disable'; member: Member }
  | { kind: 'revoke'; invitation: Invitation }

const errorText = (err: unknown, fallback: string) => (err instanceof Error && err.message ? err.message : fallback)

/** Absolute form of an invitation link the server may return as a path. */
function absoluteLink(link: string) {
  if (/^https?:\/\//.test(link)) return link
  return `${window.location.origin}${link.startsWith('/') ? '' : '/'}${link}`
}

/**
 * Settings → Members: the current workspace's members (role, disable,
 * remove), invitations (send, copy link, revoke) and direct account creation.
 */
export function MembersPanel() {
  const can = useCan()
  const allowed = can('members:write')
  const me = getLastKnownUser()
  const myRank = roleRank[me?.role ?? 'viewer'] ?? 0
  const grantable = allRoles.filter((r) => (roleRank[r] ?? 0) <= myRank)

  const [members, setMembers] = useState<Member[]>([])
  const [invitations, setInvitations] = useState<Invitation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState('viewer')
  const [inviting, setInviting] = useState(false)
  const [inviteLink, setInviteLink] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const [showCreate, setShowCreate] = useState(false)
  const [create, setCreate] = useState({ email: '', name: '', password: '', role: 'viewer' })
  const [creating, setCreating] = useState(false)

  const [pending, setPending] = useState<Pending | null>(null)

  const load = useCallback(async () => {
    try {
      const [m, i] = await Promise.all([api.get<Member[]>('/members'), api.get<Invitation[]>('/invitations')])
      setMembers(Array.isArray(m) ? m : [])
      setInvitations(Array.isArray(i) ? i : [])
      setError(null)
    } catch (err) {
      setError(errorText(err, 'Failed to load members'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (allowed) void load()
    else setLoading(false)
  }, [allowed, load])

  const run = async (action: () => Promise<unknown>, fallback: string, done?: string) => {
    setError(null)
    setNotice(null)
    try {
      await action()
      if (done) setNotice(done)
      await load()
      return true
    } catch (err) {
      setError(errorText(err, fallback))
      return false
    }
  }

  const invite = async (e: FormEvent) => {
    e.preventDefault()
    if (!inviteEmail.trim()) {
      setError('Email is required')
      return
    }
    setInviting(true)
    setInviteLink(null)
    setCopied(false)
    await run(async () => {
      const result = await api.post<CreatedInvitation>('/invitations', { email: inviteEmail.trim(), role: inviteRole })
      if (result?.invite_url) setInviteLink(absoluteLink(result.invite_url))
      setNotice(
        result?.email_sent
          ? `Invitation emailed to ${result.email}.`
          : `Invitation created for ${result?.email ?? inviteEmail.trim()}. Share the link below; it is shown only once.`,
      )
      setInviteEmail('')
    }, 'Failed to send invitation')
    setInviting(false)
  }

  const createUser = async (e: FormEvent) => {
    e.preventDefault()
    setCreating(true)
    const ok = await run(
      () => api.post<Member>('/members', { ...create, email: create.email.trim(), name: create.name.trim() }),
      'Failed to create member',
      `Account created for ${create.email.trim()}. They must change the password at first sign-in.`,
    )
    if (ok) {
      setCreate({ email: '', name: '', password: '', role: 'viewer' })
      setShowCreate(false)
    }
    setCreating(false)
  }

  const copyLink = async () => {
    if (!inviteLink) return
    try {
      await navigator.clipboard.writeText(inviteLink)
      setCopied(true)
    } catch {
      setError('Copy failed; select the link and copy it manually')
    }
  }

  const changeRole = (member: Member, role: string) =>
    run(() => api.patch(`/members/${encodeURIComponent(member.id)}`, { role }), 'Failed to change role', `${member.email} is now ${role}.`)

  const enable = (member: Member) =>
    run(() => api.patch(`/members/${encodeURIComponent(member.id)}`, { disabled: false }), 'Failed to enable member', `${member.email} enabled.`)

  const confirmPending = async () => {
    const target = pending
    setPending(null)
    if (!target) return
    if (target.kind === 'remove') {
      await run(() => api.delete(`/members/${encodeURIComponent(target.member.id)}`), 'Failed to remove member', `${target.member.email} removed.`)
    } else if (target.kind === 'disable') {
      await run(
        () => api.patch(`/members/${encodeURIComponent(target.member.id)}`, { disabled: true }),
        'Failed to disable member',
        `${target.member.email} disabled.`,
      )
    } else {
      await run(() => api.delete(`/invitations/${encodeURIComponent(target.invitation.id)}`), 'Failed to revoke invitation', 'Invitation revoked.')
    }
  }

  const manageable = (m: Member) => m.id !== me?.id && (roleRank[m.role] ?? 0) <= myRank

  return (
    <div className="space-y-6" aria-labelledby="members-heading">
      <div className="flex items-center gap-2">
        <Users className="w-5 h-5 text-amber-400" aria-hidden="true" />
        <h3 id="members-heading" className="font-medium text-white">Members</h3>
      </div>
      <p className="text-sm text-gray-400">
        People with access to this workspace. Invite someone by email (people who already have an account accept
        with their own password), or create a new account with an initial password they must change at first
        sign-in. Nobody can grant a role above their own, and every workspace keeps at least one owner.
      </p>

      {!allowed ? (
        <p className="text-sm text-gray-500">Only workspace admins and owners can manage members.</p>
      ) : (
        <>
          {error && (
            <p role="alert" className="text-sm text-rose-400">
              {error}
            </p>
          )}
          {notice && (
            <p role="status" className="text-sm text-emerald-400">
              {notice}
            </p>
          )}
          {inviteLink && (
            <div className="rounded-xl border border-emerald-500/30 bg-emerald-500/5 p-4 space-y-2">
              <div className="flex gap-2">
                <input
                  aria-label="Invitation link"
                  readOnly
                  value={inviteLink}
                  onFocus={(e) => e.currentTarget.select()}
                  className="flex-1 bg-gray-950 border border-gray-700/50 rounded-xl px-3 py-2 text-white font-mono text-xs"
                />
                <button
                  type="button"
                  onClick={copyLink}
                  className="flex items-center gap-1 px-3 py-2 rounded-xl bg-gray-800 text-gray-200 hover:bg-gray-700 text-sm"
                >
                  {copied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                  {copied ? 'Copied' : 'Copy link'}
                </button>
                <button type="button" onClick={() => setInviteLink(null)} className="px-3 py-2 rounded-xl text-gray-400 hover:text-white text-sm">
                  Done
                </button>
              </div>
            </div>
          )}

          <form onSubmit={invite} className="grid grid-cols-1 md:grid-cols-4 gap-3 items-end">
            <div className="md:col-span-2">
              <label htmlFor="invite-email" className="block text-sm font-medium text-gray-300 mb-1">
                Email
              </label>
              <input
                id="invite-email"
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                placeholder="name@example.com"
                className={inputClass}
              />
            </div>
            <div>
              <label htmlFor="invite-role" className="block text-sm font-medium text-gray-300 mb-1">
                Role
              </label>
              <select id="invite-role" value={inviteRole} onChange={(e) => setInviteRole(e.target.value)} className={inputClass}>
                {grantable.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={inviting}
                className="flex items-center gap-2 px-4 py-2 rounded-xl bg-amber-500 text-gray-950 font-medium text-sm hover:bg-amber-400 disabled:opacity-50"
              >
                {inviting ? <Loader2 className="w-4 h-4 animate-spin" /> : <MailPlus className="w-4 h-4" />}
                Invite
              </button>
              <button
                type="button"
                onClick={() => setShowCreate((v) => !v)}
                aria-expanded={showCreate}
                className="flex items-center gap-2 px-3 py-2 rounded-xl bg-gray-800 text-gray-200 hover:bg-gray-700 text-sm"
              >
                <UserPlus className="w-4 h-4" />
                Create
              </button>
            </div>
          </form>

          {showCreate && (
            <form onSubmit={createUser} aria-label="Create account" className="grid grid-cols-1 md:grid-cols-4 gap-3 items-end rounded-xl border border-gray-700/50 p-4">
              <div>
                <label htmlFor="create-email" className="block text-sm font-medium text-gray-300 mb-1">
                  Email
                </label>
                <input id="create-email" type="email" required value={create.email} onChange={(e) => setCreate({ ...create, email: e.target.value })} className={inputClass} />
              </div>
              <div>
                <label htmlFor="create-name" className="block text-sm font-medium text-gray-300 mb-1">
                  Name
                </label>
                <input id="create-name" maxLength={100} value={create.name} onChange={(e) => setCreate({ ...create, name: e.target.value })} className={inputClass} />
              </div>
              <div>
                <label htmlFor="create-password" className="block text-sm font-medium text-gray-300 mb-1">
                  Initial password
                </label>
                <input
                  id="create-password"
                  type="password"
                  required
                  minLength={12}
                  autoComplete="new-password"
                  value={create.password}
                  onChange={(e) => setCreate({ ...create, password: e.target.value })}
                  className={inputClass}
                />
              </div>
              <div>
                <label htmlFor="create-role" className="block text-sm font-medium text-gray-300 mb-1">
                  Role
                </label>
                <select id="create-role" value={create.role} onChange={(e) => setCreate({ ...create, role: e.target.value })} className={inputClass}>
                  {grantable.map((r) => (
                    <option key={r} value={r}>
                      {r}
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
                  {creating ? <Loader2 className="w-4 h-4 animate-spin" /> : <UserPlus className="w-4 h-4" />}
                  Create account
                </button>
              </div>
            </form>
          )}

          {loading ? (
            <p className="text-sm text-gray-500">Loading members…</p>
          ) : (
            <ul className="space-y-2" aria-label="Members">
              {members.map((m) => (
                <li key={m.id} className="flex flex-wrap items-center justify-between gap-2 py-2 px-4 bg-gray-950 rounded-xl">
                  <div className="min-w-0">
                    <p className="text-sm text-white">
                      {m.name || m.email}
                      {m.id === me?.id && <span className="ml-2 text-xs text-gray-500">(you)</span>}
                      {m.disabled && <span className="ml-2 px-2 py-0.5 rounded-full text-xs bg-gray-700/50 text-gray-400">disabled</span>}
                      {m.source !== 'local' && <span className="ml-2 px-2 py-0.5 rounded-full text-xs bg-sky-500/10 text-sky-400">{m.source.toUpperCase()}</span>}
                    </p>
                    <p className="text-xs text-gray-500 truncate">
                      {m.email} · {m.last_login_at ? `last sign-in ${formatDistanceToNow(m.last_login_at)}` : 'never signed in'}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <select
                      aria-label={`Role of ${m.email}`}
                      value={m.role}
                      disabled={!manageable(m) && m.id !== me?.id}
                      onChange={(e) => void changeRole(m, e.target.value)}
                      className="bg-gray-900 border border-gray-700/50 rounded-lg px-2 py-1 text-white text-sm disabled:opacity-60"
                    >
                      {allRoles
                        .filter((r) => r === m.role || (roleRank[r] ?? 0) <= myRank)
                        .map((r) => (
                          <option key={r} value={r}>
                            {r}
                          </option>
                        ))}
                    </select>
                    {manageable(m) && !m.configured_admin && (
                      m.disabled ? (
                        <button type="button" onClick={() => void enable(m)} className="px-3 py-1.5 rounded-lg text-emerald-400 hover:bg-emerald-500/10 text-sm">
                          Enable
                        </button>
                      ) : (
                        <button
                          type="button"
                          onClick={() => setPending({ kind: 'disable', member: m })}
                          aria-label={`Disable ${m.email}`}
                          className="px-3 py-1.5 rounded-lg text-amber-400 hover:bg-amber-500/10 text-sm"
                        >
                          Disable
                        </button>
                      )
                    )}
                    {manageable(m) && (
                      <button
                        type="button"
                        onClick={() => setPending({ kind: 'remove', member: m })}
                        aria-label={`Remove ${m.email}`}
                        className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-rose-400 hover:bg-rose-500/10 text-sm"
                      >
                        <Trash2 className="w-4 h-4" />
                        Remove
                      </button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}

          <div className="space-y-2">
            <h4 className="text-sm font-medium text-gray-300">Pending invitations</h4>
            {invitations.length === 0 ? (
              <p className="text-sm text-gray-500">No pending invitations.</p>
            ) : (
              <ul className="space-y-2" aria-label="Pending invitations">
                {invitations.map((inv) => (
                  <li key={inv.id} className="flex flex-wrap items-center justify-between gap-2 py-2 px-4 bg-gray-950 rounded-xl">
                    <div className="min-w-0">
                      <p className="text-sm text-white">{inv.email}</p>
                      <p className="text-xs text-gray-500">
                        {inv.role} · invited by {inv.invited_by || 'unknown'} · expires {formatDate(inv.expires_at)}
                      </p>
                    </div>
                    {(roleRank[inv.role] ?? 0) <= myRank && (
                      <button
                        type="button"
                        onClick={() => setPending({ kind: 'revoke', invitation: inv })}
                        aria-label={`Revoke invitation for ${inv.email}`}
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
          </div>
        </>
      )}

      <ConfirmDialog
        open={pending !== null}
        title={pending?.kind === 'remove' ? 'Remove member' : pending?.kind === 'disable' ? 'Disable account' : 'Revoke invitation'}
        message={
          pending?.kind === 'revoke' ? (
            <>
              Revoke the invitation for <strong>{pending.invitation.email}</strong>? The link stops working.
            </>
          ) : pending?.kind === 'disable' ? (
            <>
              Disable <strong>{pending.member.email}</strong>? They are signed out everywhere at once and cannot sign in,
              and their API keys stop working, until enabled again.
            </>
          ) : (
            <>
              Remove <strong>{pending?.member.email}</strong> from this workspace? They are signed out at once. An account
              left without any workspace is deleted.
            </>
          )
        }
        confirmLabel={pending?.kind === 'remove' ? 'Remove' : pending?.kind === 'disable' ? 'Disable' : 'Revoke'}
        onConfirm={confirmPending}
        onCancel={() => setPending(null)}
      />
    </div>
  )
}
