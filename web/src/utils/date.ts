import { useSyncExternalStore } from 'react'

export function formatDistanceToNow(date: string | Date): string {
  const now = new Date()
  const then = new Date(date)
  if (!Number.isFinite(then.getTime())) return 'Unknown time'
  const seconds = Math.floor((now.getTime() - then.getTime()) / 1000)

  if (seconds < 60) {
    return 'just now'
  }

  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) {
    return `${minutes}m ago`
  }

  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    return `${hours}h ago`
  }

  const days = Math.floor(hours / 24)
  if (days < 30) {
    return `${days}d ago`
  }

  const months = Math.floor(days / 30)
  if (months < 12) {
    return `${months}mo ago`
  }

  const years = Math.floor(months / 12)
  return `${years}y ago`
}

// ---------------------------------------------------------------------------
// Display time zone (Settings → Timezone). Empty means the viewer's own
// local time, which is also what an unknown zone falls back to.


let displayTimeZone = ''
const timeZoneListeners = new Set<() => void>()

export function isValidTimeZone(zone: string): boolean {
  if (!zone) return false
  try {
    new Intl.DateTimeFormat(undefined, { timeZone: zone })
    return true
  } catch {
    return false
  }
}

export function getDisplayTimeZone(): string {
  return displayTimeZone
}

export function setDisplayTimeZone(zone: string | null | undefined) {
  const next = zone && isValidTimeZone(zone) ? zone : ''
  if (next === displayTimeZone) return
  displayTimeZone = next
  timeZoneListeners.forEach((listener) => listener())
}

function subscribeTimeZone(listener: () => void) {
  timeZoneListeners.add(listener)
  return () => timeZoneListeners.delete(listener)
}

/** Re-renders the caller when the display time zone changes. */
export function useDisplayTimeZone(): string {
  return useSyncExternalStore(subscribeTimeZone, getDisplayTimeZone, getDisplayTimeZone)
}

function zoneOptions(options?: Intl.DateTimeFormatOptions): Intl.DateTimeFormatOptions {
  return displayTimeZone ? { ...options, timeZone: displayTimeZone } : { ...options }
}

/** Date and time in the display time zone. */
export function formatDateTime(value: string | number | Date): string {
  return new Date(value).toLocaleString(undefined, zoneOptions())
}

/** Date only, in the display time zone. */
export function formatDate(value: string | number | Date): string {
  return new Date(value).toLocaleDateString(undefined, zoneOptions())
}

/** Time only, in the display time zone. */
export function formatTime(value: string | number | Date): string {
  return new Date(value).toLocaleTimeString(undefined, zoneOptions())
}

const fallbackTimeZones = [
  'UTC', 'Europe/London', 'Europe/Berlin', 'Europe/Istanbul', 'Europe/Moscow',
  'Asia/Dubai', 'Asia/Kolkata', 'Asia/Shanghai', 'Asia/Tokyo', 'Australia/Sydney',
  'America/Sao_Paulo', 'America/New_York', 'America/Chicago', 'America/Denver',
  'America/Los_Angeles',
]

/** Every IANA zone the browser knows, UTC first. */
export function listTimeZones(): string[] {
  const supported = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf
  const zones = typeof supported === 'function' ? supported('timeZone') : fallbackTimeZones
  return ['UTC', ...zones.filter((zone) => zone !== 'UTC')]
}
