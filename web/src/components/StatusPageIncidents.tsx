import { useCallback, useEffect, useRef, useState } from 'react'
import { AlertTriangle, CalendarClock, Check, Eye, Pencil, Trash2, X } from 'lucide-react'
import { api } from '../api/client'
import type {
  IncidentImpact,
  IncidentStatus,
  StatusPage,
  StatusPageAnnouncement,
  StatusPageIncident,
  StatusPageIncidentUpdate,
} from '../api/client'
import { ConfirmDialog } from './ConfirmDialog'
import { useCan } from '../utils/permissions'
import { formatDateTime } from '../utils/date'

// Operator-posted incidents and scheduled maintenance for one status page,
// edited through the dedicated /status-pages/:id/incidents and
// /announcements endpoints so concurrent editors never overwrite each other.
// All text is plain text: React escapes it here and the server escapes it on
// the public page.

// Re-exported for the status page editor; the shapes come from the spec.
export type { IncidentImpact, IncidentStatus, StatusPageAnnouncement, StatusPageIncident, StatusPageIncidentUpdate }

interface MaintenanceWindowOption {
  id: string
  name: string
  recurring?: string
}

interface SoulOption {
  id: string
  name: string
}

const IMPACTS: IncidentImpact[] = ['none', 'minor', 'major', 'critical']
const STATUSES: IncidentStatus[] = ['investigating', 'identified', 'monitoring', 'resolved']

const impactClass: Record<string, string> = {
  none: 'bg-blue-500/10 text-blue-300',
  minor: 'bg-amber-500/10 text-amber-300',
  major: 'bg-orange-500/10 text-orange-300',
  critical: 'bg-rose-500/10 text-rose-300',
}

const phaseLabel: Record<string, string> = {
  upcoming: 'Upcoming',
  in_progress: 'In progress',
  completed: 'Completed',
}

const inputClass = 'w-full bg-gray-950 border border-gray-700/50 rounded-xl px-3 py-2 text-sm text-white focus:outline-none focus:border-amber-500/50'

function label(v: string) {
  return v.charAt(0).toUpperCase() + v.slice(1).replace('_', ' ')
}

function toLocalInput(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

function errorText(err: unknown) {
  return err instanceof Error ? err.message : 'Request failed'
}

function SoulPicker({ souls, selected, onChange, idPrefix }: { souls: SoulOption[]; selected: string[]; onChange: (ids: string[]) => void; idPrefix: string }) {
  if (souls.length === 0) {
    return <p className="text-xs text-gray-500">This page shows no services.</p>
  }
  return (
    <div className="flex flex-wrap gap-2">
      {souls.map((soul) => {
        const id = `${idPrefix}-${soul.id}`
        return (
          <label key={soul.id} htmlFor={id} className="flex items-center gap-2 px-2 py-1 bg-gray-800/60 rounded-lg text-xs text-gray-300 cursor-pointer">
            <input
              id={id}
              type="checkbox"
              checked={selected.includes(soul.id)}
              onChange={() => onChange(selected.includes(soul.id) ? selected.filter((s) => s !== soul.id) : [...selected, soul.id])}
            />
            {soul.name}
          </label>
        )
      })}
    </div>
  )
}

function UpdateForm({ incident, onPost }: { incident: StatusPageIncident; onPost: (message: string, status: IncidentStatus) => Promise<void> }) {
  const [message, setMessage] = useState('')
  const [status, setStatus] = useState<IncidentStatus>(
    STATUSES.includes(incident.status as IncidentStatus) && incident.status !== 'resolved' ? (incident.status as IncidentStatus) : 'monitoring'
  )
  const [busy, setBusy] = useState(false)
  return (
    <form
      className="mt-3 space-y-2"
      onSubmit={(e) => {
        e.preventDefault()
        if (!message.trim()) return
        setBusy(true)
        void onPost(message, status).then(() => setMessage('')).finally(() => setBusy(false))
      }}
    >
      <label className="sr-only" htmlFor={`update-${incident.id}`}>Update message for {incident.title}</label>
      <textarea id={`update-${incident.id}`} className={inputClass} rows={2} maxLength={5000} value={message} onChange={(e) => setMessage(e.target.value)} placeholder="Post an update…" />
      <div className="flex items-center gap-2">
        <label className="sr-only" htmlFor={`update-status-${incident.id}`}>Update status</label>
        <select id={`update-status-${incident.id}`} className={inputClass + ' max-w-[12rem]'} value={status} onChange={(e) => setStatus(e.target.value as IncidentStatus)}>
          {STATUSES.map((s) => <option key={s} value={s}>{label(s)}</option>)}
        </select>
        <button type="submit" disabled={busy || !message.trim()} className="px-3 py-2 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white rounded-xl text-sm">
          Post update
        </button>
      </div>
    </form>
  )
}

interface AnnouncementDraft {
  id?: string
  title: string
  message: string
  starts: string
  ends: string
  souls: string[]
  windowId: string
}

const emptyDraft: AnnouncementDraft = { title: '', message: '', starts: '', ends: '', souls: [], windowId: '' }

export function StatusPageIncidents({ page, souls, onClose }: { page: StatusPage; souls: SoulOption[]; onClose: () => void }) {
  const can = useCan()
  const canWrite = can('settings:write')
  const base = `/status-pages/${encodeURIComponent(page.id)}`
  const pageSouls = (page.souls || []).map((id) => souls.find((s) => s.id === id) ?? { id, name: id })
  const soulName = (id: string) => pageSouls.find((s) => s.id === id)?.name ?? id

  const [tab, setTab] = useState<'incidents' | 'maintenance'>('incidents')
  const [incidents, setIncidents] = useState<StatusPageIncident[]>([])
  const [announcements, setAnnouncements] = useState<StatusPageAnnouncement[]>([])
  const [windows, setWindows] = useState<MaintenanceWindowOption[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [confirm, setConfirm] = useState<{ kind: 'incident' | 'announcement'; id: string; name: string } | null>(null)

  const [title, setTitle] = useState('')
  const [impact, setImpact] = useState<IncidentImpact>('minor')
  const [status, setStatus] = useState<IncidentStatus>('investigating')
  const [message, setMessage] = useState('')
  const [affected, setAffected] = useState<string[]>([])
  const [draft, setDraft] = useState<AnnouncementDraft>(emptyDraft)
  const generation = useRef(0)

  const load = useCallback(async () => {
    const gen = ++generation.current
    try {
      const [inc, ann] = await Promise.all([
        api.get<StatusPageIncident[]>(`${base}/incidents`),
        api.get<StatusPageAnnouncement[]>(`${base}/announcements`),
      ])
      if (gen !== generation.current) return
      setIncidents([...(inc || [])].sort((a, b) => b.started_at.localeCompare(a.started_at)))
      setAnnouncements([...(ann || [])].sort((a, b) => a.starts_at.localeCompare(b.starts_at)))
      setError('')
    } catch (err) {
      if (gen === generation.current) setError(errorText(err))
    } finally {
      if (gen === generation.current) setLoading(false)
    }
  }, [base])

  useEffect(() => {
    void load()
    return () => { generation.current += 1 }
  }, [load])

  useEffect(() => {
    if (!canWrite) return
    let live = true
    api.get<MaintenanceWindowOption[] | { data: MaintenanceWindowOption[] }>('/maintenance')
      .then((res) => {
        const list = Array.isArray(res) ? res : res?.data || []
        if (live) setWindows(list.filter((w) => !w.recurring))
      })
      .catch(() => { /* linking is optional */ })
    return () => { live = false }
  }, [canWrite])

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      setError('')
      await load()
      return true
    } catch (err) {
      setError(errorText(err))
      return false
    }
  }

  const createIncident = async () => {
    if (!title.trim() || !message.trim()) return
    const ok = await run(() => api.post(`${base}/incidents`, { title, impact, status, message, affected_souls: affected }))
    if (ok) {
      setTitle('')
      setMessage('')
      setAffected([])
      setImpact('minor')
      setStatus('investigating')
    }
  }

  const saveAnnouncement = async () => {
    const body: Record<string, unknown> = { title: draft.title, message: draft.message, affected_souls: draft.souls }
    if (draft.windowId) body.maintenance_window_id = draft.windowId
    if (draft.starts) body.starts_at = new Date(draft.starts).toISOString()
    if (draft.ends) body.ends_at = new Date(draft.ends).toISOString()
    if (!draft.windowId && (!draft.starts || !draft.ends)) {
      setError('Start and end times are required.')
      return
    }
    if (draft.starts && draft.ends && new Date(draft.ends) <= new Date(draft.starts)) {
      setError('The end time must be after the start time.')
      return
    }
    const ok = await run(() => draft.id
      ? api.patch(`${base}/announcements/${encodeURIComponent(draft.id)}`, body)
      : api.post(`${base}/announcements`, body))
    if (ok) setDraft(emptyDraft)
  }

  const confirmDelete = async () => {
    if (!confirm) return
    const { kind, id } = confirm
    setConfirm(null)
    await run(() => api.delete(`${base}/${kind === 'incident' ? 'incidents' : 'announcements'}/${encodeURIComponent(id)}`))
  }

  const tabClass = (active: boolean) =>
    `flex items-center gap-2 px-4 py-2 rounded-xl text-sm ${active ? 'bg-amber-600 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-800'}`

  return (
    <>
      <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50 p-4" role="dialog" aria-modal="true" aria-labelledby="status-incidents-title">
        <div className="bg-gray-900 border border-gray-700/50 rounded-2xl w-full max-w-3xl max-h-[90vh] flex flex-col">
          <div className="flex items-center justify-between p-5 border-b border-gray-700/50">
            <div>
              <h2 id="status-incidents-title" className="text-xl font-semibold text-white">Incidents · {page.name}</h2>
              <a href={`/status/${page.slug}`} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-sm text-amber-400 hover:text-amber-300 mt-1">
                <Eye className="w-4 h-4" /> Preview live page
              </a>
            </div>
            <button onClick={onClose} className="p-2 text-gray-400 hover:text-white rounded-lg" aria-label="Close incidents">
              <X className="w-5 h-5" />
            </button>
          </div>

          <div className="flex gap-2 px-5 pt-4" role="tablist">
            <button role="tab" aria-selected={tab === 'incidents'} className={tabClass(tab === 'incidents')} onClick={() => setTab('incidents')}>
              <AlertTriangle className="w-4 h-4" /> Incidents
            </button>
            <button role="tab" aria-selected={tab === 'maintenance'} className={tabClass(tab === 'maintenance')} onClick={() => setTab('maintenance')}>
              <CalendarClock className="w-4 h-4" /> Maintenance
            </button>
          </div>

          <div className="p-5 overflow-y-auto space-y-5">
            {error && <p role="alert" className="text-sm text-rose-400">{error}</p>}
            {loading ? (
              <p className="text-sm text-gray-400" role="status">Loading…</p>
            ) : tab === 'incidents' ? (
              <>
                {canWrite && (
                  <form
                    className="space-y-3 bg-gray-800/40 rounded-xl p-4"
                    aria-label="Create incident"
                    onSubmit={(e) => { e.preventDefault(); void createIncident() }}
                  >
                    <h3 className="text-sm font-semibold text-white">Declare an incident</h3>
                    <label className="block text-xs text-gray-400" htmlFor="incident-title">Title</label>
                    <input id="incident-title" className={inputClass} maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="block text-xs text-gray-400" htmlFor="incident-impact">Impact</label>
                        <select id="incident-impact" className={inputClass} value={impact} onChange={(e) => setImpact(e.target.value as IncidentImpact)}>
                          {IMPACTS.map((i) => <option key={i} value={i}>{label(i)}</option>)}
                        </select>
                      </div>
                      <div>
                        <label className="block text-xs text-gray-400" htmlFor="incident-status">Status</label>
                        <select id="incident-status" className={inputClass} value={status} onChange={(e) => setStatus(e.target.value as IncidentStatus)}>
                          {STATUSES.map((s) => <option key={s} value={s}>{label(s)}</option>)}
                        </select>
                      </div>
                    </div>
                    <label className="block text-xs text-gray-400" htmlFor="incident-message">Message (plain text; links are clickable on the public page)</label>
                    <textarea id="incident-message" className={inputClass} rows={3} maxLength={5000} value={message} onChange={(e) => setMessage(e.target.value)} />
                    <p className="text-xs text-gray-400">Affected services</p>
                    <SoulPicker souls={pageSouls} selected={affected} onChange={setAffected} idPrefix="incident-soul" />
                    <button type="submit" disabled={!title.trim() || !message.trim()} className="px-4 py-2 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white rounded-xl text-sm">
                      Create incident
                    </button>
                  </form>
                )}
                {incidents.length === 0 ? (
                  <p className="text-sm text-gray-500">No incidents posted on this page.</p>
                ) : (
                  <ul className="space-y-4" aria-label="Incidents">
                    {incidents.map((inc) => {
                      const impactValue = inc.impact || 'none'
                      const resolved = inc.status === 'resolved'
                      return (
                        <li key={inc.id} className="bg-gray-800/40 rounded-xl p-4">
                          <div className="flex items-start justify-between gap-3">
                            <div>
                              <h3 className="font-semibold text-white break-words">{inc.title}</h3>
                              <div className="flex flex-wrap items-center gap-2 mt-1 text-xs">
                                <span className={`px-2 py-0.5 rounded-lg ${impactClass[impactValue] ?? impactClass.none}`}>{label(impactValue)}</span>
                                <span className={resolved ? 'text-emerald-400' : 'text-amber-300'}>{label(inc.status)}</span>
                                <span className="text-gray-500">Started {formatDateTime(inc.started_at)}</span>
                              </div>
                              {(inc.affected_souls || []).length > 0 && (
                                <p className="text-xs text-gray-400 mt-1">Affected: {(inc.affected_souls || []).map(soulName).join(', ')}</p>
                              )}
                            </div>
                            {canWrite && (
                              <div className="flex items-center gap-1 shrink-0">
                                {!resolved && (
                                  <button
                                    onClick={() => { void run(() => api.post(`${base}/incidents/${encodeURIComponent(inc.id)}/resolve`, {})) }}
                                    className="flex items-center gap-1 px-2 py-1 text-xs text-emerald-400 hover:bg-emerald-500/10 rounded-lg"
                                    aria-label={`Resolve incident ${inc.title}`}
                                  >
                                    <Check className="w-4 h-4" /> Resolve
                                  </button>
                                )}
                                <button
                                  onClick={() => setConfirm({ kind: 'incident', id: inc.id, name: inc.title })}
                                  className="p-1.5 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg"
                                  aria-label={`Delete incident ${inc.title}`}
                                >
                                  <Trash2 className="w-4 h-4" />
                                </button>
                              </div>
                            )}
                          </div>
                          <ol className="mt-3 border-l-2 border-gray-700 pl-3 space-y-2">
                            {[...(inc.updates || [])].reverse().map((u) => (
                              <li key={u.id} className="text-sm">
                                <span className="font-medium text-gray-200">{label(u.status)}</span>
                                <span className="text-xs text-gray-500 ml-2">{formatDateTime(u.updated_at)}</span>
                                <p className="text-gray-300 whitespace-pre-wrap break-words">{u.message}</p>
                              </li>
                            ))}
                          </ol>
                          {canWrite && (
                            <UpdateForm
                              incident={inc}
                              onPost={async (msg, st) => {
                                await run(() => api.post(`${base}/incidents/${encodeURIComponent(inc.id)}/updates`, { message: msg, status: st }))
                              }}
                            />
                          )}
                        </li>
                      )
                    })}
                  </ul>
                )}
              </>
            ) : (
              <>
                {canWrite && (
                  <form
                    className="space-y-3 bg-gray-800/40 rounded-xl p-4"
                    aria-label="Maintenance announcement"
                    onSubmit={(e) => { e.preventDefault(); void saveAnnouncement() }}
                  >
                    <h3 className="text-sm font-semibold text-white">{draft.id ? 'Edit announcement' : 'Announce maintenance'}</h3>
                    {windows.length > 0 && (
                      <>
                        <label className="block text-xs text-gray-400" htmlFor="announcement-window">Maintenance window (optional; supplies the schedule)</label>
                        <select id="announcement-window" className={inputClass} value={draft.windowId} onChange={(e) => setDraft({ ...draft, windowId: e.target.value })}>
                          <option value="">None</option>
                          {windows.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
                        </select>
                      </>
                    )}
                    <label className="block text-xs text-gray-400" htmlFor="announcement-title">Title</label>
                    <input id="announcement-title" className={inputClass} maxLength={200} value={draft.title} onChange={(e) => setDraft({ ...draft, title: e.target.value })} />
                    <label className="block text-xs text-gray-400" htmlFor="announcement-message">Message</label>
                    <textarea id="announcement-message" className={inputClass} rows={2} maxLength={5000} value={draft.message} onChange={(e) => setDraft({ ...draft, message: e.target.value })} />
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="block text-xs text-gray-400" htmlFor="announcement-starts">Starts</label>
                        <input id="announcement-starts" type="datetime-local" className={inputClass} value={draft.starts} onChange={(e) => setDraft({ ...draft, starts: e.target.value })} />
                      </div>
                      <div>
                        <label className="block text-xs text-gray-400" htmlFor="announcement-ends">Ends</label>
                        <input id="announcement-ends" type="datetime-local" className={inputClass} value={draft.ends} onChange={(e) => setDraft({ ...draft, ends: e.target.value })} />
                      </div>
                    </div>
                    <p className="text-xs text-gray-400">Affected services</p>
                    <SoulPicker souls={pageSouls} selected={draft.souls} onChange={(ids) => setDraft({ ...draft, souls: ids })} idPrefix="announcement-soul" />
                    <div className="flex gap-2">
                      <button type="submit" disabled={!draft.title.trim() && !draft.windowId} className="px-4 py-2 bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white rounded-xl text-sm">
                        {draft.id ? 'Save announcement' : 'Create announcement'}
                      </button>
                      {draft.id && (
                        <button type="button" onClick={() => setDraft(emptyDraft)} className="px-4 py-2 text-gray-400 hover:text-white text-sm">Cancel edit</button>
                      )}
                    </div>
                  </form>
                )}
                {announcements.length === 0 ? (
                  <p className="text-sm text-gray-500">No maintenance announced on this page.</p>
                ) : (
                  <ul className="space-y-3" aria-label="Maintenance announcements">
                    {announcements.map((a) => (
                      <li key={a.id} className="bg-gray-800/40 rounded-xl p-4 flex items-start justify-between gap-3">
                        <div>
                          <h3 className="font-semibold text-white break-words">{a.title}</h3>
                          <p className="text-xs text-gray-400 mt-1">
                            <span className="text-blue-300 mr-2">{phaseLabel[a.phase || ''] ?? ''}</span>
                            {formatDateTime(a.starts_at)} – {formatDateTime(a.ends_at)}
                          </p>
                          {a.message && <p className="text-sm text-gray-300 mt-1 whitespace-pre-wrap break-words">{a.message}</p>}
                          {(a.affected_souls || []).length > 0 && (
                            <p className="text-xs text-gray-400 mt-1">Affected: {(a.affected_souls || []).map(soulName).join(', ')}</p>
                          )}
                        </div>
                        {canWrite && (
                          <div className="flex items-center gap-1 shrink-0">
                            <button
                              onClick={() => setDraft({ id: a.id, title: a.title, message: a.message || '', starts: toLocalInput(a.starts_at), ends: toLocalInput(a.ends_at), souls: a.affected_souls || [], windowId: '' })}
                              className="p-1.5 text-gray-400 hover:text-white hover:bg-gray-700 rounded-lg"
                              aria-label={`Edit announcement ${a.title}`}
                            >
                              <Pencil className="w-4 h-4" />
                            </button>
                            <button
                              onClick={() => setConfirm({ kind: 'announcement', id: a.id, name: a.title })}
                              className="p-1.5 text-gray-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg"
                              aria-label={`Delete announcement ${a.title}`}
                            >
                              <Trash2 className="w-4 h-4" />
                            </button>
                          </div>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </>
            )}
          </div>
        </div>
      </div>
      <ConfirmDialog
        open={confirm !== null}
        title={confirm?.kind === 'announcement' ? 'Delete Announcement' : 'Delete Incident'}
        message={<>Delete <strong className="text-[var(--text-primary)]">{confirm?.name}</strong>? It disappears from the public page and its history. This cannot be undone.</>}
        confirmLabel="Delete"
        onConfirm={() => { void confirmDelete() }}
        onCancel={() => setConfirm(null)}
        resourceName={confirm?.name}
      />
    </>
  )
}
