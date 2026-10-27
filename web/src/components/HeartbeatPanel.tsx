import { useCallback, useEffect, useRef, useState } from 'react'
import { Copy, HeartPulse, KeyRound, Loader2 } from 'lucide-react'
import { api, type HeartbeatInfo } from '../api/client'
import { ConfirmDialog } from './ConfirmDialog'
import { formatDateTime, formatDistanceToNow } from '../utils/date'
import { cronScheduleFor } from '../utils/heartbeat'

interface HeartbeatPanelProps {
  soulId: string
  /** Roles that may edit souls see the secret ping URL and can rotate it. */
  canManage: boolean
}

const formatRunDuration = (ns?: number) => {
  if (!ns || ns <= 0) return null
  const seconds = ns / 1e9
  if (seconds < 1) return `${Math.round(seconds * 1000)}ms`
  if (seconds < 120) return `${seconds.toFixed(1)}s`
  return `${Math.round(seconds / 60)}m`
}

export function HeartbeatPanel({ soulId, canManage }: HeartbeatPanelProps) {
  const [info, setInfo] = useState<HeartbeatInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [rotating, setRotating] = useState(false)
  const [confirmRotate, setConfirmRotate] = useState(false)
  const [copied, setCopied] = useState<string | null>(null)
  const request = useRef(0)

  const load = useCallback(async () => {
    const seq = ++request.current
    try {
      const result = await api.get<HeartbeatInfo>(`/souls/${soulId}/heartbeat`)
      if (seq === request.current) {
        setInfo(result)
        setError(null)
      }
    } catch (err) {
      if (seq === request.current) setError(err instanceof Error ? err.message : 'Failed to load heartbeat')
    }
  }, [soulId])

  useEffect(() => {
    void load()
    return () => { request.current += 1 }
  }, [load])

  const copy = (label: string, text: string) => {
    void navigator.clipboard?.writeText(text)
    setCopied(label)
  }

  const rotate = async () => {
    setConfirmRotate(false)
    setRotating(true)
    const seq = ++request.current
    try {
      const result = await api.post<HeartbeatInfo>(`/souls/${soulId}/heartbeat/rotate`)
      if (seq === request.current) {
        setInfo(result)
        setError(null)
      }
    } catch (err) {
      if (seq === request.current) setError(err instanceof Error ? err.message : 'Failed to rotate token')
    } finally {
      if (seq === request.current) setRotating(false)
    }
  }

  const state = info?.state
  const runDuration = formatRunDuration(state?.last_duration)
  const cronLine = info?.ping_url
    ? `${cronScheduleFor(info.interval)} /path/to/job.sh && curl -fsS -m 10 --retry 3 ${info.ping_url}`
    : null
  const failLine = info?.fail_url
    ? `/path/to/job.sh > /tmp/job.log 2>&1 || curl -fsS -m 10 --data-binary @/tmp/job.log ${info.fail_url}`
    : null

  return (
    <section
      aria-labelledby="heartbeat-title"
      className="bg-gradient-to-br from-gray-900 to-gray-800 border border-gray-700/50 rounded-2xl p-6 space-y-4"
    >
      <div className="flex items-center justify-between gap-3">
        <h2 id="heartbeat-title" className="text-lg font-semibold text-white flex items-center gap-2">
          <HeartPulse className="w-5 h-5 text-rose-300" />
          Heartbeat
        </h2>
        {info && (
          <span className="text-xs text-gray-400">
            Expected every {info.interval}, grace {info.grace}
          </span>
        )}
      </div>

      {error && <p role="alert" className="text-sm text-rose-400">{error}</p>}

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm" data-testid="heartbeat-last-ping">
        <div>
          <p className="text-gray-500">Last ping</p>
          <p className="text-white">
            {state?.last_ping_at ? (
              <span title={formatDateTime(state.last_ping_at)}>{formatDistanceToNow(state.last_ping_at)}</span>
            ) : 'Never'}
          </p>
        </div>
        <div>
          <p className="text-gray-500">Last result</p>
          <p className={state?.last_event === 'fail' ? 'text-rose-400' : 'text-white'}>
            {state?.last_event === 'fail' ? `Failed${state.last_message ? `: ${state.last_message}` : ''}` : state?.last_event === 'success' ? 'Success' : '—'}
          </p>
        </div>
        <div>
          <p className="text-gray-500">Run duration</p>
          <p className="text-white">{runDuration ?? '—'}</p>
        </div>
      </div>

      {canManage && info?.ping_url && (
        <div className="space-y-3">
          <div>
            <label htmlFor="heartbeat-ping-url" className="block text-sm font-medium text-gray-300 mb-2">Ping URL</label>
            <div className="flex gap-2">
              <input
                id="heartbeat-ping-url"
                readOnly
                value={info.ping_url}
                className="flex-1 min-w-0 bg-gray-950 border border-gray-700 rounded-xl px-3 py-2 text-white font-mono text-xs"
              />
              <button
                type="button"
                onClick={() => copy('url', info.ping_url!)}
                className="px-3 py-2 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl flex items-center gap-1 text-sm"
                aria-label="Copy ping URL"
              >
                <Copy className="w-4 h-4" />
                {copied === 'url' ? 'Copied' : 'Copy'}
              </button>
            </div>
            <p className="mt-1 text-xs text-gray-500">
              GET or POST it when the job succeeds. Add /start when it begins to measure run time,
              or POST the error output to /fail to report a failure. Keep the URL secret.
            </p>
          </div>
          {cronLine && (
            <div>
              <p className="text-sm font-medium text-gray-300 mb-1">Example crontab line</p>
              <div className="flex gap-2 items-start">
                <pre className="flex-1 min-w-0 overflow-x-auto bg-gray-950 border border-gray-700 rounded-xl p-3 text-xs text-gray-200" data-testid="heartbeat-cron-line">{cronLine}</pre>
                <button
                  type="button"
                  onClick={() => copy('cron', cronLine)}
                  className="p-2 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl"
                  aria-label="Copy cron line"
                >
                  <Copy className="w-4 h-4" />
                </button>
              </div>
              {failLine && (
                <pre className="mt-2 overflow-x-auto bg-gray-950 border border-gray-700 rounded-xl p-3 text-xs text-gray-400">{failLine}</pre>
              )}
            </div>
          )}
          <button
            type="button"
            onClick={() => setConfirmRotate(true)}
            disabled={rotating}
            className="flex items-center gap-2 px-4 py-2 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-xl text-sm disabled:opacity-50"
          >
            {rotating ? <Loader2 className="w-4 h-4 animate-spin" /> : <KeyRound className="w-4 h-4" />}
            Rotate token
          </button>
        </div>
      )}

      <ConfirmDialog
        open={confirmRotate}
        title="Rotate ping token?"
        message="The current ping URL stops working immediately. Update every job that pings it."
        confirmLabel="Rotate"
        onConfirm={() => void rotate()}
        onCancel={() => setConfirmRotate(false)}
      />
    </section>
  )
}
