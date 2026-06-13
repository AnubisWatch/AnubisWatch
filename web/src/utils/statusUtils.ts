// Shared status utility functions to eliminate duplicated code across components

export type DisplayStatus = 'healthy' | 'unhealthy' | 'unknown' | 'checking' | 'check_failed' | 'degraded' | 'down' | 'up'
export type ClusterStatus = 'healthy' | 'unhealthy' | 'offline'

// Status color mappings - single source of truth
export const statusColors: Record<DisplayStatus, { bg: string; text: string; label: string }> = {
  healthy: { bg: 'bg-emerald-500', text: 'text-emerald-400', label: 'Healthy' },
  up: { bg: 'bg-emerald-500', text: 'text-emerald-400', label: 'Up' },
  unhealthy: { bg: 'bg-rose-500', text: 'text-rose-400', label: 'Unhealthy' },
  down: { bg: 'bg-rose-500', text: 'text-rose-400', label: 'Down' },
  checking: { bg: 'bg-amber-400 animate-pulse', text: 'text-amber-400', label: 'Checking' },
  check_failed: { bg: 'bg-rose-500', text: 'text-rose-400', label: 'Check failed' },
  degraded: { bg: 'bg-amber-500', text: 'text-amber-400', label: 'Degraded' },
  unknown: { bg: 'bg-gray-500', text: 'text-gray-400', label: 'Unknown' },
}

// Cluster-specific status colors
export const clusterStatusColors: Record<ClusterStatus, { bg: string; text: string }> = {
  healthy: { bg: 'bg-emerald-500', text: 'text-emerald-400' },
  unhealthy: { bg: 'bg-amber-500', text: 'text-amber-400' },
  offline: { bg: 'bg-rose-500', text: 'text-rose-400' },
}

export function getStatusColor(status?: DisplayStatus): string {
  return statusColors[status ?? 'unknown'].bg
}

export function getStatusText(status?: DisplayStatus): string {
  return statusColors[status ?? 'unknown'].text
}

export function getStatusLabel(status?: DisplayStatus): string {
  return statusColors[status ?? 'unknown'].label
}

export function getClusterStatusColor(status: string): string {
  return (clusterStatusColors as Record<string, { bg: string; text: string }>)[status]?.bg ?? 'bg-gray-500'
}

export function getClusterStatusTextColor(status: string): string {
  return (clusterStatusColors as Record<string, { bg: string; text: string }>)[status]?.text ?? 'text-gray-400'
}
