import { useMemo, useState } from 'react'
import { Activity, AlertTriangle, CheckCircle2, Clock, Zap, X } from 'lucide-react'
import { formatDistanceToNow } from '../utils/date'
import { useWebSocket, type WebSocketMessage } from '../hooks/webSocketContext'

interface Event {
  id: string
  type: 'success' | 'warning' | 'error' | 'info'
  message: string
  soulName?: string
  timestamp: string
}

interface EventsFeedProps {
  maxEvents?: number
}

export function EventsFeed({ maxEvents = 10 }: EventsFeedProps) {
  const { messages } = useWebSocket()
  const [dismissed, setDismissed] = useState<Set<string>>(new Set())

  const events = useMemo(
    () => messages.map(messageToEvent).filter((event): event is Event => event !== null),
    [messages]
  )

  const dismissEvent = (id: string) => {
    setDismissed(prev => new Set(prev).add(id))
  }

  const filteredEvents = events.filter(e => !dismissed.has(e.id)).slice(0, maxEvents)

  const getIcon = (type: Event['type']) => {
    switch (type) {
      case 'success':
        return <CheckCircle2 className="w-4 h-4 text-emerald-400" />
      case 'warning':
        return <AlertTriangle className="w-4 h-4 text-amber-400" />
      case 'error':
        return <Zap className="w-4 h-4 text-rose-400" />
      case 'info':
        return <Activity className="w-4 h-4 text-blue-400" />
    }
  }

  const getBorderColor = (type: Event['type']) => {
    switch (type) {
      case 'success':
        return 'border-emerald-500/20 hover:border-emerald-500/40'
      case 'warning':
        return 'border-amber-500/20 hover:border-amber-500/40'
      case 'error':
        return 'border-rose-500/20 hover:border-rose-500/40'
      case 'info':
        return 'border-blue-500/20 hover:border-blue-500/40'
    }
  }

  if (filteredEvents.length === 0) {
    return (
      <div className="text-center py-8 text-gray-500">
        <Clock className="w-8 h-8 mx-auto mb-2 opacity-50" />
        <p className="text-sm">No recent events</p>
      </div>
    )
  }

  return (
    <div className="space-y-2">
      {filteredEvents.map((event) => (
        <div
          key={event.id}
          className={`group flex items-start gap-3 p-3 rounded-lg bg-gray-800/30 border ${getBorderColor(event.type)}
                      transition-all duration-300 animate-slide-in`}
        >
          <div className="mt-0.5">{getIcon(event.type)}</div>
          <div className="flex-1 min-w-0">
            <p className="text-sm text-gray-200">
              {event.message}
              {event.soulName && (
                <span className="text-gray-500"> · {event.soulName}</span>
              )}
            </p>
            <p className="text-xs text-gray-500 mt-0.5">
              {formatDistanceToNow(event.timestamp)}
            </p>
          </div>
          <button
            onClick={() => dismissEvent(event.id)}
            className="opacity-0 group-hover:opacity-100 p-1 text-gray-500 hover:text-gray-300
                       transition-opacity"
            aria-label="Dismiss event"
          >
            <X className="w-3 h-3" />
          </button>
        </div>
      ))}
    </div>
  )
}

function messageToEvent(message: WebSocketMessage): Event | null {
  const payload = isRecord(message.data) ? message.data : {}
  const timestamp = message.timestamp || new Date().toISOString()
  const id = `${message.type}-${timestamp}-${String(payload.id ?? payload.soul_id ?? '')}`

  switch (message.type) {
    case 'judgment': {
      const status = String(payload.status ?? '').toLowerCase()
      const passed = status === 'alive' || status === 'healthy' || status === 'passed'
      return {
        id,
        type: passed ? 'success' : 'error',
        message: passed ? 'Health check passed' : 'Health check failed',
        soulName: stringValue(payload.soul_name) ?? stringValue(payload.soul_id),
        timestamp
      }
    }
    case 'alert':
      return {
        id,
        type: severityToEventType(payload.severity),
        message: stringValue(payload.message) ?? 'Alert triggered',
        soulName: stringValue(payload.soul_name) ?? stringValue(payload.soul_id),
        timestamp
      }
    case 'incident':
      return {
        id,
        type: severityToEventType(payload.severity),
        message: `Incident ${String(payload.status ?? 'updated')}`,
        soulName: stringValue(payload.soul_name) ?? stringValue(payload.soul_id),
        timestamp
      }
    case 'soul_update':
      return {
        id,
        type: 'info',
        message: 'Soul updated',
        soulName: stringValue(payload.name) ?? stringValue(payload.id),
        timestamp
      }
    default:
      return null
  }
}

function severityToEventType(severity: unknown): Event['type'] {
  switch (severity) {
    case 'critical':
      return 'error'
    case 'warning':
      return 'warning'
    default:
      return 'info'
  }
}

function stringValue(value: unknown): string | undefined {
  return typeof value === 'string' && value !== '' ? value : undefined
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
